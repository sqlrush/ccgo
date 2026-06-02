package session

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
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
	Data                 []contracts.SDKEvent `json:"data"`
	Events               []contracts.SDKEvent `json:"events"`
	Items                []contracts.SDKEvent `json:"items"`
	Results              []contracts.SDKEvent `json:"results"`
	Records              []contracts.SDKEvent `json:"records"`
	Rows                 []contracts.SDKEvent `json:"rows"`
	Entries              []contracts.SDKEvent `json:"entries"`
	Messages             []contracts.SDKEvent `json:"messages"`
	History              []contracts.SDKEvent `json:"history"`
	Nodes                []contracts.SDKEvent `json:"nodes"`
	Edges                []contracts.SDKEvent `json:"edges"`
	HasMore              bool                 `json:"has_more"`
	HasMoreCamel         bool                 `json:"hasMore"`
	HasNext              bool                 `json:"has_next"`
	HasNextCamel         bool                 `json:"hasNext"`
	HasNextPage          bool                 `json:"has_next_page"`
	HasNextPageCamel     bool                 `json:"hasNextPage"`
	HasPrevious          bool                 `json:"has_previous"`
	HasPreviousCamel     bool                 `json:"hasPrevious"`
	HasPreviousPage      bool                 `json:"has_previous_page"`
	HasPreviousPageCamel bool                 `json:"hasPreviousPage"`
	HasOlder             bool                 `json:"has_older"`
	HasOlderCamel        bool                 `json:"hasOlder"`
	More                 bool                 `json:"more"`
	FirstID              string               `json:"first_id"`
	FirstIDCamel         string               `json:"firstId"`
	NextBeforeID         string               `json:"next_before_id"`
	NextBeforeIDCamel    string               `json:"nextBeforeId"`
	NextCursor           string               `json:"next_cursor"`
	NextCursorCamel      string               `json:"nextCursor"`
	EndCursor            string               `json:"end_cursor"`
	EndCursorCamel       string               `json:"endCursor"`
	StartCursor          string               `json:"start_cursor"`
	StartCursorCamel     string               `json:"startCursor"`
	PreviousCursor       string               `json:"previous_cursor"`
	PreviousCursorCamel  string               `json:"previousCursor"`
	PrevCursor           string               `json:"prev_cursor"`
	PrevCursorCamel      string               `json:"prevCursor"`
	BeforeCursor         string               `json:"before_cursor"`
	BeforeCursorCamel    string               `json:"beforeCursor"`
	OlderCursor          string               `json:"older_cursor"`
	OlderCursorCamel     string               `json:"olderCursor"`
	BeforeID             string               `json:"before_id"`
	BeforeIDCamel        string               `json:"beforeId"`
	Cursor               string               `json:"cursor"`
	CursorCamel          string               `json:"pageCursor"`
	LastID               string               `json:"last_id"`
	LastIDCamel          string               `json:"lastId"`
	NextLink             string
	PreviousLink         string
	PrevLink             string
	OlderLink            string
}

func (r *sessionEventsResponse) UnmarshalJSON(data []byte) error {
	*r = sessionEventsResponse{}
	return r.mergeJSON(data)
}

