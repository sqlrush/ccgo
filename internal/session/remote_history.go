package session

import (
	"context"
	"encoding/json"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"ccgo/internal/auth"
	"ccgo/internal/contracts"
)

const RemoteHistoryPageSize = 100
const RemoteHistoryBeta = "ccr-byoc-2025-07-29"

type RemoteHistoryPage struct {
	Events  []contracts.SDKEvent
	FirstID string
	HasMore bool
}

type RemoteHistoryFetchOptions struct {
	Limit    int
	MaxPages int
	BeforeID string
}

type RemoteHistoryEvents struct {
	Events       []contracts.SDKEvent
	Pages        int
	Complete     bool
	NextBeforeID string
}

type RemoteHistoryAuthContext struct {
	BaseURL string
	Headers http.Header
}

type RemoteHistoryTokenProvider interface {
	CurrentAccessToken(context.Context) (string, error)
	RefreshAccessToken(context.Context) (string, error)
}

type sessionEventsResponse struct {
	Data              []contracts.SDKEvent `json:"data"`
	Events            []contracts.SDKEvent `json:"events"`
	Items             []contracts.SDKEvent `json:"items"`
	Results           []contracts.SDKEvent `json:"results"`
	HasMore           bool                 `json:"has_more"`
	HasMoreCamel      bool                 `json:"hasMore"`
	FirstID           string               `json:"first_id"`
	FirstIDCamel      string               `json:"firstId"`
	NextBeforeID      string               `json:"next_before_id"`
	NextBeforeIDCamel string               `json:"nextBeforeId"`
	LastID            string               `json:"last_id"`
	LastIDCamel       string               `json:"lastId"`
}

func NewRemoteHistoryAuthContext(sessionID string, accessToken string, orgUUID string, config auth.OAuthConfig) RemoteHistoryAuthContext {
	if config.BaseAPIURL == "" {
		config = auth.ProductionOAuthConfig()
	}
	base := strings.TrimRight(config.BaseAPIURL, "/") + "/v1/sessions/" + url.PathEscape(sessionID) + "/events"
	headers := http.Header{}
	if accessToken != "" {
		headers.Set("Authorization", "Bearer "+accessToken)
	}
	headers.Set("anthropic-beta", RemoteHistoryBeta)
	if orgUUID != "" {
		headers.Set("x-organization-uuid", orgUUID)
	}
	return RemoteHistoryAuthContext{BaseURL: base, Headers: headers}
}

func (c RemoteHistoryAuthContext) WithAccessToken(accessToken string) RemoteHistoryAuthContext {
	headers := cloneHeader(c.Headers)
	if accessToken == "" {
		headers.Del("Authorization")
	} else {
		headers.Set("Authorization", "Bearer "+accessToken)
	}
	return RemoteHistoryAuthContext{BaseURL: c.BaseURL, Headers: headers}
}

func LatestEventsQuery(limit int) url.Values {
	if limit <= 0 {
		limit = RemoteHistoryPageSize
	}
	values := url.Values{}
	values.Set("limit", strconv.Itoa(limit))
	values.Set("anchor_to_latest", "true")
	return values
}

func OlderEventsQuery(beforeID string, limit int) url.Values {
	if limit <= 0 {
		limit = RemoteHistoryPageSize
	}
	values := url.Values{}
	values.Set("limit", strconv.Itoa(limit))
	values.Set("before_id", beforeID)
	return values
}

func FetchLatestEvents(ctx context.Context, client *http.Client, authCtx RemoteHistoryAuthContext, limit int) (*RemoteHistoryPage, error) {
	return fetchRemoteHistoryPage(ctx, client, authCtx, LatestEventsQuery(limit))
}

func FetchOlderEvents(ctx context.Context, client *http.Client, authCtx RemoteHistoryAuthContext, beforeID string, limit int) (*RemoteHistoryPage, error) {
	return fetchRemoteHistoryPage(ctx, client, authCtx, OlderEventsQuery(beforeID, limit))
}

func FetchLatestEventsWithTokenRefresh(ctx context.Context, client *http.Client, authCtx RemoteHistoryAuthContext, provider RemoteHistoryTokenProvider, limit int) (*RemoteHistoryPage, error) {
	return fetchRemoteHistoryPageWithTokenRefresh(ctx, client, authCtx, provider, LatestEventsQuery(limit))
}

func FetchOlderEventsWithTokenRefresh(ctx context.Context, client *http.Client, authCtx RemoteHistoryAuthContext, provider RemoteHistoryTokenProvider, beforeID string, limit int) (*RemoteHistoryPage, error) {
	return fetchRemoteHistoryPageWithTokenRefresh(ctx, client, authCtx, provider, OlderEventsQuery(beforeID, limit))
}

func FetchRemoteHistory(ctx context.Context, client *http.Client, authCtx RemoteHistoryAuthContext, options RemoteHistoryFetchOptions) (*RemoteHistoryEvents, error) {
	return fetchRemoteHistory(ctx, func(query url.Values) (*RemoteHistoryPage, error) {
		return fetchRemoteHistoryPage(ctx, client, authCtx, query)
	}, options)
}

