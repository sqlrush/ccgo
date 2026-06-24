package main

// CLI-FLAG-38 (G30): --file downloads Files API resources to local paths before session start.
// CC ref: src/services/api/filesApi.ts downloadSessionFiles; main.tsx:1304-1330.

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"ccgo/internal/api/anthropic"
)

// TestDownloadFileSpecsWritesToDisk verifies that downloadFileSpecs:
// - parses "file_id:relative_path" specs,
// - GETs /v1/files/{id}/content from the Files API,
// - writes the content to cwd/relative_path.
func TestDownloadFileSpecsWritesToDisk(t *testing.T) {
	const content = "hello from files api"
	const fileID = "file-abc123"

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		wantPath := "/v1/files/" + fileID + "/content"
		if r.URL.Path != wantPath {
			t.Errorf("unexpected path %s want %s", r.URL.Path, wantPath)
			http.Error(w, "not found", http.StatusNotFound)
			return
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(content))
	}))
	defer srv.Close()

	cwd := t.TempDir()
	fc := &anthropic.FilesClient{
		BaseURL:    srv.URL,
		HTTPClient: srv.Client(),
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	specs := []string{fileID + ":sub/doc.txt"}
	if err := downloadFileSpecs(ctx, fc, specs, cwd); err != nil {
		t.Fatalf("downloadFileSpecs error: %v", err)
	}

	dst := filepath.Join(cwd, "sub", "doc.txt")
	got, err := os.ReadFile(dst)
	if err != nil {
		t.Fatalf("ReadFile %s: %v", dst, err)
	}
	if string(got) != content {
		t.Errorf("file content = %q want %q", got, content)
	}
}

// TestDownloadFileSpecsInvalidSpec verifies downloadFileSpecs returns error on malformed spec.
func TestDownloadFileSpecsInvalidSpec(t *testing.T) {
	fc := &anthropic.FilesClient{}
	ctx := context.Background()
	err := downloadFileSpecs(ctx, fc, []string{"nocolon"}, t.TempDir())
	if err == nil {
		t.Fatal("expected error for invalid spec")
	}
}

// TestDownloadFileSpecsAPIError verifies downloadFileSpecs propagates Files API errors.
func TestDownloadFileSpecsAPIError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, `{"error":"not found"}`, http.StatusNotFound)
	}))
	defer srv.Close()

	fc := &anthropic.FilesClient{BaseURL: srv.URL, HTTPClient: srv.Client()}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	err := downloadFileSpecs(ctx, fc, []string{"file-bad:out.txt"}, t.TempDir())
	if err == nil {
		t.Fatal("expected error on 404")
	}
}

// TestDownloadFileSpecsMultiple verifies multiple specs all download correctly.
func TestDownloadFileSpecsMultiple(t *testing.T) {
	files := map[string]string{
		"file-001": "content one",
		"file-002": "content two",
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// r.URL.Path looks like /v1/files/{id}/content
		parts := filepath.Base(filepath.Dir(r.URL.Path)) // "file-001" or "file-002"
		if c, ok := files[parts]; ok {
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(c))
			return
		}
		t.Errorf("unexpected path %s", r.URL.Path)
		http.Error(w, "not found", http.StatusNotFound)
	}))
	defer srv.Close()

	fc := &anthropic.FilesClient{BaseURL: srv.URL, HTTPClient: srv.Client()}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	cwd := t.TempDir()
	specs := []string{"file-001:a.txt", "file-002:b.txt"}
	if err := downloadFileSpecs(ctx, fc, specs, cwd); err != nil {
		t.Fatalf("downloadFileSpecs: %v", err)
	}

	for _, tc := range []struct{ id, path, want string }{
		{"file-001", "a.txt", "content one"},
		{"file-002", "b.txt", "content two"},
	} {
		got, err := os.ReadFile(filepath.Join(cwd, tc.path))
		if err != nil {
			t.Fatalf("ReadFile %s: %v", tc.path, err)
		}
		if string(got) != tc.want {
			t.Errorf("%s content = %q want %q", tc.path, got, tc.want)
		}
	}
}

