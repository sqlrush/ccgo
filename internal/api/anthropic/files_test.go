package anthropic

// TestFilesClientUploadFile verifies that FilesClient.UploadFile sends a
// multipart/form-data POST to /v1/files and decodes the JSON response.

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestFilesClientUploadFile(t *testing.T) {
	var receivedContentType string
	var receivedAPIKey string
	var receivedBody []byte

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/files" {
			t.Errorf("unexpected path: %s", r.URL.Path)
			http.Error(w, "not found", http.StatusNotFound)
			return
		}
		receivedContentType = r.Header.Get("Content-Type")
		receivedAPIKey = r.Header.Get("x-api-key")
		var err error
		receivedBody, err = io.ReadAll(r.Body)
		if err != nil {
			t.Errorf("read body: %v", err)
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"file_id":  "file-xyz789",
			"filename": "hello.txt",
		})
	}))
	defer srv.Close()

	client := &FilesClient{
		BaseURL:    srv.URL,
		APIKey:     "sk-test-key",
		HTTPClient: srv.Client(),
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	result, err := client.UploadFile(ctx, "hello.txt", []byte("hello world"), "text/plain")
	if err != nil {
		t.Fatalf("UploadFile returned error: %v", err)
	}
	if result.FileID != "file-xyz789" {
		t.Errorf("FileID = %q want file-xyz789", result.FileID)
	}
	if result.Filename != "hello.txt" {
		t.Errorf("Filename = %q want hello.txt", result.Filename)
	}
	if !strings.HasPrefix(receivedContentType, "multipart/form-data") {
		t.Errorf("Content-Type = %q want multipart/form-data prefix", receivedContentType)
	}
	if receivedAPIKey != "sk-test-key" {
		t.Errorf("x-api-key = %q want sk-test-key", receivedAPIKey)
	}
	if !strings.Contains(string(receivedBody), "hello world") {
		t.Errorf("request body missing file content; got %q", receivedBody)
	}
}

func TestFilesClientUploadFileNonOK(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, `{"error":{"type":"invalid_request_error","message":"bad request"}}`, http.StatusBadRequest)
	}))
	defer srv.Close()

	client := &FilesClient{
		BaseURL:    srv.URL,
		HTTPClient: srv.Client(),
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	_, err := client.UploadFile(ctx, "bad.txt", []byte("x"), "")
	if err == nil {
		t.Fatal("UploadFile should return error for non-2xx response")
	}
	if !strings.Contains(err.Error(), "400") {
		t.Errorf("error should mention status 400, got: %v", err)
	}
}

// TestFilesClientDownloadFile verifies DownloadFile sends GET /v1/files/{id}/content
// and returns the response body (CLI-FLAG-38).
func TestFilesClientDownloadFile(t *testing.T) {
	const wantFileID = "file-abc123"
	const wantContent = "hello from the Files API"
	var receivedAPIKey, receivedPath string

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedAPIKey = r.Header.Get("x-api-key")
		receivedPath = r.URL.Path
		if r.Method != http.MethodGet {
			t.Errorf("method = %s want GET", r.Method)
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(wantContent))
	}))
	defer srv.Close()

	client := &FilesClient{
		BaseURL:    srv.URL,
		APIKey:     "sk-test-download",
		HTTPClient: srv.Client(),
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	got, err := client.DownloadFile(ctx, wantFileID)
	if err != nil {
		t.Fatalf("DownloadFile returned error: %v", err)
	}
	if string(got) != wantContent {
		t.Errorf("content = %q want %q", got, wantContent)
	}
	wantPath := "/v1/files/" + wantFileID + "/content"
	if receivedPath != wantPath {
		t.Errorf("path = %q want %q", receivedPath, wantPath)
	}
	if receivedAPIKey != "sk-test-download" {
		t.Errorf("x-api-key = %q want sk-test-download", receivedAPIKey)
	}
}

// TestFilesClientDownloadFileNonOK verifies DownloadFile returns an error on non-2xx (CLI-FLAG-38).
func TestFilesClientDownloadFileNonOK(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "not found", http.StatusNotFound)
	}))
	defer srv.Close()

	client := &FilesClient{BaseURL: srv.URL, HTTPClient: srv.Client()}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	_, err := client.DownloadFile(ctx, "file-missing")
	if err == nil {
		t.Fatal("DownloadFile should error on 404")
	}
	if !strings.Contains(err.Error(), "404") {
		t.Errorf("error should mention 404, got: %v", err)
	}
}

// TestParseFileSpec verifies ParseFileSpec parses "file_id:path" specs (CLI-FLAG-38).
func TestParseFileSpec(t *testing.T) {
	cases := []struct {
		spec         string
		wantFileID   string
		wantPath     string
		wantOK       bool
	}{
		{"file-abc:doc.txt", "file-abc", "doc.txt", true},
		{"file-xyz:sub/dir/file.pdf", "file-xyz", "sub/dir/file.pdf", true},
		{"nocolon", "", "", false},
		{":emptyid", "", "", false},
		{"file-abc:", "", "", false},
	}
	for _, tc := range cases {
		fileID, relPath, ok := ParseFileSpec(tc.spec)
		if ok != tc.wantOK {
			t.Errorf("ParseFileSpec(%q) ok=%v want %v", tc.spec, ok, tc.wantOK)
		}
		if ok && (fileID != tc.wantFileID || relPath != tc.wantPath) {
			t.Errorf("ParseFileSpec(%q) = %q,%q want %q,%q", tc.spec, fileID, relPath, tc.wantFileID, tc.wantPath)
		}
	}
}

func TestFilesClientDefaultMimeType(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		// application/octet-stream should appear in the multipart header
		if !strings.Contains(string(body), "application/octet-stream") {
			t.Errorf("request body should contain default mime type application/octet-stream; got %q", body)
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"file_id": "file-1", "filename": "f.bin"})
	}))
	defer srv.Close()

	client := &FilesClient{BaseURL: srv.URL, HTTPClient: srv.Client()}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	_, err := client.UploadFile(ctx, "f.bin", []byte{0x01, 0x02}, "")
	if err != nil {
		t.Fatalf("UploadFile: %v", err)
	}
}