func FetchRemoteHistoryWithTokenRefresh(ctx context.Context, client *http.Client, authCtx RemoteHistoryAuthContext, provider RemoteHistoryTokenProvider, options RemoteHistoryFetchOptions) (*RemoteHistoryEvents, error) {
	if provider == nil {
		return FetchRemoteHistory(ctx, client, authCtx, options)
	}
	pageAuthCtx := authCtx
	return fetchRemoteHistory(ctx, func(query url.Values) (*RemoteHistoryPage, error) {
		if pageAuthCtx.Headers.Get("Authorization") == "" {
			token, err := provider.CurrentAccessToken(ctx)
			if err != nil {
				return nil, err
			}
			pageAuthCtx = pageAuthCtx.WithAccessToken(token)
		}
		page, status, err := fetchRemoteHistoryPageStatus(ctx, client, pageAuthCtx, query)
		if err != nil || status != http.StatusUnauthorized {
			return page, err
		}
		token, err := provider.RefreshAccessToken(ctx)
		if err != nil {
			return nil, err
		}
		pageAuthCtx = pageAuthCtx.WithAccessToken(token)
		return fetchRemoteHistoryPage(ctx, client, pageAuthCtx, query)
	}, options)
}

func fetchRemoteHistoryPage(ctx context.Context, client *http.Client, authCtx RemoteHistoryAuthContext, query url.Values) (*RemoteHistoryPage, error) {
	page, _, err := fetchRemoteHistoryPageStatus(ctx, client, authCtx, query)
	return page, err
}

func fetchRemoteHistoryPageWithTokenRefresh(ctx context.Context, client *http.Client, authCtx RemoteHistoryAuthContext, provider RemoteHistoryTokenProvider, query url.Values) (*RemoteHistoryPage, error) {
	if provider == nil {
		return fetchRemoteHistoryPage(ctx, client, authCtx, query)
	}
	if authCtx.Headers.Get("Authorization") == "" {
		token, err := provider.CurrentAccessToken(ctx)
		if err != nil {
			return nil, err
		}
		authCtx = authCtx.WithAccessToken(token)
	}
	page, status, err := fetchRemoteHistoryPageStatus(ctx, client, authCtx, query)
	if err != nil || status != http.StatusUnauthorized {
		return page, err
	}
	token, err := provider.RefreshAccessToken(ctx)
	if err != nil {
		return nil, err
	}
	return fetchRemoteHistoryPage(ctx, client, authCtx.WithAccessToken(token), query)
}

func fetchRemoteHistory(ctx context.Context, fetchPage func(url.Values) (*RemoteHistoryPage, error), options RemoteHistoryFetchOptions) (*RemoteHistoryEvents, error) {
	if fetchPage == nil {
		return &RemoteHistoryEvents{Events: []contracts.SDKEvent{}}, nil
	}
	limit := options.Limit
	if limit <= 0 {
		limit = RemoteHistoryPageSize
	}
	result := &RemoteHistoryEvents{Events: []contracts.SDKEvent{}}
	query := LatestEventsQuery(limit)
	if options.BeforeID != "" {
		query = OlderEventsQuery(options.BeforeID, limit)
	}
	for {
		if ctx.Err() != nil {
			return result, ctx.Err()
		}
		if options.MaxPages > 0 && result.Pages >= options.MaxPages {
			result.Complete = false
			return result, nil
		}
		page, err := fetchPage(query)
		if err != nil {
			return result, err
		}
		if page == nil {
			result.Complete = false
			return result, nil
		}
		result.Pages++
		result.Events = append(result.Events, page.Events...)
		result.NextBeforeID = page.FirstID
		if !page.HasMore || page.FirstID == "" {
			result.Complete = true
			result.NextBeforeID = ""
			return result, nil
		}
		query = OlderEventsQuery(page.FirstID, limit)
	}
}

func fetchRemoteHistoryPageStatus(ctx context.Context, client *http.Client, authCtx RemoteHistoryAuthContext, query url.Values) (*RemoteHistoryPage, int, error) {
	if client == nil {
		client = &http.Client{Timeout: 15 * time.Second}
	}
	endpoint, err := url.Parse(authCtx.BaseURL)
	if err != nil {
		return nil, 0, err
	}
	endpoint.RawQuery = query.Encode()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint.String(), nil)
	if err != nil {
		return nil, 0, err
	}
	for key, values := range authCtx.Headers {
		for _, value := range values {
			req.Header.Add(key, value)
		}
	}
	resp, err := client.Do(req)
	if err != nil {
		if ctx.Err() != nil {
			return nil, 0, ctx.Err()
		}
		return nil, 0, nil
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, resp.StatusCode, nil
	}
	var decoded sessionEventsResponse
	if err := json.NewDecoder(resp.Body).Decode(&decoded); err != nil {
		return nil, resp.StatusCode, err
	}
	events := firstEventList(decoded.Data, decoded.Events, decoded.Items, decoded.Results)
	if events == nil {
		events = []contracts.SDKEvent{}
	}
	return &RemoteHistoryPage{
		Events:  events,
		FirstID: firstNonEmpty(decoded.FirstID, decoded.FirstIDCamel, decoded.NextBeforeID, decoded.NextBeforeIDCamel, decoded.LastID, decoded.LastIDCamel),
		HasMore: decoded.HasMore || decoded.HasMoreCamel,
	}, resp.StatusCode, nil
}

func firstEventList(values ...[]contracts.SDKEvent) []contracts.SDKEvent {
	for _, value := range values {
		if value != nil {
			return value
		}
	}
	return nil
}

func cloneHeader(header http.Header) http.Header {
	out := http.Header{}
	for key, values := range header {
		out[key] = append([]string(nil), values...)
	}
	return out
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value != "" {
			return value
		}
	}
	return ""
}
