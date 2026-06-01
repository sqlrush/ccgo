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

type RemoteHistoryAuthContext struct {
	BaseURL string
	Headers http.Header
}

type sessionEventsResponse struct {
	Data    []contracts.SDKEvent `json:"data"`
	HasMore bool                 `json:"has_more"`
	FirstID string               `json:"first_id"`
	LastID  string               `json:"last_id"`
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

func fetchRemoteHistoryPage(ctx context.Context, client *http.Client, authCtx RemoteHistoryAuthContext, query url.Values) (*RemoteHistoryPage, error) {
	if client == nil {
		client = &http.Client{Timeout: 15 * time.Second}
	}
	endpoint, err := url.Parse(authCtx.BaseURL)
	if err != nil {
		return nil, err
	}
	endpoint.RawQuery = query.Encode()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint.String(), nil)
	if err != nil {
		return nil, err
	}
	for key, values := range authCtx.Headers {
		for _, value := range values {
			req.Header.Add(key, value)
		}
	}
	resp, err := client.Do(req)
	if err != nil {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		return nil, nil
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, nil
	}
	var decoded sessionEventsResponse
	if err := json.NewDecoder(resp.Body).Decode(&decoded); err != nil {
		return nil, err
	}
	events := decoded.Data
	if events == nil {
		events = []contracts.SDKEvent{}
	}
	return &RemoteHistoryPage{
		Events:  events,
		FirstID: decoded.FirstID,
		HasMore: decoded.HasMore,
	}, nil
}
