package auth

import (
	"errors"
	"fmt"
	"net/url"
	"os"
	"os/exec"
	"runtime"
)

var errInvalidBrowserURL = errors.New("auth: browser url must be http or https")

// BrowserOpener opens a URL in the user's default browser. Injected so tests
// never launch a real browser.
type BrowserOpener interface {
	Open(url string) error
}

// osBrowserOpener launches the platform browser command. runner is a seam for
// tests; in production it execs the command.
type osBrowserOpener struct {
	runner func(name string, args ...string) error
}

// NewOSBrowserOpener returns a BrowserOpener backed by the OS browser command.
func NewOSBrowserOpener() *osBrowserOpener {
	return &osBrowserOpener{runner: func(name string, args ...string) error {
		return exec.Command(name, args...).Start()
	}}
}

func (o *osBrowserOpener) Open(raw string) error {
	if err := validateBrowserURL(raw); err != nil {
		return err
	}
	// Honor $BROWSER like CC does, when it names a single command.
	if custom := os.Getenv("BROWSER"); custom != "" {
		return o.runner(custom, raw)
	}
	name, args := browserCommand(runtime.GOOS, raw)
	return o.runner(name, args...)
}

// validateBrowserURL rejects anything but http/https to avoid passing a
// file:// or javascript: URL to the OS opener.
func validateBrowserURL(raw string) error {
	u, err := url.Parse(raw)
	if err != nil {
		return fmt.Errorf("%w: %v", errInvalidBrowserURL, err)
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return errInvalidBrowserURL
	}
	return nil
}

// browserCommand returns the OS-specific command + args to open url. Pure.
func browserCommand(goos string, url string) (string, []string) {
	switch goos {
	case "darwin":
		return "open", []string{url}
	case "windows":
		// rundll32 url.dll,FileProtocolHandler <url>
		return "rundll32", []string{"url.dll,FileProtocolHandler", url}
	default:
		return "xdg-open", []string{url}
	}
}