func (r *sessionEventsResponse) mergeJSON(data []byte) error {
	data = bytes.TrimSpace(data)
	if len(data) == 0 || bytes.Equal(data, []byte("null")) {
		return nil
	}
	if data[0] == '[' {
		var events []contracts.SDKEvent
		if err := json.Unmarshal(data, &events); err != nil {
			return err
		}
		if r.Data == nil {
			r.Data = events
		}
		return nil
	}
	if data[0] != '{' {
		return fmt.Errorf("remote history response must be an object or event array")
	}
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	if err := r.mergeScalarFields(raw); err != nil {
		return err
	}
	for _, spec := range []struct {
		name   string
		target *[]contracts.SDKEvent
	}{
		{name: "data", target: &r.Data},
		{name: "events", target: &r.Events},
		{name: "items", target: &r.Items},
		{name: "results", target: &r.Results},
		{name: "records", target: &r.Records},
		{name: "rows", target: &r.Rows},
		{name: "entries", target: &r.Entries},
		{name: "messages", target: &r.Messages},
		{name: "history", target: &r.History},
		{name: "nodes", target: &r.Nodes},
		{name: "edges", target: &r.Edges},
		{name: "event_list", target: &r.Events},
		{name: "eventList", target: &r.Events},
		{name: "session_events", target: &r.Events},
		{name: "sessionEvents", target: &r.Events},
		{name: "connection", target: &r.Events},
		{name: "event_connection", target: &r.Events},
		{name: "eventConnection", target: &r.Events},
		{name: "events_connection", target: &r.Events},
		{name: "eventsConnection", target: &r.Events},
		{name: "session_events_connection", target: &r.Events},
		{name: "sessionEventsConnection", target: &r.Events},
	} {
		value, ok := raw[spec.name]
		if !ok {
			continue
		}
		if err := r.mergeEventListField(spec.name, spec.target, value); err != nil {
			return err
		}
	}
	for _, name := range []string{"page", "pagination", "page_info", "pageInfo", "paging", "links", "meta", "metadata"} {
		value, ok := raw[name]
		if !ok {
			continue
		}
		if err := r.mergeWrappedFields(name, value); err != nil {
			return err
		}
	}
	return nil
}

func (r *sessionEventsResponse) mergeScalarFields(raw map[string]json.RawMessage) error {
	if err := setBoolField(raw, "has_more", &r.HasMore); err != nil {
		return err
	}
	if err := setBoolField(raw, "hasMore", &r.HasMoreCamel); err != nil {
		return err
	}
	if err := setBoolField(raw, "has_next", &r.HasNext); err != nil {
		return err
	}
	if err := setBoolField(raw, "hasNext", &r.HasNextCamel); err != nil {
		return err
	}
	if err := setBoolField(raw, "has_next_page", &r.HasNextPage); err != nil {
		return err
	}
	if err := setBoolField(raw, "hasNextPage", &r.HasNextPageCamel); err != nil {
		return err
	}
	if err := setBoolField(raw, "has_previous", &r.HasPrevious); err != nil {
		return err
	}
	if err := setBoolField(raw, "hasPrevious", &r.HasPreviousCamel); err != nil {
		return err
	}
	if err := setBoolField(raw, "has_previous_page", &r.HasPreviousPage); err != nil {
		return err
	}
	if err := setBoolField(raw, "hasPreviousPage", &r.HasPreviousPageCamel); err != nil {
		return err
	}
	if err := setBoolField(raw, "has_older", &r.HasOlder); err != nil {
		return err
	}
	if err := setBoolField(raw, "hasOlder", &r.HasOlderCamel); err != nil {
		return err
	}
	if err := setBoolField(raw, "more", &r.More); err != nil {
		return err
	}
	for _, spec := range []struct {
		name   string
		target *string
	}{
		{name: "first_id", target: &r.FirstID},
		{name: "firstId", target: &r.FirstIDCamel},
		{name: "next_before_id", target: &r.NextBeforeID},
		{name: "nextBeforeId", target: &r.NextBeforeIDCamel},
		{name: "next_cursor", target: &r.NextCursor},
		{name: "nextCursor", target: &r.NextCursorCamel},
		{name: "end_cursor", target: &r.EndCursor},
		{name: "endCursor", target: &r.EndCursorCamel},
		{name: "start_cursor", target: &r.StartCursor},
		{name: "startCursor", target: &r.StartCursorCamel},
		{name: "previous_cursor", target: &r.PreviousCursor},
		{name: "previousCursor", target: &r.PreviousCursorCamel},
		{name: "prev_cursor", target: &r.PrevCursor},
		{name: "prevCursor", target: &r.PrevCursorCamel},
		{name: "before_cursor", target: &r.BeforeCursor},
		{name: "beforeCursor", target: &r.BeforeCursorCamel},
		{name: "older_cursor", target: &r.OlderCursor},
		{name: "olderCursor", target: &r.OlderCursorCamel},
		{name: "before_id", target: &r.BeforeID},
		{name: "beforeId", target: &r.BeforeIDCamel},
		{name: "cursor", target: &r.Cursor},
		{name: "pageCursor", target: &r.CursorCamel},
		{name: "last_id", target: &r.LastID},
		{name: "lastId", target: &r.LastIDCamel},
	} {
		if err := setStringField(raw, spec.name, spec.target); err != nil {
			return err
		}
	}
	for _, spec := range []struct {
		names  []string
		target *string
	}{
		{names: []string{"next", "next_link", "nextLink", "next_url", "nextUrl", "next_href", "nextHref"}, target: &r.NextLink},
		{names: []string{"previous", "previous_link", "previousLink", "previous_url", "previousUrl", "previous_href", "previousHref"}, target: &r.PreviousLink},
		{names: []string{"prev", "prev_link", "prevLink", "prev_url", "prevUrl", "prev_href", "prevHref"}, target: &r.PrevLink},
		{names: []string{"older", "older_link", "olderLink", "older_url", "olderUrl", "older_href", "olderHref"}, target: &r.OlderLink},
	} {
		if err := setLinkField(raw, spec.names, spec.target); err != nil {
			return err
		}
	}
	return nil
}

