package repl

import (
	"context"
	"errors"
	"testing"

	"ccgo/internal/auth"
)

// --- stubs ---

type stubLoginFunc struct {
	called bool
	err    error
	creds  auth.Credentials
}

func (s *stubLoginFunc) RunLoginFlow(ctx context.Context, opts auth.LoginOptions) (auth.Credentials, error) {
	s.called = true
	return s.creds, s.err
}

type stubStore struct {
	deleteCalled bool
	deleteErr    error
}

func (s *stubStore) Load(ctx context.Context) (auth.Credentials, error) { return auth.Credentials{}, nil }
func (s *stubStore) Save(ctx context.Context, c auth.Credentials) error  { return nil }
func (s *stubStore) Delete(ctx context.Context) error {
	s.deleteCalled = true
	return s.deleteErr
}

// --- login handler tests ---

func TestLoginHandlerCallsLoginFlow(t *testing.T) {
	stub := &stubLoginFunc{creds: auth.Credentials{Source: auth.SourceOAuth}}
	h := loginHandlerWith(stub.RunLoginFlow, &stubStore{})

	out, err := h(context.Background(), CommandContext{})
	if err != nil {
		t.Fatalf("loginHandler error: %v", err)
	}
	if !out.Handled {
		t.Fatal("Handled must be true")
	}
	if !stub.called {
		t.Fatal("RunLoginFlow was not called")
	}
	if out.Status == "" {
		t.Fatal("Status must be non-empty after successful login")
	}
}

func TestLoginHandlerReportsError(t *testing.T) {
	loginErr := errors.New("oauth failed")
	stub := &stubLoginFunc{err: loginErr}
	h := loginHandlerWith(stub.RunLoginFlow, &stubStore{})

	out, err := h(context.Background(), CommandContext{})
	// handler should NOT return an error to the loop; it surfaces it as Status
	if err != nil {
		t.Fatalf("loginHandler should not propagate error, got: %v", err)
	}
	if !out.Handled {
		t.Fatal("Handled must be true even on login failure")
	}
	if out.Status == "" {
		t.Fatal("Status must convey the error message")
	}
}

// --- logout handler tests ---

func TestLogoutHandlerClearsCreds(t *testing.T) {
	store := &stubStore{}
	h := logoutHandlerWith(store)

	out, err := h(context.Background(), CommandContext{})
	if err != nil {
		t.Fatalf("logoutHandler error: %v", err)
	}
	if !out.Handled {
		t.Fatal("Handled must be true")
	}
	if !store.deleteCalled {
		t.Fatal("Delete was not called")
	}
	if out.Status == "" {
		t.Fatal("Status must confirm logout")
	}
}

func TestLogoutHandlerReportsDeleteError(t *testing.T) {
	deleteErr := errors.New("keychain locked")
	store := &stubStore{deleteErr: deleteErr}
	h := logoutHandlerWith(store)

	out, err := h(context.Background(), CommandContext{})
	if err != nil {
		t.Fatalf("logoutHandler should not propagate error, got: %v", err)
	}
	if !out.Handled {
		t.Fatal("Handled must be true even on delete failure")
	}
	if out.Status == "" {
		t.Fatal("Status must convey the error message")
	}
}

// TestProductionRouterRegistersLoginLogout ensures login/logout are registered
// in the production router so they are not silently dropped.
func TestProductionRouterRegistersLoginLogout(t *testing.T) {
	router := newProductionRouter("", nil)
	names := make(map[string]struct{})
	for _, n := range router.Names() {
		names[n] = struct{}{}
	}
	for _, want := range []string{"login", "logout"} {
		if _, ok := names[want]; !ok {
			t.Errorf("production router is missing %q command", want)
		}
	}
}
