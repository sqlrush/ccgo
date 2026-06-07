package session

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sort"
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
	Value                []contracts.SDKEvent `json:"value"`
	Values               []contracts.SDKEvent `json:"values"`
	Resources            []contracts.SDKEvent `json:"resources"`
	Collection           []contracts.SDKEvent `json:"collection"`
	Records              []contracts.SDKEvent `json:"records"`
	Rows                 []contracts.SDKEvent `json:"rows"`
	Entries              []contracts.SDKEvent `json:"entries"`
	Messages             []contracts.SDKEvent `json:"messages"`
	History              []contracts.SDKEvent `json:"history"`
	Nodes                []contracts.SDKEvent `json:"nodes"`
	Edges                []contracts.SDKEvent `json:"edges"`
	Included             []contracts.SDKEvent `json:"included"`
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
		{name: "event", target: &r.Events},
		{name: "events", target: &r.Events},
		{name: "item", target: &r.Items},
		{name: "items", target: &r.Items},
		{name: "results", target: &r.Results},
		{name: "list", target: &r.Events},
		{name: "object", target: &r.Events},
		{name: "objects", target: &r.Events},
		{name: "value", target: &r.Value},
		{name: "values", target: &r.Values},
		{name: "resources", target: &r.Resources},
		{name: "resource", target: &r.Resources},
		{name: "collection", target: &r.Collection},
		{name: "payload", target: &r.Events},
		{name: "response", target: &r.Events},
		{name: "result", target: &r.Results},
		{name: "body", target: &r.Events},
		{name: "records", target: &r.Records},
		{name: "record", target: &r.Records},
		{name: "rows", target: &r.Rows},
		{name: "entries", target: &r.Entries},
		{name: "entry", target: &r.Entries},
		{name: "messages", target: &r.Messages},
		{name: "message", target: &r.Messages},
		{name: "history", target: &r.History},
		{name: "nodes", target: &r.Nodes},
		{name: "edges", target: &r.Edges},
		{name: "children", target: &r.Events},
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
		{name: "results_connection", target: &r.Results},
		{name: "resultsConnection", target: &r.Results},
		{name: "children_connection", target: &r.Events},
		{name: "childrenConnection", target: &r.Events},
	} {
		value, ok := raw[spec.name]
		if !ok {
			continue
		}
		if err := r.mergeEventListField(spec.name, spec.target, value); err != nil {
			return err
		}
	}
	for _, name := range []string{
		"page", "pagination", "page_info", "pageInfo", "paging", "links", "_links", "meta", "metadata",
		"attributes", "properties", "attrs", "relationships",
		"session", "project_session", "projectSession", "conversation", "remote_history", "remoteHistory",
		"event_page", "eventPage", "session_history", "sessionHistory", "viewer", "node", "_embedded", "embedded",
	} {
		value, ok := raw[name]
		if !ok {
			continue
		}
		if name == "links" || name == "_links" {
			if err := r.mergeLinksField(value); err != nil {
				return err
			}
			continue
		}
		if err := r.mergeWrappedFields(name, value); err != nil {
			return err
		}
	}
	if value, ok := raw["included"]; ok {
		events, ok, err := decodeRemoteHistoryFilteredEventArray("included", value)
		if err != nil {
			return err
		}
		if ok && r.Included == nil {
			r.Included = events
		}
	}
	if responseEventList(*r) == nil {
		if text, ok := remoteHistoryProviderResponseText(raw); ok {
			var nested sessionEventsResponse
			if err := nested.mergeJSON([]byte(text)); err != nil {
				return fmt.Errorf("remote history provider response: %w", err)
			}
			r.mergeFrom(nested)
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
	for _, name := range []string{
		"has_more_results",
		"hasMoreResults",
		"has_more_items",
		"hasMoreItems",
		"has_more_pages",
		"hasMorePages",
		"is_truncated",
		"isTruncated",
		"truncated",
	} {
		if err := setBoolField(raw, name, &r.HasMore); err != nil {
			return err
		}
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
		{name: "next_page_token", target: &r.NextCursor},
		{name: "nextPageToken", target: &r.NextCursorCamel},
		{name: "next_token", target: &r.NextCursor},
		{name: "nextToken", target: &r.NextCursorCamel},
		{name: "previous_page_token", target: &r.PreviousCursor},
		{name: "previousPageToken", target: &r.PreviousCursorCamel},
		{name: "previous_token", target: &r.PreviousCursor},
		{name: "previousToken", target: &r.PreviousCursorCamel},
		{name: "prev_page_token", target: &r.PrevCursor},
		{name: "prevPageToken", target: &r.PrevCursorCamel},
		{name: "prev_token", target: &r.PrevCursor},
		{name: "prevToken", target: &r.PrevCursorCamel},
		{name: "older_page_token", target: &r.OlderCursor},
		{name: "olderPageToken", target: &r.OlderCursorCamel},
		{name: "older_token", target: &r.OlderCursor},
		{name: "olderToken", target: &r.OlderCursorCamel},
		{name: "older_than", target: &r.OlderCursor},
		{name: "olderThan", target: &r.OlderCursorCamel},
		{name: "page_token", target: &r.Cursor},
		{name: "pageToken", target: &r.CursorCamel},
		{name: "continuation_token", target: &r.Cursor},
		{name: "continuationToken", target: &r.CursorCamel},
		{name: "next_key", target: &r.NextCursor},
		{name: "nextKey", target: &r.NextCursorCamel},
		{name: "last_evaluated_key", target: &r.NextCursor},
		{name: "lastEvaluatedKey", target: &r.NextCursorCamel},
		{name: "last_key", target: &r.NextCursor},
		{name: "lastKey", target: &r.NextCursorCamel},
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
		{name: "ending_before", target: &r.BeforeCursor},
		{name: "endingBefore", target: &r.BeforeCursorCamel},
		{name: "starting_after", target: &r.NextCursor},
		{name: "startingAfter", target: &r.NextCursorCamel},
		{name: "after", target: &r.NextCursor},
		{name: "after_id", target: &r.NextCursor},
		{name: "afterId", target: &r.NextCursorCamel},
		{name: "afterID", target: &r.NextCursorCamel},
		{name: "until", target: &r.BeforeCursor},
		{name: "until_id", target: &r.BeforeCursor},
		{name: "untilId", target: &r.BeforeCursorCamel},
		{name: "older_cursor", target: &r.OlderCursor},
		{name: "olderCursor", target: &r.OlderCursorCamel},
		{name: "before", target: &r.BeforeID},
		{name: "beforeID", target: &r.BeforeIDCamel},
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
		{names: []string{"next", "next_link", "nextLink", "next_url", "nextUrl", "next_href", "nextHref", "@odata.nextLink", "@odata.next_link", "odata.nextLink", "odata.next_link", "__next"}, target: &r.NextLink},
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
		events, page, err := decodeRemoteHistoryEventArrayWithPage(name, data)
		if err != nil {
			return err
		}
		if *target == nil {
			*target = events
		}
		r.mergePageFields(page)
		return nil
	}
	if data[0] == '{' {
		var fields map[string]json.RawMessage
		if err := json.Unmarshal(data, &fields); err == nil && remoteHistoryLooksLikeSingleEvent(fields) {
			event, err := decodeRemoteHistoryEventElement(name, 0, data)
			if err != nil {
				return err
			}
			if *target == nil {
				*target = []contracts.SDKEvent{event}
			}
			return nil
		}
		events, ok, err := decodeRemoteHistoryEventMap(name, data)
		if err != nil {
			return err
		}
		if ok {
			if *target == nil {
				*target = events
			}
			return nil
		}
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

func (r *sessionEventsResponse) mergeLinksField(data json.RawMessage) error {
	data = bytes.TrimSpace(data)
	if len(data) == 0 || bytes.Equal(data, []byte("null")) {
		return nil
	}
	if data[0] == '{' {
		return r.mergeWrappedFields("links", data)
	}
	if data[0] != '[' {
		return fmt.Errorf("links must be an object wrapper or link array")
	}
	var links []map[string]json.RawMessage
	if err := json.Unmarshal(data, &links); err != nil {
		return fmt.Errorf("links: %w", err)
	}
	for _, link := range links {
		href := firstNonEmpty(remoteHistoryStringField(link, "href", "url", "uri", "link"), remoteHistoryCursorField(link))
		if href == "" {
			continue
		}
		for _, rel := range remoteHistoryRelTokens(link) {
			switch rel {
			case "previous":
				setIfEmpty(&r.PreviousLink, href)
			case "prev":
				setIfEmpty(&r.PrevLink, href)
			case "older":
				setIfEmpty(&r.OlderLink, href)
			case "next":
				setIfEmpty(&r.NextLink, href)
			}
		}
	}
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
	if r.Value == nil {
		r.Value = other.Value
	}
	if r.Values == nil {
		r.Values = other.Values
	}
	if r.Resources == nil {
		r.Resources = other.Resources
	}
	if r.Collection == nil {
		r.Collection = other.Collection
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
	if r.Included == nil {
		r.Included = other.Included
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
	var number int
	if err := json.Unmarshal(data, &number); err == nil {
		*target = number != 0
		return nil
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.UseNumber()
	var jsonNumber json.Number
	if err := decoder.Decode(&jsonNumber); err == nil {
		value, err := strconv.ParseFloat(jsonNumber.String(), 64)
		if err != nil {
			return fmt.Errorf("%s: %w", name, err)
		}
		*target = value != 0
		return nil
	}
	var text string
	if err := json.Unmarshal(data, &text); err != nil {
		return fmt.Errorf("%s: %w", name, err)
	}
	switch strings.ToLower(strings.TrimSpace(text)) {
	case "1", "t", "true", "yes", "y", "on":
		*target = true
	case "0", "f", "false", "no", "n", "off":
		*target = false
	default:
		number, err := strconv.ParseFloat(strings.TrimSpace(text), 64)
		if err == nil {
			*target = number != 0
			return nil
		}
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
	if err := json.Unmarshal(data, &value); err == nil {
		*target = value
		return nil
	}
	if value, ok := jsonNumberString(data); ok {
		*target = value
		return nil
	}
	return fmt.Errorf("%s must be a string", name)
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
		if value == "" {
			value = remoteHistoryCursorField(object)
		}
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
	events, _, err := decodeRemoteHistoryEventArrayWithPage(name, data)
	return events, err
}

func decodeRemoteHistoryEventArrayWithPage(name string, data json.RawMessage) ([]contracts.SDKEvent, sessionEventsResponse, error) {
	var page sessionEventsResponse
	if name != "edges" {
		var rawEvents []json.RawMessage
		if err := json.Unmarshal(data, &rawEvents); err != nil {
			return nil, page, fmt.Errorf("%s: %w", name, err)
		}
		events := make([]contracts.SDKEvent, 0, len(rawEvents))
		for index, rawEvent := range rawEvents {
			if !remoteHistoryRawLooksLikeEvent(rawEvent) {
				nested, ok, err := decodeRemoteHistoryNestedEventArrayItem(name, index, rawEvent)
				if err != nil {
					return nil, page, err
				}
				if ok {
					events = append(events, responseEventList(nested)...)
					page.mergePageFields(nested)
					continue
				}
				if remoteHistoryRawLooksLikeNonEventResource(rawEvent) {
					continue
				}
			}
			event, err := decodeRemoteHistoryEventElement(name, index, rawEvent)
			if err != nil {
				return nil, page, err
			}
			events = append(events, event)
		}
		return events, page, nil
	}
	var rawEdges []map[string]json.RawMessage
	if err := json.Unmarshal(data, &rawEdges); err != nil {
		return nil, page, fmt.Errorf("%s: %w", name, err)
	}
	events := make([]contracts.SDKEvent, 0, len(rawEdges))
	for index, edge := range rawEdges {
		cursor := remoteHistoryStringField(edge, "cursor")
		rawEvent := firstObjectRawField(edge, "node", "event", "record", "item", "resource", "value")
		if rawEvent == nil {
			rawEvent = edgeJSON(edge)
		}
		if !remoteHistoryRawLooksLikeEvent(rawEvent) && remoteHistoryRawLooksLikeNonEventResource(rawEvent) {
			continue
		}
		event, err := decodeRemoteHistoryEventElement(name, index, rawEvent)
		if err != nil {
			return nil, page, err
		}
		if event.ID == "" {
			event.ID = contracts.ID(cursor)
		}
		events = append(events, event)
	}
	return events, page, nil
}

func decodeRemoteHistoryNestedEventArrayItem(name string, index int, data json.RawMessage) (sessionEventsResponse, bool, error) {
	var nested sessionEventsResponse
	data = bytes.TrimSpace(data)
	if len(data) == 0 || bytes.Equal(data, []byte("null")) || data[0] != '{' {
		return nested, false, nil
	}
	if err := nested.mergeJSON(data); err != nil {
		return nested, false, fmt.Errorf("%s[%d]: %w", name, index, err)
	}
	if responseEventList(nested) == nil {
		return nested, false, nil
	}
	return nested, true, nil
}

func decodeRemoteHistoryFilteredEventArray(name string, data json.RawMessage) ([]contracts.SDKEvent, bool, error) {
	data = bytes.TrimSpace(data)
	if len(data) == 0 || bytes.Equal(data, []byte("null")) {
		return nil, false, nil
	}
	if data[0] != '[' {
		return nil, false, fmt.Errorf("%s must be an event array", name)
	}
	var rawEvents []json.RawMessage
	if err := json.Unmarshal(data, &rawEvents); err != nil {
		return nil, false, fmt.Errorf("%s: %w", name, err)
	}
	events := make([]contracts.SDKEvent, 0, len(rawEvents))
	for index, rawEvent := range rawEvents {
		if !remoteHistoryRawLooksLikeEvent(rawEvent) {
			continue
		}
		event, err := decodeRemoteHistoryEventElement(name, index, rawEvent)
		if err != nil {
			return nil, false, err
		}
		events = append(events, event)
	}
	if len(events) == 0 {
		return nil, false, nil
	}
	return events, true, nil
}

func decodeRemoteHistoryEventMap(name string, data json.RawMessage) ([]contracts.SDKEvent, bool, error) {
	var rawEvents map[string]json.RawMessage
	if err := json.Unmarshal(data, &rawEvents); err != nil {
		return nil, false, fmt.Errorf("%s: %w", name, err)
	}
	keys := make([]string, 0, len(rawEvents))
	for key, rawEvent := range rawEvents {
		if remoteHistoryEventMapReservedKey(key) {
			continue
		}
		rawEvent = bytes.TrimSpace(rawEvent)
		if len(rawEvent) == 0 || bytes.Equal(rawEvent, []byte("null")) || rawEvent[0] != '{' {
			continue
		}
		var fields map[string]json.RawMessage
		if err := json.Unmarshal(rawEvent, &fields); err != nil {
			return nil, false, fmt.Errorf("%s.%s: %w", name, key, err)
		}
		if !remoteHistoryHasDirectEventFields(fields) && remoteHistoryWrappedEventRaw(fields) == nil && !remoteHistoryLooksLikeSingleEvent(fields) {
			continue
		}
		keys = append(keys, key)
	}
	if len(keys) == 0 {
		return nil, false, nil
	}
	sort.Strings(keys)
	events := make([]contracts.SDKEvent, 0, len(keys))
	for index, key := range keys {
		event, err := decodeRemoteHistoryEventElement(name, index, rawEvents[key])
		if err != nil {
			return nil, false, err
		}
		if event.ID == "" {
			event.ID = contracts.ID(key)
		}
		events = append(events, event)
	}
	return events, true, nil
}

func remoteHistoryEventMapReservedKey(key string) bool {
	switch key {
	case "page", "pagination", "page_info", "pageInfo", "paging", "links", "_links", "meta", "metadata",
		"attributes", "properties", "attrs", "included",
		"has_more", "hasMore", "has_next", "hasNext", "has_next_page", "hasNextPage",
		"has_previous", "hasPrevious", "has_previous_page", "hasPreviousPage", "has_older", "hasOlder", "more",
		"first_id", "firstId", "next_before_id", "nextBeforeId", "next_cursor", "nextCursor",
		"next_page_token", "nextPageToken", "next_token", "nextToken", "page_token", "pageToken",
		"previous_page_token", "previousPageToken", "previous_token", "previousToken",
		"prev_page_token", "prevPageToken", "prev_token", "prevToken",
		"older_page_token", "olderPageToken", "older_token", "olderToken", "older_than", "olderThan",
		"continuation_token", "continuationToken", "before_id", "beforeId", "cursor", "pageCursor",
		"last_id", "lastId", "start_cursor", "startCursor", "end_cursor", "endCursor",
		"previous_cursor", "previousCursor", "prev_cursor", "prevCursor", "before_cursor", "beforeCursor",
		"ending_before", "endingBefore", "until", "until_id", "untilId", "before", "beforeID",
		"starting_after", "startingAfter", "after", "after_id", "afterId", "afterID",
		"older_cursor", "olderCursor":
		return true
	default:
		return false
	}
}

func remoteHistoryRawLooksLikeEvent(data json.RawMessage) bool {
	data = bytes.TrimSpace(data)
	if len(data) == 0 || bytes.Equal(data, []byte("null")) || data[0] != '{' {
		return false
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(data, &fields); err != nil {
		return false
	}
	if remoteHistoryLooksLikeSingleEvent(fields) {
		return true
	}
	if nested := remoteHistoryWrappedEventRaw(fields); nested != nil {
		return remoteHistoryRawLooksLikeEvent(nested)
	}
	return false
}

func remoteHistoryRawLooksLikeNonEventResource(data json.RawMessage) bool {
	data = bytes.TrimSpace(data)
	if len(data) == 0 || bytes.Equal(data, []byte("null")) || data[0] != '{' {
		return false
	}
	if remoteHistoryRawLooksLikeEvent(data) {
		return false
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(data, &fields); err != nil {
		return false
	}
	for _, value := range []string{
		remoteHistoryStringField(fields, "type", "resource_type", "resourceType"),
		remoteHistoryStringField(fields, "kind", "resource_kind", "resourceKind"),
	} {
		compact := strings.NewReplacer("_", "", "-", "", " ", "", ".", "").Replace(strings.ToLower(strings.TrimSpace(value)))
		if compact == "" || contracts.CanonicalSDKEventType(value) != "" {
			continue
		}
		if strings.Contains(compact, "event") || strings.Contains(compact, "message") || strings.Contains(compact, "history") {
			continue
		}
		return true
	}
	return false
}

func decodeRemoteHistoryEventElement(name string, index int, data json.RawMessage) (contracts.SDKEvent, error) {
	data = bytes.TrimSpace(data)
	if len(data) == 0 || bytes.Equal(data, []byte("null")) {
		return contracts.SDKEvent{}, nil
	}
	cursor := ""
	resourceID := ""
	rawEvent := data
	for len(rawEvent) > 0 && rawEvent[0] == '{' {
		var fields map[string]json.RawMessage
		if err := json.Unmarshal(rawEvent, &fields); err != nil {
			break
		}
		cursor = firstNonEmpty(cursor, remoteHistoryStringField(fields, "cursor"))
		resourceID = firstNonEmpty(resourceID, remoteHistoryStringField(fields, "id", "event_id", "eventId", "uuid"))
		nested := remoteHistoryWrappedEventRaw(fields)
		if nested == nil {
			break
		}
		nested = bytes.TrimSpace(nested)
		if len(nested) == 0 || bytes.Equal(nested, rawEvent) {
			break
		}
		rawEvent = nested
	}
	var event contracts.SDKEvent
	if err := json.Unmarshal(rawEvent, &event); err != nil {
		return contracts.SDKEvent{}, fmt.Errorf("%s[%d]: %w", name, index, err)
	}
	if event.ID == "" {
		event.ID = contracts.ID(firstNonEmpty(resourceID, cursor))
	}
	return event, nil
}

func remoteHistoryWrappedEventRaw(fields map[string]json.RawMessage) json.RawMessage {
	if remoteHistoryRecognizedEventType(fields) == "" {
		if nested := firstObjectRawField(fields, "attributes", "properties", "attrs"); nested != nil {
			return nested
		}
	}
	if remoteHistoryHasDirectEventFields(fields) {
		return nil
	}
	return firstObjectRawField(fields, "edge", "node", "event", "record", "entry", "item", "resource", "value", "attributes", "properties", "attrs", "data", "payload", "body", "result", "response", "output")
}

func remoteHistoryHasDirectEventFields(fields map[string]json.RawMessage) bool {
	for _, name := range []string{
		"type",
		"event_type",
		"eventType",
		"name",
		"kind",
		"role",
		"messageType",
		"message_type",
		"id",
		"event_id",
		"eventId",
		"uuid",
		"session_id",
		"sessionId",
		"sessionID",
		"session_uuid",
		"sessionUuid",
		"sessionUUID",
		"status",
		"message",
		"message_payload",
		"messagePayload",
		"serialized_message",
		"serializedMessage",
	} {
		if value, ok := fields[name]; ok && len(bytes.TrimSpace(value)) > 0 && !bytes.Equal(bytes.TrimSpace(value), []byte("null")) {
			return true
		}
	}
	if value, ok := fields["event"]; ok {
		trimmed := bytes.TrimSpace(value)
		return len(trimmed) > 0 && !bytes.Equal(trimmed, []byte("null")) && trimmed[0] != '{'
	}
	return false
}

func remoteHistoryLooksLikeSingleEvent(fields map[string]json.RawMessage) bool {
	if remoteHistoryRecognizedEventType(fields) != "" {
		return true
	}
	for _, name := range []string{"attributes", "properties", "attrs"} {
		nested := firstObjectRawField(fields, name)
		if nested == nil {
			continue
		}
		var nestedFields map[string]json.RawMessage
		if err := json.Unmarshal(nested, &nestedFields); err == nil && remoteHistoryLooksLikeSingleEvent(nestedFields) {
			return true
		}
	}
	status := remoteHistoryStringField(fields, "status")
	if status == "" {
		return false
	}
	return firstNonEmpty(
		remoteHistoryStringField(fields, "id", "event_id", "eventId", "uuid"),
		remoteHistoryStringField(fields, "session_id", "sessionId", "sessionID", "session_uuid", "sessionUuid", "sessionUUID"),
		remoteHistoryStringField(fields, "timestamp", "created_at", "createdAt", "time", "datetime", "dateTime"),
	) != ""
}

func remoteHistoryRecognizedEventType(fields map[string]json.RawMessage) string {
	for _, name := range []string{"type", "event_type", "eventType", "event", "name", "kind", "role", "messageType", "message_type"} {
		if contracts.CanonicalSDKEventType(remoteHistoryStringField(fields, name)) != "" {
			return name
		}
	}
	return ""
}

func firstObjectRawField(raw map[string]json.RawMessage, names ...string) json.RawMessage {
	for _, name := range names {
		value, ok := raw[name]
		if !ok {
			continue
		}
		trimmed := bytes.TrimSpace(value)
		if len(trimmed) > 0 && !bytes.Equal(trimmed, []byte("null")) && trimmed[0] == '{' {
			return value
		}
	}
	return nil
}

func remoteHistoryStringField(raw map[string]json.RawMessage, names ...string) string {
	for _, name := range names {
		value, ok := raw[name]
		if !ok {
			continue
		}
		if text, ok := remoteHistoryStringRawValue(value); ok {
			return text
		}
	}
	for _, name := range names {
		normalizedName := remoteHistoryNormalizedFieldName(name)
		for rawName, value := range raw {
			if remoteHistoryNormalizedFieldName(rawName) != normalizedName {
				continue
			}
			if text, ok := remoteHistoryStringRawValue(value); ok {
				return text
			}
		}
	}
	return ""
}

func remoteHistoryStringRawValue(value json.RawMessage) (string, bool) {
	if len(bytes.TrimSpace(value)) == 0 || bytes.Equal(bytes.TrimSpace(value), []byte("null")) {
		return "", false
	}
	var text string
	if err := json.Unmarshal(value, &text); err == nil {
		return text, true
	}
	if text, ok := jsonNumberString(value); ok {
		return text, true
	}
	return "", false
}

func remoteHistoryNormalizedFieldName(name string) string {
	name = strings.ToLower(strings.TrimSpace(name))
	name = strings.ReplaceAll(name, "_", "")
	name = strings.ReplaceAll(name, "-", "")
	return name
}

func remoteHistoryCursorField(raw map[string]json.RawMessage) string {
	return firstNonEmpty(
		remoteHistoryStringField(raw, "before_id", "beforeId"),
		remoteHistoryStringField(raw, "next_before_id", "nextBeforeId"),
		remoteHistoryStringField(raw, "cursor", "pageCursor"),
		remoteHistoryStringField(raw, "next_page_token", "nextPageToken"),
		remoteHistoryStringField(raw, "next_token", "nextToken"),
		remoteHistoryStringField(raw, "previous_page_token", "previousPageToken"),
		remoteHistoryStringField(raw, "previous_token", "previousToken"),
		remoteHistoryStringField(raw, "prev_page_token", "prevPageToken"),
		remoteHistoryStringField(raw, "prev_token", "prevToken"),
		remoteHistoryStringField(raw, "older_page_token", "olderPageToken"),
		remoteHistoryStringField(raw, "older_token", "olderToken"),
		remoteHistoryStringField(raw, "older_than", "olderThan"),
		remoteHistoryStringField(raw, "page_token", "pageToken"),
		remoteHistoryStringField(raw, "continuation_token", "continuationToken"),
		remoteHistoryStringField(raw, "next_key", "nextKey"),
		remoteHistoryStringField(raw, "last_evaluated_key", "lastEvaluatedKey"),
		remoteHistoryStringField(raw, "last_key", "lastKey"),
		remoteHistoryStringField(raw, "$skiptoken", "$skipToken", "skiptoken", "skipToken"),
		remoteHistoryStringField(raw, "previous_cursor", "previousCursor"),
		remoteHistoryStringField(raw, "prev_cursor", "prevCursor"),
		remoteHistoryStringField(raw, "before_cursor", "beforeCursor"),
		remoteHistoryStringField(raw, "ending_before", "endingBefore"),
		remoteHistoryStringField(raw, "starting_after", "startingAfter"),
		remoteHistoryStringField(raw, "after", "after_id", "afterId", "afterID"),
		remoteHistoryStringField(raw, "until", "until_id", "untilId"),
		remoteHistoryStringField(raw, "older_cursor", "olderCursor"),
		remoteHistoryStringField(raw, "before", "beforeID"),
		remoteHistoryStringField(raw, "start_cursor", "startCursor"),
		remoteHistoryStringField(raw, "end_cursor", "endCursor"),
	)
}

func remoteHistoryRelTokens(raw map[string]json.RawMessage) []string {
	var tokens []string
	for _, name := range []string{"rel", "relation", "name", "type", "kind", "label"} {
		value, ok := raw[name]
		if !ok {
			continue
		}
		data := bytes.TrimSpace(value)
		if len(data) == 0 || bytes.Equal(data, []byte("null")) {
			continue
		}
		var text string
		if err := json.Unmarshal(data, &text); err == nil {
			tokens = append(tokens, remoteHistoryRelTextTokens(text)...)
			continue
		}
		var values []string
		if err := json.Unmarshal(data, &values); err == nil {
			for _, value := range values {
				tokens = append(tokens, remoteHistoryRelTextTokens(value)...)
			}
		}
	}
	return tokens
}

func remoteHistoryRelTextTokens(text string) []string {
	var tokens []string
	for _, token := range strings.Fields(text) {
		if rel := remoteHistoryCanonicalRelToken(token); rel != "" {
			tokens = append(tokens, rel)
		}
	}
	return tokens
}

func remoteHistoryCanonicalRelToken(token string) string {
	compact := strings.NewReplacer("_", "", "-", "", " ", "").Replace(strings.ToLower(strings.TrimSpace(token)))
	switch compact {
	case "previous", "previouspage", "previouslink", "previouscursor":
		return "previous"
	case "prev", "prevpage", "prevlink", "prevcursor":
		return "prev"
	case "older", "olderpage", "olderlink", "oldercursor":
		return "older"
	case "next", "nextpage", "nextlink", "nextcursor":
		return "next"
	default:
		return ""
	}
}

func remoteHistoryProviderResponseText(raw map[string]json.RawMessage) (string, bool) {
	for _, name := range []string{
		"choice",
		"choices",
		"output",
		"outputs",
		"candidate",
		"candidates",
		"generation",
		"generations",
		"completion",
		"completions",
		"response",
		"responses",
		"result",
		"results",
		"message",
		"content",
		"text",
		"outputText",
		"output_text",
	} {
		value, ok := raw[name]
		if !ok {
			continue
		}
		if text, ok := remoteHistoryProviderTextFromRaw(value, 0, remoteHistoryProviderScalarEnvelope(name)); ok {
			return text, true
		}
	}
	return "", false
}

func remoteHistoryProviderScalarEnvelope(name string) bool {
	switch name {
	case "content", "text", "outputText", "output_text":
		return true
	default:
		return false
	}
}

func remoteHistoryProviderTextFromRaw(raw json.RawMessage, depth int, allowScalar bool) (string, bool) {
	raw = bytes.TrimSpace(raw)
	if len(raw) == 0 || bytes.Equal(raw, []byte("null")) || depth > 8 {
		return "", false
	}
	var text string
	if err := json.Unmarshal(raw, &text); err == nil {
		text = strings.TrimSpace(text)
		if allowScalar {
			return remoteHistoryProviderJSONPayload(text)
		}
		return "", false
	}
	if raw[0] == '[' {
		var items []json.RawMessage
		if err := json.Unmarshal(raw, &items); err != nil {
			return "", false
		}
		parts := make([]string, 0, len(items))
		for _, item := range items {
			part, ok := remoteHistoryProviderTextFromRaw(item, depth+1, false)
			if ok {
				parts = append(parts, part)
			}
		}
		if len(parts) == 0 {
			return "", false
		}
		text = strings.Join(parts, "\n")
		return remoteHistoryProviderJSONPayload(text)
	}
	if raw[0] != '{' {
		return "", false
	}
	fields := map[string]json.RawMessage{}
	if err := json.Unmarshal(raw, &fields); err != nil {
		return "", false
	}
	for _, name := range []string{"text", "content", "value", "output", "outputText", "output_text"} {
		value, ok := fields[name]
		if !ok {
			continue
		}
		if text, ok := remoteHistoryProviderTextFromRaw(value, depth+1, true); ok {
			return text, true
		}
	}
	for _, name := range []string{"message", "delta", "part", "parts", "candidate", "choice", "generation", "result", "response"} {
		value, ok := fields[name]
		if !ok {
			continue
		}
		if text, ok := remoteHistoryProviderTextFromRaw(value, depth+1, false); ok {
			return text, true
		}
	}
	return "", false
}

func remoteHistoryProviderTextLooksJSON(text string) bool {
	text = strings.TrimSpace(text)
	return strings.HasPrefix(text, "{") || strings.HasPrefix(text, "[")
}

func remoteHistoryProviderJSONPayload(text string) (string, bool) {
	text = strings.TrimSpace(text)
	if remoteHistoryProviderTextLooksJSON(text) {
		return text, true
	}
	start := strings.Index(text, "```")
	if start < 0 {
		return "", false
	}
	afterFence := text[start+3:]
	lineEnd := strings.IndexAny(afterFence, "\r\n")
	if lineEnd < 0 {
		return "", false
	}
	content := strings.TrimLeft(afterFence[lineEnd:], "\r\n")
	end := strings.Index(content, "```")
	if end >= 0 {
		content = content[:end]
	}
	content = strings.TrimSpace(content)
	if remoteHistoryProviderTextLooksJSON(content) {
		return content, true
	}
	return "", false
}

func jsonNumberString(data []byte) (string, bool) {
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.UseNumber()
	var number json.Number
	if err := decoder.Decode(&number); err != nil {
		return "", false
	}
	return number.String(), true
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
	if resp.StatusCode == http.StatusNoContent || resp.StatusCode == http.StatusNotFound || resp.StatusCode == http.StatusGone {
		return &RemoteHistoryPage{Events: []contracts.SDKEvent{}}, resp.StatusCode, nil
	}
	if resp.StatusCode != http.StatusOK {
		return nil, resp.StatusCode, nil
	}
	var decoded sessionEventsResponse
	if err := json.NewDecoder(resp.Body).Decode(&decoded); err != nil {
		if err == io.EOF {
			return &RemoteHistoryPage{Events: []contracts.SDKEvent{}}, resp.StatusCode, nil
		}
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
		if cursor := remoteHistoryQueryCursor(query); cursor != "" {
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

func remoteHistoryQueryCursor(query url.Values) string {
	return remoteHistoryQueryValue(query,
		"before_id",
		"beforeId",
		"next_before_id",
		"nextBeforeId",
		"cursor",
		"pageCursor",
		"next_page_token",
		"nextPageToken",
		"next_token",
		"nextToken",
		"previous_page_token",
		"previousPageToken",
		"previous_token",
		"previousToken",
		"prev_page_token",
		"prevPageToken",
		"prev_token",
		"prevToken",
		"older_page_token",
		"olderPageToken",
		"older_token",
		"olderToken",
		"older_than",
		"olderThan",
		"page_token",
		"pageToken",
		"continuation_token",
		"continuationToken",
		"next_key",
		"nextKey",
		"last_evaluated_key",
		"lastEvaluatedKey",
		"last_key",
		"lastKey",
		"$skiptoken",
		"$skipToken",
		"skiptoken",
		"skipToken",
		"previous_cursor",
		"previousCursor",
		"prev_cursor",
		"prevCursor",
		"before_cursor",
		"beforeCursor",
		"ending_before",
		"endingBefore",
		"starting_after",
		"startingAfter",
		"after",
		"after_id",
		"afterId",
		"afterID",
		"until",
		"until_id",
		"untilId",
		"older_cursor",
		"olderCursor",
		"before",
		"beforeID",
		"start_cursor",
		"startCursor",
		"end_cursor",
		"endCursor",
	)
}

func remoteHistoryQueryValue(query url.Values, names ...string) string {
	for _, name := range names {
		if value := strings.TrimSpace(query.Get(name)); value != "" {
			return value
		}
	}
	normalized := map[string]string{}
	for name, values := range query {
		if len(values) == 0 {
			continue
		}
		value := strings.TrimSpace(values[0])
		if value == "" {
			continue
		}
		key := remoteHistoryNormalizedQueryKey(name)
		if normalized[key] == "" {
			normalized[key] = value
		}
	}
	for _, name := range names {
		if value := normalized[remoteHistoryNormalizedQueryKey(name)]; value != "" {
			return value
		}
	}
	return ""
}

func remoteHistoryNormalizedQueryKey(name string) string {
	name = strings.ToLower(strings.TrimSpace(name))
	name = strings.ReplaceAll(name, "_", "")
	name = strings.ReplaceAll(name, "-", "")
	return name
}

func remoteHistoryLinkHeaderCursor(values []string) string {
	if len(values) == 0 {
		return ""
	}
	cursors := map[string]string{}
	for _, value := range values {
		for _, part := range remoteHistorySplitLinkHeader(value) {
			link, rel := remoteHistoryLinkHeaderPart(part)
			cursor := remoteHistoryLinkCursor(link)
			if cursor == "" {
				continue
			}
			for _, name := range remoteHistoryRelTextTokens(rel) {
				if cursors[name] == "" {
					cursors[name] = cursor
				}
			}
		}
	}
	return firstNonEmpty(cursors["previous"], cursors["prev"], cursors["older"], cursors["next"])
}

func remoteHistorySplitLinkHeader(value string) []string {
	var parts []string
	start := 0
	inAngle := false
	inQuote := false
	escaped := false
	for i := 0; i < len(value); i++ {
		ch := value[i]
		if inQuote {
			if escaped {
				escaped = false
				continue
			}
			if ch == '\\' {
				escaped = true
				continue
			}
			if ch == '"' {
				inQuote = false
			}
			continue
		}
		switch ch {
		case '"':
			inQuote = true
		case '<':
			inAngle = true
		case '>':
			inAngle = false
		case ',':
			if inAngle {
				continue
			}
			part := strings.TrimSpace(value[start:i])
			if part != "" {
				parts = append(parts, part)
			}
			start = i + 1
		}
	}
	part := strings.TrimSpace(value[start:])
	if part != "" {
		parts = append(parts, part)
	}
	return parts
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

func responseEventList(response sessionEventsResponse) []contracts.SDKEvent {
	candidates := [][]contracts.SDKEvent{
		response.Data,
		response.Events,
		response.Items,
		response.Results,
		response.Value,
		response.Values,
		response.Resources,
		response.Collection,
		response.Records,
		response.Rows,
		response.Entries,
		response.Messages,
		response.History,
		response.Nodes,
		response.Edges,
		response.Included,
	}
	var first []contracts.SDKEvent
	haveFirst := false
	for _, candidate := range candidates {
		if candidate == nil {
			continue
		}
		if !haveFirst {
			first = candidate
			haveFirst = true
		}
		if remoteHistoryEventsHaveMaterialPayload(candidate) {
			return candidate
		}
	}
	if haveFirst {
		return first
	}
	return nil
}

func remoteHistoryEventsHaveMaterialPayload(events []contracts.SDKEvent) bool {
	for _, event := range events {
		if remoteHistoryEventHasMaterialPayload(event) {
			return true
		}
	}
	return false
}

func remoteHistoryEventHasMaterialPayload(event contracts.SDKEvent) bool {
	switch event.Type {
	case contracts.SDKEventSystem,
		contracts.SDKEventAssistant,
		contracts.SDKEventUser,
		contracts.SDKEventResult,
		contracts.SDKEventError,
		contracts.SDKEventStatus:
		return true
	}
	return event.SessionID != "" ||
		event.SessionIDCamel != "" ||
		event.ParentUUID != nil ||
		event.ParentUUIDCamel != nil ||
		event.Timestamp != "" ||
		event.Message != nil ||
		event.Status != "" ||
		event.Result != nil ||
		event.Error != ""
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