func (r *sessionEventsResponse) mergeEventListField(name string, target *[]contracts.SDKEvent, data json.RawMessage) error {
	data = bytes.TrimSpace(data)
	if len(data) == 0 || bytes.Equal(data, []byte("null")) {
		return nil
	}
	if data[0] == '[' {
		events, err := decodeRemoteHistoryEventArray(name, data)
		if err != nil {
			return err
		}
		if *target == nil {
			*target = events
		}
		return nil
	}
	if data[0] == '{' {
		var nested sessionEventsResponse
		if err := nested.mergeJSON(data); err != nil {
			return fmt.Errorf("%s: %w", name, err)
		}
		if *target == nil {
			*target = responseEventList(nested)
		}
		r.mergePageFields(nested)
		return nil
	}
	return fmt.Errorf("%s must be an event array or object wrapper", name)
}

func (r *sessionEventsResponse) mergeWrappedFields(name string, data json.RawMessage) error {
	data = bytes.TrimSpace(data)
	if len(data) == 0 || bytes.Equal(data, []byte("null")) {
		return nil
	}
	if data[0] != '{' {
		return fmt.Errorf("%s must be an object wrapper", name)
	}
	var nested sessionEventsResponse
	if err := nested.mergeJSON(data); err != nil {
		return fmt.Errorf("%s: %w", name, err)
	}
	r.mergeFrom(nested)
	return nil
}

func (r *sessionEventsResponse) mergeFrom(other sessionEventsResponse) {
	if r.Data == nil {
		r.Data = other.Data
	}
	if r.Events == nil {
		r.Events = other.Events
	}
	if r.Items == nil {
		r.Items = other.Items
	}
	if r.Results == nil {
		r.Results = other.Results
	}
	if r.Records == nil {
		r.Records = other.Records
	}
	if r.Rows == nil {
		r.Rows = other.Rows
	}
	if r.Entries == nil {
		r.Entries = other.Entries
	}
	if r.Messages == nil {
		r.Messages = other.Messages
	}
	if r.History == nil {
		r.History = other.History
	}
	if r.Nodes == nil {
		r.Nodes = other.Nodes
	}
	if r.Edges == nil {
		r.Edges = other.Edges
	}
	r.mergePageFields(other)
}

