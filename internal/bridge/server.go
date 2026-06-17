package bridge

import (
	"context"
	"crypto/subtle"
	"errors"
	"fmt"
	"net"
	"net/http"
	"strings"
	"time"
)

const defaultDirectListenAddr = "127.0.0.1:0"

type DirectServerOptions struct {
	Addr              string
	Token             string
	Handler           http.Handler
	ReadHeaderTimeout time.Duration
}

type DirectServer struct {
	server   *http.Server
	listener net.Listener
	errs     chan error
	url      string
}

func StartDirectServer(opts DirectServerOptions) (*DirectServer, error) {
	if opts.Handler == nil {
		return nil, errors.New("bridge direct handler is required")
	}
	addr := strings.TrimSpace(opts.Addr)
	if addr == "" {
		addr = defaultDirectListenAddr
	}
	if err := validateLoopbackListenAddr(addr); err != nil {
		return nil, err
	}
	listener, err := net.Listen("tcp", addr)
	if err != nil {
		return nil, err
	}
	handler := opts.Handler
	if token := strings.TrimSpace(opts.Token); token != "" {
		handler = DirectTokenGuard(handler, token)
	}
	timeout := opts.ReadHeaderTimeout
	if timeout <= 0 {
		timeout = 5 * time.Second
	}
	server := &http.Server{
		Handler:           handler,
		ReadHeaderTimeout: timeout,
	}
	direct := &DirectServer{
		server:   server,
		listener: listener,
		errs:     make(chan error, 1),
		url:      "http://" + listener.Addr().String(),
	}
	go func() {
		err := server.Serve(listener)
		if err != nil && !errors.Is(err, http.ErrServerClosed) {
			direct.errs <- err
		}
		close(direct.errs)
	}()
	return direct, nil
}

func (s *DirectServer) URL() string {
	if s == nil {
		return ""
	}
	return s.url
}

func (s *DirectServer) Addr() net.Addr {
	if s == nil || s.listener == nil {
		return nil
	}
	return s.listener.Addr()
}

func (s *DirectServer) Errors() <-chan error {
	if s == nil {
		ch := make(chan error)
		close(ch)
		return ch
	}
	return s.errs
}

func (s *DirectServer) Close(ctx context.Context) error {
	if s == nil || s.server == nil {
		return nil
	}
	if ctx == nil {
		ctx = context.Background()
	}
	return s.server.Shutdown(ctx)
}

func DirectTokenGuard(next http.Handler, token string) http.Handler {
	token = strings.TrimSpace(token)
	if token == "" {
		return next
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if directTokenAllowed(r, token) {
			next.ServeHTTP(w, r)
			return
		}
		writeDirectError(w, http.StatusUnauthorized, "bridge direct token is required")
	})
}

func directTokenAllowed(r *http.Request, token string) bool {
	presented := strings.TrimSpace(r.Header.Get("X-Bridge-Token"))
	if presented == "" {
		presented = bearerToken(r.Header.Get("Authorization"))
	}
	if presented == "" {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(presented), []byte(token)) == 1
}

func bearerToken(header string) string {
	header = strings.TrimSpace(header)
	if header == "" {
		return ""
	}
	kind, value, ok := strings.Cut(header, " ")
	if !ok || !strings.EqualFold(kind, "Bearer") {
		return ""
	}
	return strings.TrimSpace(value)
}

func validateLoopbackListenAddr(addr string) error {
	host, _, err := net.SplitHostPort(addr)
	if err != nil {
		return fmt.Errorf("bridge direct listen address %q must be host:port: %w", addr, err)
	}
	host = strings.Trim(host, "[]")
	if host == "" {
		return fmt.Errorf("bridge direct listen address %q must specify a loopback host", addr)
	}
	if strings.EqualFold(host, "localhost") {
		return nil
	}
	ip := net.ParseIP(host)
	if ip == nil || !ip.IsLoopback() {
		return fmt.Errorf("bridge direct listen address %q must use a loopback host", addr)
	}
	return nil
}
