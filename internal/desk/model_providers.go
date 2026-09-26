package desk

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"mime"
	"net/http"
	"path/filepath"

	"github.com/Judgment-Pack/judgment-pack-desk/internal/codexbridge"
)

// Narrow account interface also keeps HTTP tests independent of native login.
// No arbitrary RPC, token, executable, provider URL or tool capability crosses it.
type providerAccountManager interface {
	Status(context.Context, string, bool) (codexbridge.Status, error)
	StartLogin(context.Context, codexbridge.Owner, string) (codexbridge.Challenge, error)
	CancelLogin(context.Context, string, string) error
	Logout(context.Context) error
	Close()
}

func (s *Server) initModelProviders() {
	if s.cfg.CodexBin == "off" || !s.assistant.usable() {
		return
	}
	if s.cfg.CodexBin == "" && !codexbridge.ManagedRuntimeSupported() {
		return
	}
	s.codex = codexbridge.NewLazyManager(codexbridge.Options{
		Binary: s.cfg.CodexBin, ProfileDir: filepath.Join(s.configDir, "codex"), ProjectDir: s.projectDir,
	})
	// No profile lease, subprocess or download until an account operation.

}

func providerReply(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Referrer-Policy", "no-referrer")
	writeJSON(w, status, body)
}
func providerFailure(w http.ResponseWriter, err error) {
	code, status := "provider-unavailable", http.StatusServiceUnavailable
	switch {
	case errors.Is(err, codexbridge.ErrInstall):
		code = "runtime-install-failed"
	case errors.Is(err, codexbridge.ErrSignIn):
		code, status = "sign-in-required", http.StatusConflict
	case errors.Is(err, codexbridge.ErrBusy):
		code, status = "provider-busy", http.StatusConflict
	case errors.Is(err, codexbridge.ErrAttempt):
		code, status = "login-unavailable", http.StatusConflict
	case errors.Is(err, codexbridge.ErrConnected):
		code, status = "already-connected", http.StatusConflict
	case errors.Is(err, context.Canceled), errors.Is(err, context.DeadlineExceeded):
		code, status = "operation-canceled", http.StatusRequestTimeout
	}
	providerReply(w, status, map[string]string{"error": code})
}
func providerJSON(w http.ResponseWriter, r *http.Request, to any) bool {
	media, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if err != nil || media != "application/json" {
		providerReply(w, 415, map[string]string{"error": "invalid-request"})
		return false
	}
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1024))
	decoder.DisallowUnknownFields()
	if decoder.Decode(to) != nil || decoder.Decode(new(any)) != io.EOF {
		providerReply(w, 400, map[string]string{"error": "invalid-request"})
		return false
	}
	return true
}

func (s *Server) handleModelProviders(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Referrer-Policy", "no-referrer")
	if !s.guard(w, r) {
		return
	}
	held, ok := s.sessionOf(r)
	if !ok {
		providerReply(w, 401, map[string]string{"error": "browser-session-required"})
		return
	}
	// Only minted browser sessions own login work. Policy/session revocation
	// cancels a pending challenge after this HTTP request has returned.
	s.signIn.mu.Lock()
	epoch := s.signIn.epoch
	s.signIn.mu.Unlock()
	owner := codexbridge.Owner{ID: s.sessions.handle(bearerOf(r)), Session: held.ctx, Policy: epoch}
	action := r.PathValue("action")
	catalog := r.URL.Path == "/api/model-providers"
	if r.URL.RawQuery != "" || (catalog && r.Method != http.MethodGet) {
		providerReply(w, 400, map[string]string{"error": "invalid-request"})
		return
	}
	if catalog {
		availability := "available"
		if s.cfg.CodexBin == "off" {
			availability = "disabled"
		} else if s.cfg.CodexBin == "" && !codexbridge.ManagedRuntimeSupported() {
			availability = "unsupported"
		} else if s.codex == nil {
			availability = "unavailable"
		}
		providerReply(w, 200, map[string]any{"providers": []any{map[string]any{
			"id": "openai", "authMethod": "subscription", "agent": "codex",
			"configured": s.codex != nil, "enabled": s.codex != nil,
			"availability": availability, "engineReady": s.codex != nil, "requiredVersion": codexbridge.Version,
			"loginMethods": []string{"browser", "device"},
		}}})
		return
	}
	switch action {
	case "models":
		if r.Method != http.MethodGet {
			providerReply(w, 405, map[string]string{"error": "method"})
			return
		}
		lister, ok := s.codex.(interface {
			Models(context.Context) ([]codexbridge.Model, error)
		})
		if !ok {
			providerFailure(w, codexbridge.ErrUnavailable)
			return
		}
		ctx, cancel := context.WithCancel(r.Context())
		defer cancel()
		stopSession := context.AfterFunc(held.ctx, cancel)
		defer stopSession()
		stopPolicy := context.AfterFunc(epoch, cancel)
		defer stopPolicy()
		models, err := lister.Models(ctx)
		if err != nil {
			providerFailure(w, err)
			return
		}
		providerReply(w, 200, map[string]any{"models": models})
		return
	case "status":
		if r.Method != http.MethodGet {
			w.Header().Set("Allow", "GET")
			providerReply(w, 405, map[string]string{"error": "method"})
			return
		}
	case "login", "cancel", "logout", "refresh":
		if r.Method != http.MethodPost {
			w.Header().Set("Allow", "POST")
			providerReply(w, 405, map[string]string{"error": "method"})
			return
		}
		if _, ok = s.signInBrowser(w, r); !ok {
			return
		}
	default:
		providerReply(w, 404, map[string]string{"error": "not-found"})
		return
	}
	if s.codex == nil {
		providerFailure(w, codexbridge.ErrUnavailable)
		return
	}
	switch action {
	case "status":
		status, err := s.codex.Status(r.Context(), owner.ID, false)
		if err != nil {
			providerFailure(w, err)
			return
		}
		providerReply(w, 200, status)
	case "login":
		var request struct {
			Method string `json:"method"`
		}
		if !providerJSON(w, r, &request) {
			return
		}
		if request.Method != "browser" && request.Method != "device" {
			providerReply(w, 400, map[string]string{"error": "invalid-login-method"})
			return
		}
		challenge, err := s.codex.StartLogin(r.Context(), owner, request.Method)
		if err != nil {
			providerFailure(w, err)
			return
		}
		providerReply(w, 200, challenge)
	case "cancel":
		var request struct {
			ID string `json:"id"`
		}
		if !providerJSON(w, r, &request) {
			return
		}
		if request.ID == "" || len(request.ID) > 64 {
			providerReply(w, 400, map[string]string{"error": "invalid-request"})
			return
		}
		if err := s.codex.CancelLogin(r.Context(), owner.ID, request.ID); err != nil {
			providerFailure(w, err)
			return
		}
		providerReply(w, 200, map[string]string{"state": "canceled"})
	case "logout", "refresh":
		var request struct{}
		if !providerJSON(w, r, &request) {
			return
		}
		if action == "logout" {
			if err := s.codex.Logout(r.Context()); err != nil {
				providerFailure(w, err)
				return
			}
			providerReply(w, 200, map[string]string{"state": "signed-out"})
		} else {
			status, err := s.codex.Status(r.Context(), owner.ID, true)
			if err != nil {
				providerFailure(w, err)
				return
			}
			providerReply(w, 200, status)
		}
	}
}