func (r *sessionEventsResponse) mergePageFields(other sessionEventsResponse) {
	r.HasMore = r.HasMore || other.HasMore
	r.HasMoreCamel = r.HasMoreCamel || other.HasMoreCamel
	r.HasNext = r.HasNext || other.HasNext
	r.HasNextCamel = r.HasNextCamel || other.HasNextCamel
	r.HasNextPage = r.HasNextPage || other.HasNextPage
	r.HasNextPageCamel = r.HasNextPageCamel || other.HasNextPageCamel
	r.HasPrevious = r.HasPrevious || other.HasPrevious
	r.HasPreviousCamel = r.HasPreviousCamel || other.HasPreviousCamel
	r.HasPreviousPage = r.HasPreviousPage || other.HasPreviousPage
	r.HasPreviousPageCamel = r.HasPreviousPageCamel || other.HasPreviousPageCamel
	r.HasOlder = r.HasOlder || other.HasOlder
	r.HasOlderCamel = r.HasOlderCamel || other.HasOlderCamel
	r.More = r.More || other.More
	setIfEmpty(&r.FirstID, other.FirstID)
	setIfEmpty(&r.FirstIDCamel, other.FirstIDCamel)
	setIfEmpty(&r.NextBeforeID, other.NextBeforeID)
	setIfEmpty(&r.NextBeforeIDCamel, other.NextBeforeIDCamel)
	setIfEmpty(&r.NextCursor, other.NextCursor)
	setIfEmpty(&r.NextCursorCamel, other.NextCursorCamel)
	setIfEmpty(&r.EndCursor, other.EndCursor)
	setIfEmpty(&r.EndCursorCamel, other.EndCursorCamel)
	setIfEmpty(&r.StartCursor, other.StartCursor)
	setIfEmpty(&r.StartCursorCamel, other.StartCursorCamel)
	setIfEmpty(&r.PreviousCursor, other.PreviousCursor)
	setIfEmpty(&r.PreviousCursorCamel, other.PreviousCursorCamel)
	setIfEmpty(&r.PrevCursor, other.PrevCursor)
	setIfEmpty(&r.PrevCursorCamel, other.PrevCursorCamel)
	setIfEmpty(&r.BeforeCursor, other.BeforeCursor)
	setIfEmpty(&r.BeforeCursorCamel, other.BeforeCursorCamel)
	setIfEmpty(&r.OlderCursor, other.OlderCursor)
	setIfEmpty(&r.OlderCursorCamel, other.OlderCursorCamel)
	setIfEmpty(&r.BeforeID, other.BeforeID)
	setIfEmpty(&r.BeforeIDCamel, other.BeforeIDCamel)
	setIfEmpty(&r.Cursor, other.Cursor)
	setIfEmpty(&r.CursorCamel, other.CursorCamel)
	setIfEmpty(&r.LastID, other.LastID)
	setIfEmpty(&r.LastIDCamel, other.LastIDCamel)
	setIfEmpty(&r.NextLink, other.NextLink)
	setIfEmpty(&r.PreviousLink, other.PreviousLink)
	setIfEmpty(&r.PrevLink, other.PrevLink)
	setIfEmpty(&r.OlderLink, other.OlderLink)
}

func setBoolField(raw map[string]json.RawMessage, name string, target *bool) error {
	data, ok := raw[name]
	if !ok {
		return nil
	}
	data = bytes.TrimSpace(data)
	if len(data) == 0 || bytes.Equal(data, []byte("null")) {
		return nil
	}
	var value bool
	if err := json.Unmarshal(data, &value); err == nil {
		*target = value
		return nil
	}
	var text string
	if err := json.Unmarshal(data, &text); err != nil {
		return fmt.Errorf("%s: %w", name, err)
	}
	switch strings.ToLower(strings.TrimSpace(text)) {
	case "true":
		*target = true
	case "false":
		*target = false
	default:
		return fmt.Errorf("%s must be a boolean", name)
	}
	return nil
}

func setStringField(raw map[string]json.RawMessage, name string, target *string) error {
	if *target != "" {
		return nil
	}
	data, ok := raw[name]
	if !ok {
		return nil
	}
	data = bytes.TrimSpace(data)
	if len(data) == 0 || bytes.Equal(data, []byte("null")) {
		return nil
	}
	var value string
	if err := json.Unmarshal(data, &value); err != nil {
		return fmt.Errorf("%s: %w", name, err)
	}
	*target = value
	return nil
}

func setLinkField(raw map[string]json.RawMessage, names []string, target *string) error {
	if *target != "" {
		return nil
	}
	for _, name := range names {
		data, ok := raw[name]
		if !ok {
			continue
		}
		data = bytes.TrimSpace(data)
		if len(data) == 0 || bytes.Equal(data, []byte("null")) {
			continue
		}
		var value string
		if err := json.Unmarshal(data, &value); err == nil {
			*target = value
			return nil
		}
		var object map[string]json.RawMessage
		if err := json.Unmarshal(data, &object); err != nil {
			return fmt.Errorf("%s: %w", name, err)
		}
		value = remoteHistoryStringField(object, "href", "url", "uri", "link")
		if value != "" {
			*target = value
			return nil
		}
	}
	return nil
}

