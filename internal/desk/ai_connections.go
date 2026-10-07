package desk

// AI connection identity is independent of the desk that selects it. Registry
// and credentials use the existing pinned custody store, never a project path.
import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"path/filepath"
	"regexp"
	"strings"
	"sync"

	"github.com/Judgment-Pack/judgment-pack-desk/internal/codexbridge"
)

const aiConnectionsFile = "ai-connections.json"

// maxAIRegistryBytes bounds the registry on both ends: the reader refuses a
// file larger than this, so the writer refuses to write one. The bytes written
// are indented and can be longer than the request that asked for them.
const maxAIRegistryBytes = 1 << 20

// testHookAfterAIRegistryWrite runs after a registry write has been renamed
// into place and before it is read back, and is nil outside tests.
var testHookAfterAIRegistryWrite func(path string)

func afterAIRegistryWrite(path string) {
	if testHookAfterAIRegistryWrite != nil {
		testHookAfterAIRegistryWrite(path)
	}
}

const aiConnectionHeader = "X-Assistant-Connection"
const aiRevisionHeader = "X-Assistant-Revision"

var aiIDPattern = regexp.MustCompile(`^(legacy-api|legacy-codex|ai-[a-f0-9]{24})$`)

type aiConnection struct {
	ID        string            `json:"id"`
	Name      string            `json:"name"`
	Enabled   bool              `json:"enabled"`
	Assistant AssistantSlotView `json:"assistant"`
	Revision  string            `json:"revision,omitempty"`
}
type aiRegistry struct {
	Version           int            `json:"version"`
	DefaultConnection string         `json:"defaultConnection"`
	Connections       []aiConnection `json:"connections"`
}
type aiRegistryReply struct {
	aiRegistry
	SHA256 string `json:"sha256"`
	Path   string `json:"path"`
}
type aiManagers struct {
	mu     sync.Mutex
	closed bool
	values map[string]providerAccountManager
}

// Reject ambiguous JSON at every level, including duplicate members in raw
// assistant configuration. Bound depth before walking user-controlled input.
func uniqueJSON(data []byte) bool {
	d := json.NewDecoder(bytes.NewReader(data))
	var walk func(int) bool
	walk = func(depth int) bool {
		if depth > 32 {
			return false
		}
		t, e := d.Token()
		if e != nil {
			return false
		}
		delim, ok := t.(json.Delim)
		if !ok {
			return true
		}
		switch delim {
		case '{':
			seen := map[string]bool{}
			for d.More() {
				k, e := d.Token()
				s, ok := k.(string)
				if e != nil || !ok || seen[s] {
					return false
				}
				seen[s] = true
				if !walk(depth + 1) {
					return false
				}
			}
			end, e := d.Token()
			return e == nil && end == json.Delim('}')
		case '[':
			for d.More() {
				if !walk(depth + 1) {
					return false
				}
			}
			end, e := d.Token()
			return e == nil && end == json.Delim(']')
		}
		return false
	}
	return walk(0) && func() bool { _, e := d.Token(); return e == io.EOF }()
}
func decodeAIRegistry(data []byte) (aiRegistry, error) {
	var value aiRegistry
	fail := func() (aiRegistry, error) {
		return value, errors.New("AI connections are invalid. Reload Connections > AI or repair ai-connections.json.")
	}
	if len(data) > maxAIRegistryBytes || !validUTF8(data) || !uniqueJSON(data) {
		return fail()
	}
	d := json.NewDecoder(bytes.NewReader(data))
	d.DisallowUnknownFields()
	if d.Decode(&value) != nil || value.Version != 1 || value.Connections == nil || len(value.Connections) > 32 {
		return fail()
	}
	seen := map[string]bool{}
	defaultFound := value.DefaultConnection == ""
	for i := range value.Connections {
		c := &value.Connections[i]
		if !aiIDPattern.MatchString(c.ID) || seen[c.ID] || c.Name == "" || strings.TrimSpace(c.Name) != c.Name || len(c.Name) > 128 || strings.IndexFunc(c.Name, isControl) >= 0 {
			return fail()
		}
		seen[c.ID] = true
		raw, _ := json.Marshal(c.Assistant)
		decoded := decodeDeskFile(append(append([]byte(`{"deskConfigVersion":1,"assistant":`), raw...), '}'))
		if decoded.refused() || decoded.Engine == "codex" && (decoded.Agent == nil || decoded.Endpoint != nil) || decoded.Engine == "vercel" && (decoded.Endpoint == nil || decoded.Agent != nil) {
			return fail()
		}
		if c.ID == "legacy-api" && decoded.Engine != "vercel" || c.ID == "legacy-codex" && decoded.Engine != "codex" {
			return fail()
		}
		c.Assistant = slotView(decoded)
		c.Revision = aiRevision(*c)
		if c.ID == value.DefaultConnection && c.Enabled {
			defaultFound = true
		}
	}
	if !defaultFound {
		return fail()
	}
	return value, nil
}

