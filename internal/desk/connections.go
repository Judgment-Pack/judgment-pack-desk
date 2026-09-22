package desk

// Provider operations are opaque RPCs to the gateway companion. This relay is
// needed because the browser cannot hold the private pipe or provider tokens.
// It neither implements OAuth nor sends requests to a provider itself.
import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"os/exec"
	"path/filepath"
	"sync"
	"time"
)

type connectionCompanion struct {
	mu     sync.Mutex
	cmd    *exec.Cmd
	input  io.WriteCloser
	output *bufio.Reader
	done   chan struct{}
	closed bool
}

func (c *connectionCompanion) stop() {
	if c.input != nil {
		c.input.Close()
	}
	if c.done != nil {
		select {
		case <-c.done:
		case <-time.After(2 * time.Second):
			c.cmd.Process.Kill()
			<-c.done
		}
	}
	c.cmd = nil
	c.input = nil
	c.output = nil
	c.done = nil
}
func (c *connectionCompanion) close() { c.mu.Lock(); defer c.mu.Unlock(); c.closed = true; c.stop() }
func (c *connectionCompanion) call(ctx context.Context, bundle, dir, method string, params json.RawMessage, provider string, existingOnly bool) (json.RawMessage, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}
	if c.closed {
		return nil, errors.New("connections are closed")
	}
	if c.done != nil {
		select {
		case <-c.done:
			c.stop()
		default:
		}
	}
	if c.cmd == nil && existingOnly {
		return json.RawMessage(`{"state":"canceled"}`), nil
	}
	if c.cmd == nil {
		if err := verifyGatewayBundle(bundle); err != nil {
			return nil, err
		}
		c.cmd = exec.Command(filepath.Join(bundle, executableName("gateway-connections")), "--state-dir", dir, "--principal", "desk-local", "--provider", provider)
		var err error
		c.input, err = c.cmd.StdinPipe()
		if err != nil {
			c.stop()
			return nil, err
		}
		pipe, err := c.cmd.StdoutPipe()
		if err != nil {
			c.stop()
			return nil, err
		}
		c.output = bufio.NewReaderSize(pipe, 64<<10)
		c.cmd.Stderr = io.Discard
		if err = c.cmd.Start(); err != nil {
			c.stop()
			return nil, err
		}
		c.done = make(chan struct{})
		go func(cmd *exec.Cmd, done chan struct{}) { _ = cmd.Wait(); close(done) }(c.cmd, c.done)
	}
	id, err := randomStagingName("rpc-")
	if err != nil {
		return nil, err
	}
	if err = json.NewEncoder(c.input).Encode(map[string]any{"id": id, "method": method, "params": params}); err != nil {
		c.stop()
		return nil, errors.New("gateway connection service unavailable")
	}
	type response struct {
		raw []byte
		err error
	}
	reply := make(chan response, 1)
	go func(reader *bufio.Reader) { raw, err := reader.ReadSlice('\n'); reply <- response{raw, err} }(c.output)
	select {
	case r := <-reply:
		if r.err != nil || len(r.raw) > 64<<10 {
			c.stop()
			return nil, errors.New("gateway connection service unavailable")
		}
		var envelope struct {
			ID     string          `json:"id"`
			Result json.RawMessage `json:"result"`
			Error  string          `json:"error"`
		}
		if json.Unmarshal(r.raw, &envelope) != nil || envelope.ID != id {
			c.stop()
			return nil, errors.New("invalid gateway connection response")
		}
		if envelope.Error != "" {
			return json.Marshal(map[string]string{"error": envelope.Error})
		}
		return envelope.Result, nil
	case <-ctx.Done():
		// The provider may already have rotated a refresh token. Let the
		// companion finish its bounded operation and durable commit before
		// closing it. Drain the reply under the mutex so it can never be
		// mistaken for another request's response. The browser is free to leave.
		select {
		case <-reply:
		case <-time.After(65 * time.Second):
		}
		c.stop()
		return nil, errors.New("gateway connection request canceled")
	}
}
func (s *Server) handleConnections(w http.ResponseWriter, r *http.Request) {
	if !s.guard(w, r) || s.refuseUnusableStore(w) {
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	method := r.PathValue("method")
	provider := r.PathValue("provider")
	if method == "catalog" && provider != "" {
		writeJSONCoded(w, 400, CodeBadRequest, "unknown connection operation")
		return
	}
	if provider == "" {
		provider = "google-drive"
	}
	// Preserve legacy endpoint refusals even when no companion is available.
	if method == "pick" && (provider == "gmail" || provider == "notion" || provider == "obsidian") || (method == "search" || method == "select") && provider == "google-drive" {
		writeJSONCoded(w, 400, CodeBadRequest, "unknown connection operation")
		return
	}
	if !catalogIdentifier.MatchString(provider) {
		writeJSONCoded(w, 400, CodeBadRequest, "unknown connection provider")
		return
	}

	switch method {
	case "catalog", "status", "configure", "connect", "pick", "poll", "cancel", "disconnect", "search", "select":
	default:
		writeJSONCoded(w, 400, CodeBadRequest, "unknown connection operation")
		return
	}
	if r.URL.RawQuery != "" {
		writeJSONCoded(w, 400, CodeBadRequest, "connection operations carry no query")
		return
	}
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, 32<<10))
	if err != nil {
		writeJSONCoded(w, 413, CodeTooLarge, "connection request too large")
		return
	}
	if len(body) == 0 {
		body = []byte("{}")
	}
	var obj map[string]json.RawMessage
	if decodeDataJSON(body, &obj) != nil {
		writeJSONCoded(w, 400, CodeBadRequest, "invalid connection request")
		return
	}
	if method == "catalog" && (obj == nil || len(obj) != 0) {
		writeJSONCoded(w, 400, CodeBadRequest, "catalog takes no parameters")
		return
	}
	bundle, directory := "", ""
	// Cleanup refers only to a flow this server already owns. It remains
	// available after a settings change and must never start a companion.
	if method != "cancel" {
		// No implicit fallback from a configured organization/external gateway.
		_, raw, err := s.readDeskFile()
		if err != nil {
			writeJSONCoded(w, 409, CodeBadRequest, "connection settings unavailable")
			return
		}
		status := s.localGatewayStatus(raw)
		if s.localGateway == nil || status == nil || status.Status != "ready" {
			if method != "status" {
				writeJSONCoded(w, 503, CodeBadRequest, "gateway connection service unavailable")
				return
			}
			writeJSON(w, 200, map[string]any{"version": 1, "provider": provider, "state": "unavailable", "maxFileBytes": 4 << 20, "maxFiles": 4})
			return
		}
		if method == "search" || method == "select" {
			gateway, err := s.configuredResearch()
			if err != nil || !gateway.managedLocal || gateway.maxFileBytes == 0 {
				writeJSONCoded(w, http.StatusConflict, CodeResearchUnconfigured, "local document processing is no longer available; nothing was sent")
				return
			}
		}
		bundle = s.localGateway.bundle
		directory = filepath.Join(s.assistant.dir, "gateway-connections")
	}
	ctx, cancel := context.WithTimeout(r.Context(), 55*time.Second)
	defer cancel()
	var out json.RawMessage
	if method == "catalog" {
		out, err = readConnectionCatalog(ctx, bundle)
	} else {
		if method != "cancel" {
			catalogRaw, catalogErr := readConnectionCatalog(ctx, bundle)
			var catalog connectionCatalog
			if catalogErr != nil || json.Unmarshal(catalogRaw, &catalog) != nil {
				writeJSONCoded(w, 503, CodeBadRequest, "gateway connection service unavailable")
				return
			}
			allowed := false
			for _, descriptor := range catalog.Providers {
				if descriptor.ID == provider {
					for _, operation := range descriptor.Operations {
						allowed = allowed || operation == method
					}
				}
			}
			if !allowed {
				writeJSONCoded(w, 400, CodeBadRequest, "unknown connection operation")
				return
			}
		}
		companion := s.connectionCompanion(provider, method != "cancel")
		if companion == nil {
			out = json.RawMessage(`{"state":"canceled"}`)
		} else {
			out, err = companion.call(ctx, bundle, directory, method, body, provider, method == "cancel")
		}
	}
	if err != nil {
		writeJSONCoded(w, 503, CodeBadRequest, "gateway connection service unavailable")
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(200)
	_, _ = w.Write(out)
}

func (s *Server) connectionCompanion(provider string, create bool) *connectionCompanion {
	s.providerMu.Lock()
	defer s.providerMu.Unlock()
	if s.providersClosed {
		return nil
	}
	// Existing fields preserve ownership for callers of the legacy endpoints.
	switch provider {
	case "google-drive":
		return &s.connections
	case "gmail":
		return &s.gmailConnections
	case "notion":
		return &s.notionConnections
	case "obsidian":
		return &s.obsidianConnections
	}
	if s.providerConnections == nil {
		s.providerConnections = map[string]*connectionCompanion{}
	}
	c := s.providerConnections[provider]
	if c == nil && create && len(s.providerConnections) < 32 {
		c = &connectionCompanion{}
		s.providerConnections[provider] = c
	}
	return c
}