func setIfEmpty(target *string, value string) {
	if *target == "" {
		*target = value
	}
}

func decodeRemoteHistoryEventArray(name string, data json.RawMessage) ([]contracts.SDKEvent, error) {
	if name != "edges" {
		var events []contracts.SDKEvent
		if err := json.Unmarshal(data, &events); err != nil {
			return nil, fmt.Errorf("%s: %w", name, err)
		}
		return events, nil
	}
	var rawEdges []map[string]json.RawMessage
	if err := json.Unmarshal(data, &rawEdges); err != nil {
		return nil, fmt.Errorf("%s: %w", name, err)
	}
	events := make([]contracts.SDKEvent, 0, len(rawEdges))
	for index, edge := range rawEdges {
		cursor := remoteHistoryStringField(edge, "cursor")
		rawEvent := firstRawField(edge, "node", "event", "record", "item")
		if rawEvent == nil {
			rawEvent = edgeJSON(edge)
		}
		var event contracts.SDKEvent
		if err := json.Unmarshal(rawEvent, &event); err != nil {
			return nil, fmt.Errorf("%s[%d]: %w", name, index, err)
		}
		if event.ID == "" {
			event.ID = contracts.ID(cursor)
		}
		events = append(events, event)
	}
	return events, nil
}

func firstRawField(raw map[string]json.RawMessage, names ...string) json.RawMessage {
	for _, name := range names {
		value, ok := raw[name]
		if ok && len(bytes.TrimSpace(value)) > 0 && !bytes.Equal(bytes.TrimSpace(value), []byte("null")) {
			return value
		}
	}
	return nil
}

func remoteHistoryStringField(raw map[string]json.RawMessage, names ...string) string {
	for _, name := range names {
		value, ok := raw[name]
		if !ok || len(bytes.TrimSpace(value)) == 0 || bytes.Equal(bytes.TrimSpace(value), []byte("null")) {
			continue
		}
		var text string
		if err := json.Unmarshal(value, &text); err == nil {
			return text
		}
	}
	return ""
}

func edgeJSON(edge map[string]json.RawMessage) json.RawMessage {
	data, err := json.Marshal(edge)
	if err != nil {
		return nil
	}
	return data
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
	events := responseEventList(decoded)
	if events == nil {
		events = []contracts.SDKEvent{}
	}
	headerBeforeID := remoteHistoryLinkHeaderCursor(resp.Header.Values("Link"))
	firstID := responseNextBeforeID(decoded, events)
	hasMore := responseHasMore(decoded)
	if headerBeforeID != "" {
		if firstID == "" {
			firstID = headerBeforeID
		}
		hasMore = true
	}
	return &RemoteHistoryPage{
		Events:  events,
		FirstID: firstID,
		HasMore: hasMore,
	}, resp.StatusCode, nil
}

func responseNextBeforeID(decoded sessionEventsResponse, events []contracts.SDKEvent) string {
	firstID := firstNonEmpty(
		decoded.FirstID,
		decoded.FirstIDCamel,
		decoded.NextBeforeID,
		decoded.NextBeforeIDCamel,
		decoded.NextCursor,
		decoded.NextCursorCamel,
		decoded.EndCursor,
		decoded.EndCursorCamel,
		decoded.StartCursor,
		decoded.StartCursorCamel,
		decoded.PreviousCursor,
		decoded.PreviousCursorCamel,
		decoded.PrevCursor,
		decoded.PrevCursorCamel,
		decoded.BeforeCursor,
		decoded.BeforeCursorCamel,
		decoded.OlderCursor,
		decoded.OlderCursorCamel,
		decoded.BeforeID,
		decoded.BeforeIDCamel,
		decoded.Cursor,
		decoded.CursorCamel,
		decoded.LastID,
		decoded.LastIDCamel,
		responseLinkBeforeID(decoded),
	)
	if firstID != "" {
		return firstID
	}
	return firstRemoteHistoryEventID(events)
}