// encodeAIRegistry is the bytes a registry is written as, refused where its
// own reader would refuse them: the indented file can be longer than the
// request that asked for it, and a file the reader refuses is a registry no
// desk can read again.
func encodeAIRegistry(value aiRegistry) ([]byte, error) {
	written, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return nil, err
	}
	written = append(written, '\n')
	if len(written) > maxAIRegistryBytes {
		return nil, errors.New("AI connections exceed the size limit once written; nothing was written.")
	}
	return written, nil
}

func aiRevision(c aiConnection) string {
	raw, _ := json.Marshal(struct {
		Enabled   bool
		Assistant AssistantSlotView
	}{c.Enabled, c.Assistant})
	return digestOf(raw)
}
func (s *Server) readAIRegistry() (aiRegistryReply, error) {
	out := aiRegistryReply{Path: filepath.Join(s.configDir, aiConnectionsFile)}
	present, data, err := s.assistant.readConfigNamed(aiConnectionsFile)
	if err != nil {
		return out, err
	}
	if present {
		out.aiRegistry, err = decodeAIRegistry(data)
		out.SHA256 = digestOf(data)
		return out, err
	}
	// Lazy migration: opening Admin performs no write or authentication operation.
	// Fixed legacy IDs preserve the existing private key and native account path.
	out.aiRegistry = aiRegistry{Version: 1, Connections: []aiConnection{}}
	present, data, err = s.readDeskFile()
	if err != nil {
		return out, err
	}
	if present {
		if !validUTF8(data) {
			return out, errors.New("Shared AI settings are not UTF-8 text.")
		}
		d := decodeDeskFile(data)
		if d.refused() {
			return out, errors.New("Shared AI settings could not be read. Repair desk.json before adding connections.")
		}
		v := slotView(d)
		if v.Endpoint != nil {
			a := v
			a.Engine = "vercel"
			a.Agent = nil
			out.Connections = append(out.Connections, aiConnection{ID: "legacy-api", Name: "API connection", Enabled: true, Assistant: a})
		}
		if v.Agent != nil {
			a := v
			a.Engine = "codex"
			a.Endpoint = nil
			out.Connections = append(out.Connections, aiConnection{ID: "legacy-codex", Name: "ChatGPT", Enabled: true, Assistant: a})
		}
		for _, c := range out.Connections {
			if c.Assistant.Engine == d.Engine {
				out.DefaultConnection = c.ID
			}
		}
	}
	for i := range out.Connections {
		out.Connections[i].Revision = aiRevision(out.Connections[i])
	}
	raw, _ := json.Marshal(out.aiRegistry)
	out.SHA256 = digestOf(raw)
	return out, nil
}
func (s *Server) handleAIConnections(w http.ResponseWriter, r *http.Request) {
	if !s.guard(w, r) {
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	if r.URL.RawQuery != "" {
		writeJSONCoded(w, 400, CodeBadRequest, "This route does not accept a query.")
		return
	}
	if r.Method == http.MethodGet {
		value, err := s.readAIRegistry()
		if err != nil {
			writeJSONCoded(w, 409, CodeAssistantUnconfigured, err.Error())
			return
		}
		writeJSON(w, 200, value)
		return
	}
	if r.Method != http.MethodPut {
		w.Header().Set("Allow", "GET, PUT")
		w.WriteHeader(405)
		return
	}
	raw, err := readBounded(r.Body, maxAIRegistryBytes)
	if err != nil {
		writeJSONCoded(w, 413, CodeTooLarge, "AI connections exceed the size limit.")
		return
	}
	next, err := decodeAIRegistry(raw)
	if err != nil {
		writeJSONCoded(w, 422, CodeDeskConfigRefused, err.Error())
		return
	}
	expected := r.Header.Get("If-Match")
	if expected == "" {
		writeJSONCoded(w, 400, CodeBadRequest, "If-Match is required. Reload connections before saving.")
		return
	}
	s.writes.Lock()
	defer s.writes.Unlock()
	current, err := s.readAIRegistry()
	if err != nil {
		writeJSONCoded(w, 409, CodeAssistantUnconfigured, err.Error())
		return
	}
	if current.SHA256 != expected {
		writeJSONCoded(w, 409, CodeDeskConfigChanged, "AI connections changed. Reload before saving; your edits were not written.")
		return
	}
	// A connection's authentication method is identity, not an editable default.
	for _, old := range current.Connections {
		for _, c := range next.Connections {
			if c.ID == old.ID && c.Assistant.Engine != old.Assistant.Engine {
				writeJSONCoded(w, 422, CodeDeskConfigRefused, "Add a new connection to use a different connection method.")
				return
			}
		}
	}
	for i := range next.Connections {
		next.Connections[i].Revision = ""
	}
	written, err := encodeAIRegistry(next)
	if err != nil {
		writeJSONCoded(w, 413, CodeTooLarge, err.Error())
		return
	}
	err = s.assistant.writeConfigNamed(aiConnectionsFile, written, func() error {
		now, e := s.readAIRegistry()
		if e != nil {
			return e
		}
		if now.SHA256 != expected {
			return errors.New("AI connections changed before saving. Reload and try again.")
		}
		return nil
	})
	if err != nil {
		writeJSONCoded(w, 409, CodeDeskConfigChanged, err.Error())
		return
	}
	afterAIRegistryWrite(filepath.Join(s.configDir, aiConnectionsFile))
	// **Read back and held to what was written.** The answer is the registry
	// as it now reads from disk, and only when its bytes are the ones this
	// write put there: a file that changed between the rename and this read is
	// not reported as this save.
	answer, err := s.readAIRegistry()
	if err != nil {
		writeJSONCoded(w, 500, CodeInternal, "Connections were saved but could not be reloaded.")
		return
	}
	if answer.SHA256 != digestOf(written) {
		writeJSONCoded(w, 409, CodeDeskConfigChanged, "AI connections changed after saving. Reload before continuing.")
		return
	}
	writeJSON(w, 200, answer)
}
func (s *Server) selectedAIConnection(r *http.Request, enforceDesk bool) (*aiConnection, error) {
	id := r.Header.Get(aiConnectionHeader)
	if id == "" {
		present, _, err := s.assistant.readConfigNamed(aiConnectionsFile)
		profile, profileErr := s.readAssistantProfile()
		if err != nil || present || profileErr == nil && profile.Version == 2 {
			return nil, withCode(CodeAssistantUnconfigured, errors.New("Choose an AI connection before continuing."))
		}
		return nil, nil
	}
	fail := func(message string) (*aiConnection, error) {
		return nil, withCode(CodeAssistantUnconfigured, errors.New(message))
	}
	if len(r.Header.Values(aiConnectionHeader)) != 1 || len(r.Header.Values(aiRevisionHeader)) > 1 || !aiIDPattern.MatchString(id) {
		return fail("Choose a valid AI connection.")
	}
	all, err := s.readAIRegistry()
	if err != nil {
		return fail("AI connections could not be read. Reload Connections > AI.")
	}
	for _, c := range all.Connections {
		if c.ID == id {
			if revision := r.Header.Get(aiRevisionHeader); revision != "" && revision != c.Revision {
				return fail("This AI connection changed. Reload before sending again.")
			}
			if enforceDesk {
				if r.Header.Get(aiRevisionHeader) == "" {
					return fail("Reload the AI connection before sending.")
				}
				if !c.Enabled {
					return fail("This AI connection is disabled. Choose another connection.")
				}
				profile, e := s.readAssistantProfile()
				if e != nil {
					return fail(assistantProfileProblem)
				}
				if profile.Version == 2 && profile.Inherit != nil && !*profile.Inherit && !contains(profile.Connections, id) {
					return fail("This AI connection is not enabled for this desk.")
				}
			}
			return &c, nil
		}
	}
	return fail("This AI connection was removed. Choose another connection.")
}
func connectionKeyName(c *aiConnection) string {
	if c == nil || c.ID == "legacy-api" {
		return assistantKeyName
	}
	return "assistant-" + c.ID
}
func (s *Server) endpointForConnection(r *http.Request, enforceDesk bool) (assistantEndpoint, string, *aiConnection, error) {
	c, err := s.selectedAIConnection(r, enforceDesk)
	if err != nil {
		return assistantEndpoint{}, "", nil, err
	}
	if c == nil {
		endpoint, e := s.configuredEndpoint()
		return endpoint, assistantKeyName, nil, e
	}
	if c.Assistant.Engine != "vercel" || c.Assistant.Endpoint == nil {
		return assistantEndpoint{}, "", c, withCode(CodeAssistantUnconfigured, errors.New("Select an API connection."))
	}
	raw, _ := json.Marshal(c.Assistant)
	d := decodeDeskFile(append(append([]byte(`{"deskConfigVersion":1,"assistant":`), raw...), '}'))
	return *d.Endpoint, connectionKeyName(c), c, nil
}
func (s *Server) accountForConnection(r *http.Request) (providerAccountManager, error) {
	c, err := s.selectedAIConnection(r, false)
	if err != nil {
		return nil, err
	}
	if c == nil || c.ID == "legacy-codex" {
		return s.codex, nil
	}
	if c.Assistant.Engine != "codex" {
		return nil, errors.New("Select a ChatGPT connection.")
	}
	root := s
	for root.cfg.parent != nil {
		root = root.cfg.parent
	}
	root.aiAccounts.mu.Lock()
	defer root.aiAccounts.mu.Unlock()
	if root.aiAccounts.closed || root.codex == nil {
		return nil, codexbridge.ErrUnavailable
	}
	if root.aiAccounts.values == nil {
		root.aiAccounts.values = map[string]providerAccountManager{}
	}
	if m := root.aiAccounts.values[c.ID]; m != nil {
		return m, nil
	}
	m := codexbridge.NewLazyManager(codexbridge.Options{Binary: root.cfg.CodexBin, ProfileDir: filepath.Join(root.configDir, "codex-"+c.ID), ProjectDir: root.projectDir})
	root.aiAccounts.values[c.ID] = m
	return m, nil
}
func (s *Server) closeAIAccounts() {
	s.aiAccounts.mu.Lock()
	defer s.aiAccounts.mu.Unlock()
	s.aiAccounts.closed = true
	for _, m := range s.aiAccounts.values {
		m.Close()
	}
}
func connectionModels(c *aiConnection, p *assistantProfile) []string {
	var models []string
	if c.Assistant.Engine == "codex" {
		a := c.Assistant.Agent
		models = a.Models
		if models == nil && a.Model != nil {
			models = []string{*a.Model}
		}
	} else {
		models = c.Assistant.Endpoint.Models
	}
	var prefs *modelPreferences
	if p.Version == 2 {
		prefs = p.Models[c.ID]
	} else if c.ID == "legacy-codex" {
		prefs = p.Codex
	} else if c.ID == "legacy-api" {
		prefs = p.API
	}
	if prefs != nil && !prefs.Inherit {
		return prefs.Models
	}
	return models
}
func (s *Server) connectionAgentAllowed(r *http.Request, model string) bool {
	c, e := s.selectedAIConnection(r, true)
	if e != nil {
		return false
	}
	if c == nil {
		return s.agentModelAllowed(model)
	}
	if c.Assistant.Engine != "codex" {
		return false
	}
	p, e := s.readAssistantProfile()
	return e == nil && contains(connectionModels(c, p), model)
}
