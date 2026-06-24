package anthropic

// FilesClient uploads files to the Anthropic Files API.
// CC ref: src/utils/files.ts FilesAPI (SDK-64).
//
// The Files API is a separate endpoint from the messages API. Files are
// uploaded as multipart/form-data and returned with a file_id that can be
// referenced in subsequent messages.

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"path/filepath"
	"strings"
)

// FilesClient uploads files to the Anthropic Files API.
type FilesClient struct {
	// BaseURL is the Anthropic API base URL (e.g. "https://api.anthropic.com").
	// When empty, DefaultBaseURL is used.
	BaseURL string
	// APIKey is the Anthropic API key sent as x-api-key.
	APIKey string
	// HTTPClient is the underlying HTTP client. When nil, http.DefaultClient is used.
	HTTPClient *http.Client
}

// UploadedFile is the response from the Files API after a successful upload.
type UploadedFile struct {
	FileID   string `json:"file_id"`
	Filename string `json:"filename"`
}

// DownloadFile retrieves the raw content of a previously uploaded file by its
// file_id from the Files API.  The endpoint is GET /v1/files/{file_id}/content.
// CC ref: src/services/api/filesApi.ts:downloadFile (CLI-FLAG-38).
func (c *FilesClient) DownloadFile(ctx context.Context, fileID string) ([]byte, error) {
	baseURL := strings.TrimRight(c.BaseURL, "/")
	if baseURL == "" {
		baseURL = strings.TrimRight(DefaultBaseURL, "/")
	}
	url := baseURL + "/v1/files/" + fileID + "/content"

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("files api download: build request: %w", err)
	}
	req.Header.Set("anthropic-version", DefaultVersion)
	if c.APIKey != "" {
		req.Header.Set("x-api-key", c.APIKey)
	}

	httpClient := c.HTTPClient
	if httpClient == nil {
		httpClient = http.DefaultClient
	}

	resp, err := httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("files api download: http: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("files api download: read response: %w", err)
	}
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return nil, fmt.Errorf("files api download: status %d: %s", resp.StatusCode, body)
	}
	return body, nil
}

// ParseFileSpec parses a single "--file" spec of the form "file_id:relative/path".
// Returns ("", "", false) if the format is invalid.
// CC ref: src/services/api/filesApi.ts:parseFileSpecs (CLI-FLAG-38).
func ParseFileSpec(spec string) (fileID, relativePath string, ok bool) {
	idx := strings.IndexByte(spec, ':')
	if idx <= 0 || idx == len(spec)-1 {
		return "", "", false
	}
	return spec[:idx], spec[idx+1:], true
}

// UploadFile uploads content as filename with the given MIME type and returns
// the file metadata from the API. When mimeType is empty,
// "application/octet-stream" is used.
// CC ref: src/utils/files.ts FilesAPI.uploadFile (SDK-64).
func (c *FilesClient) UploadFile(ctx context.Context, filename string, content []byte, mimeType string) (*UploadedFile, error) {
	if mimeType == "" {
		mimeType = "application/octet-stream"
	}

	// Build multipart/form-data body.
	var body bytes.Buffer
	mw := multipart.NewWriter(&body)

	// Write the file field.
	h := make(map[string][]string)
	h["Content-Disposition"] = []string{fmt.Sprintf(`form-data; name="file"; filename="%s"`, filepath.Base(filename))}
	h["Content-Type"] = []string{mimeType}
	part, err := mw.CreatePart(h)
	if err != nil {
		return nil, fmt.Errorf("files api: create multipart: %w", err)
	}
	if _, err := part.Write(content); err != nil {
		return nil, fmt.Errorf("files api: write content: %w", err)
	}
	if err := mw.Close(); err != nil {
		return nil, fmt.Errorf("files api: close multipart: %w", err)
	}

	baseURL := strings.TrimRight(c.BaseURL, "/")
	if baseURL == "" {
		baseURL = strings.TrimRight(DefaultBaseURL, "/")
	}
	url := baseURL + "/v1/files"

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, &body)
	if err != nil {
		return nil, fmt.Errorf("files api: build request: %w", err)
	}
	req.Header.Set("Content-Type", mw.FormDataContentType())
	req.Header.Set("anthropic-version", DefaultVersion)
	if c.APIKey != "" {
		req.Header.Set("x-api-key", c.APIKey)
	}

	httpClient := c.HTTPClient
	if httpClient == nil {
		httpClient = http.DefaultClient
	}

	resp, err := httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("files api: http: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("files api: read response: %w", err)
	}
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return nil, fmt.Errorf("files api: status %d: %s", resp.StatusCode, respBody)
	}

	var result UploadedFile
	if err := json.Unmarshal(respBody, &result); err != nil {
		return nil, fmt.Errorf("files api: decode response: %w", err)
	}
	return &result, nil
}