func responseHasMore(decoded sessionEventsResponse) bool {
	return decoded.HasMore ||
		decoded.HasMoreCamel ||
		decoded.HasNext ||
		decoded.HasNextCamel ||
		decoded.HasNextPage ||
		decoded.HasNextPageCamel ||
		decoded.HasPrevious ||
		decoded.HasPreviousCamel ||
		decoded.HasPreviousPage ||
		decoded.HasPreviousPageCamel ||
		decoded.HasOlder ||
		decoded.HasOlderCamel ||
		decoded.More ||
		responseLinkBeforeID(decoded) != ""
}

func responseLinkBeforeID(decoded sessionEventsResponse) string {
	for _, link := range []string{decoded.PreviousLink, decoded.PrevLink, decoded.OlderLink, decoded.NextLink} {
		if cursor := remoteHistoryLinkCursor(link); cursor != "" {
			return cursor
		}
	}
	return ""
}

func remoteHistoryLinkCursor(link string) string {
	link = strings.TrimSpace(link)
	if link == "" {
		return ""
	}
	parsed, err := url.Parse(link)
	if err == nil {
		query := parsed.Query()
		if cursor := firstNonEmpty(
			query.Get("before_id"),
			query.Get("beforeId"),
			query.Get("next_before_id"),
			query.Get("nextBeforeId"),
			query.Get("cursor"),
			query.Get("pageCursor"),
			query.Get("previous_cursor"),
			query.Get("previousCursor"),
			query.Get("prev_cursor"),
			query.Get("prevCursor"),
			query.Get("before_cursor"),
			query.Get("beforeCursor"),
			query.Get("older_cursor"),
			query.Get("olderCursor"),
			query.Get("start_cursor"),
			query.Get("startCursor"),
			query.Get("end_cursor"),
			query.Get("endCursor"),
		); cursor != "" {
			return cursor
		}
		if parsed.Scheme != "" || parsed.Host != "" || strings.Contains(link, "/") {
			return ""
		}
	}
	if strings.ContainsAny(link, "?=&/") {
		return ""
	}
	return link
}

func remoteHistoryLinkHeaderCursor(values []string) string {
	if len(values) == 0 {
		return ""
	}
	cursors := map[string]string{}
	for _, value := range values {
		for _, part := range strings.Split(value, ",") {
			link, rel := remoteHistoryLinkHeaderPart(part)
			cursor := remoteHistoryLinkCursor(link)
			if cursor == "" {
				continue
			}
			for _, name := range strings.Fields(strings.ToLower(rel)) {
				if name == "previous" || name == "prev" || name == "older" || name == "next" {
					if cursors[name] == "" {
						cursors[name] = cursor
					}
				}
			}
		}
	}
	return firstNonEmpty(cursors["previous"], cursors["prev"], cursors["older"], cursors["next"])
}

func remoteHistoryLinkHeaderPart(part string) (string, string) {
	part = strings.TrimSpace(part)
	if part == "" {
		return "", ""
	}
	link := part
	rest := ""
	if strings.HasPrefix(part, "<") {
		if end := strings.Index(part, ">"); end >= 0 {
			link = part[1:end]
			rest = part[end+1:]
		}
	} else if fields := strings.SplitN(part, ";", 2); len(fields) == 2 {
		link = fields[0]
		rest = fields[1]
	}
	rel := ""
	for _, token := range strings.Split(rest, ";") {
		name, value, ok := strings.Cut(token, "=")
		if !ok || !strings.EqualFold(strings.TrimSpace(name), "rel") {
			continue
		}
		rel = strings.Trim(strings.TrimSpace(value), `"'`)
	}
	return strings.TrimSpace(link), rel
}

func firstRemoteHistoryEventID(events []contracts.SDKEvent) string {
	for _, event := range events {
		if id := strings.TrimSpace(string(event.ID)); id != "" {
			return id
		}
	}
	return ""
}

func firstEventList(values ...[]contracts.SDKEvent) []contracts.SDKEvent {
	for _, value := range values {
		if value != nil {
			return value
		}
	}
	return nil
}

func responseEventList(response sessionEventsResponse) []contracts.SDKEvent {
	return firstEventList(response.Data, response.Events, response.Items, response.Results, response.Records, response.Rows, response.Entries, response.Messages, response.History, response.Nodes, response.Edges)
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
