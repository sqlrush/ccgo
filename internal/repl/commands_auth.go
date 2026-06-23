package repl

import (
	"context"
	"fmt"

	"ccgo/internal/auth"
	"ccgo/internal/tui"
)

// LoginFlowFunc is the DI seam for running an OAuth login flow.
// Production code uses auth.RunLoginFlow; tests inject a stub.
type LoginFlowFunc func(ctx context.Context, opts auth.LoginOptions) (auth.Credentials, error)

// loginHandlerWith returns a CommandHandler for /login backed by the given
// flow function and credential store. On success it reports the login outcome;
// on failure it surfaces the error as a Status message rather than propagating
// so the REPL loop stays alive.
func loginHandlerWith(flow LoginFlowFunc, store auth.CredentialStore) CommandHandler {
	return func(ctx context.Context, cc CommandContext) (CommandOutcome, error) {
		_, err := flow(ctx, auth.LoginOptions{
			Browser: auth.NewOSBrowserOpener(),
			Store:   store,
			// Default to claude.ai (same as `claude auth login` default).
			LoginWithClaudeAI: true,
			OnURL: func(u string) {
				// Surface the URL as a status message so the user can paste it
				// if the browser did not open.
				if cc.Screen != nil {
					cc.Screen.AppendMessage(tui.Message{
						Role: tui.RoleSystem,
						Text: fmt.Sprintf("If your browser did not open, visit:\n%s", u),
					})
				}
			},
		})
		if err != nil {
			return CommandOutcome{
				Handled: true,
				Status:  fmt.Sprintf("Login failed: %v", err),
			}, nil
		}
		return CommandOutcome{
			Handled: true,
			Status:  "Login successful.",
		}, nil
	}
}

// loginHandler is the production handler that uses the real OAuth flow and the
// OS-native credential store.
func loginHandler() CommandHandler {
	store := auth.NewDefaultCredentialStore()
	return loginHandlerWith(auth.RunLoginFlow, store)
}

// logoutHandlerWith returns a CommandHandler for /logout backed by the given
// credential store. On success it reports sign-out; on failure it surfaces the
// error as a Status message rather than propagating.
func logoutHandlerWith(store auth.CredentialStore) CommandHandler {
	return func(ctx context.Context, cc CommandContext) (CommandOutcome, error) {
		if err := store.Delete(ctx); err != nil {
			return CommandOutcome{
				Handled: true,
				Status:  fmt.Sprintf("Logout failed: %v", err),
			}, nil
		}
		return CommandOutcome{
			Handled: true,
			Status:  "Signed out. Stored credentials removed.",
		}, nil
	}
}

// logoutHandler is the production handler that uses the OS-native credential store.
func logoutHandler() CommandHandler {
	return logoutHandlerWith(auth.NewDefaultCredentialStore())
}
