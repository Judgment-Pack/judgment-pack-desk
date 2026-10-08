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
	// The request's line, whole, before anything is started or written: a
	// line past what the companion reads for this method is refused here,
	// and the companion is sent none of it.
	id, err := randomStagingName("rpc-")
	if err != nil {
		return nil, err
	}
	line, err := json.Marshal(map[string]any{"id": id, "method": method, "params": params})
	if err != nil {
		return nil, err
	}
	if bound := companionRequestBound(provider, method); bound > 0 && len(line) > bound {
		return nil, errCompanionRequestTooLarge
	}
	if c.cmd == nil {
		if err := verifyGatewayBundle(bundle); err != nil {
			return nil, err
		}
		c.cmd = exec.Command(filepath.Join(bundle, executableName("gateway-connections")), "--state-dir", dir, "--principal", "desk-local", "--provider", provider)
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
	if _, err = c.input.Write(append(line, '\n')); err != nil {
		c.stop()
		return nil, errors.New("gateway connection service unavailable")
	}
	type response struct {
		raw []byte
		err error
	}
	reply := make(chan response, 1)
	limit := 64 << 10
	if method == "files-read" {
		limit = 6 << 20
	} else if method == "files-list" {
		limit = 512 << 10
	}
	go func(reader *bufio.Reader) {
		raw, err := readConnectionLine(reader, limit)
		reply <- response{raw, err}
	}(c.output)
	select {
	case r := <-reply:
		if r.err != nil || len(r.raw) > limit {
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
	if !catalogIdentifier.MatchString(provider) {
		writeJSONCoded(w, 400, CodeBadRequest, "unknown connection provider")
		return
	}
	// Document processing has its own route, which rebuilds the companion's
	// answers without credentials and holds them to what was sent; this
	// relay passes answers through as they are. It is refused by name, not
	// left to the catalogs' not listing it.
	if provider == "document-processing" {
		writeJSONCoded(w, 400, CodeBadRequest, "document processing has its own route; nothing was sent")
		return
	}

	switch method {
	case "catalog", "status", "configure", "connect", "poll", "cancel", "disconnect", "search", "select", "files-list", "files-read", "files-prepare", "files-commit", "files-status", "test":
	default:
		writeJSONCoded(w, 400, CodeBadRequest, "unknown connection operation")
		return
	}
	if r.URL.RawQuery != "" {
		writeJSONCoded(w, 400, CodeBadRequest, "connection operations carry no query")
		return
	}
	bodyLimit := int64(32 << 10)
	if method == "files-prepare" {
		bodyLimit = 6 << 20
	}
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, bodyLimit))
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
	timeout := 55 * time.Second
	if provider == "web-search" && method == "test" {
		timeout = 130 * time.Second
	}
	ctx, cancel := context.WithTimeout(r.Context(), timeout)
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

// errCompanionRequestTooLarge is a request whose line the companion would
// refuse for its length; nothing of it was sent.
var errCompanionRequestTooLarge = errors.New("connection request too large; nothing was sent")

// companionRequestBound is the most a request's line may hold, its ending
// left out, where Desk holds the line to the companion's own bound (gateway
// v0.10.0: 6 MiB for a request that carries a document, 64 KiB for every
// other), or 0 where the companion alone holds it. The document-processing
// companion's lines are held here: one of them carries a credential, and a
// test carries a PDF.
func companionRequestBound(provider, method string) int {
	if provider != "document-processing" {
		return 0
	}
	if method == "test" {
		return 6 << 20
	}
	return 64 << 10
}

// Read a bounded line without allocating the file-response limit for ordinary RPCs.
func readConnectionLine(reader *bufio.Reader, limit int) ([]byte, error) {
	var raw []byte
	for {
		part, err := reader.ReadSlice('\n')
		if len(raw)+len(part) > limit {
			return nil, errors.New("connection response too large")
		}
		raw = append(raw, part...)
		if err == bufio.ErrBufferFull {
			continue
		}
		return raw, err
	}
}
