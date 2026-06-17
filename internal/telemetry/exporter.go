package telemetry

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"ccgo/internal/platform"
)

const defaultExportTimeout = 2 * time.Second

type ExportTarget struct {
	Path        string
	URL         string
	Headers     map[string]string
	HTTPTimeout time.Duration
}

type Delivery struct {
	FilePath   string `json:"file_path,omitempty"`
	URL        string `json:"url,omitempty"`
	HTTPStatus int    `json:"http_status,omitempty"`
}

func (target ExportTarget) Configured() bool {
	return strings.TrimSpace(target.Path) != "" || strings.TrimSpace(target.URL) != ""
}

func ExportEvent(ctx context.Context, target ExportTarget, event Event) (Delivery, error) {
	if !target.Configured() {
		return Delivery{}, nil
	}
	if ctx == nil {
		ctx = context.Background()
	}
	event = PrepareEvent(event)
	data, err := json.Marshal(event)
	if err != nil {
		return Delivery{}, err
	}
	var delivery Delivery
	var errs []error
	if path := strings.TrimSpace(target.Path); path != "" {
		if err := appendExportLine(path, data); err != nil {
			errs = append(errs, err)
		} else {
			delivery.FilePath = path
		}
	}
	if rawURL := strings.TrimSpace(target.URL); rawURL != "" {
		status, err := postExportEvent(ctx, rawURL, target.Headers, target.HTTPTimeout, data)
		if err != nil {
			errs = append(errs, err)
		} else {
			delivery.URL = RedactEndpoint(rawURL)
			delivery.HTTPStatus = status
		}
	}
	if len(errs) > 0 {
		return delivery, errorsJoin(errs)
	}
	return delivery, nil
}

func appendExportLine(path string, data []byte) error {
	if err := platform.EnsureDir(filepath.Dir(path)); err != nil {
		return err
	}
	file, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return err
	}
	defer file.Close()
	line := append(append([]byte(nil), data...), '\n')
	_, err = file.Write(line)
	return err
}

func postExportEvent(ctx context.Context, rawURL string, headers map[string]string, timeout time.Duration, data []byte) (int, error) {
	parsed, err := url.Parse(rawURL)
	if err != nil {
		return 0, err
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return 0, fmt.Errorf("telemetry exporter url requires http or https scheme")
	}
	if timeout <= 0 {
		timeout = defaultExportTimeout
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, rawURL, bytes.NewReader(data))
	if err != nil {
		return 0, err
	}
	request.Header.Set("Content-Type", "application/json")
	for key, value := range headers {
		key = strings.TrimSpace(key)
		if key == "" {
			continue
		}
		request.Header.Set(key, value)
	}
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		return 0, err
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return response.StatusCode, fmt.Errorf("telemetry exporter returned HTTP %d", response.StatusCode)
	}
	return response.StatusCode, nil
}

func RedactEndpoint(rawURL string) string {
	parsed, err := url.Parse(strings.TrimSpace(rawURL))
	if err != nil {
		return "(invalid)"
	}
	parsed.User = nil
	parsed.RawQuery = ""
	parsed.Fragment = ""
	return parsed.String()
}

func errorsJoin(errs []error) error {
	switch len(errs) {
	case 0:
		return nil
	case 1:
		return errs[0]
	default:
		messages := make([]string, 0, len(errs))
		for _, err := range errs {
			if err != nil {
				messages = append(messages, err.Error())
			}
		}
		return fmt.Errorf("%s", strings.Join(messages, "; "))
	}
}
