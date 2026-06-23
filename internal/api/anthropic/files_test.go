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
