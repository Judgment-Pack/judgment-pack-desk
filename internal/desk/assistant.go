package desk

// The assistant slot: the desk-level configuration file, the key this machine
// keeps, and one reachability probe.
//
// # Why any of this is in Go at all
//
// The desk's rule is that the chassis has no per-feature endpoints. Three
// facts about a model endpoint break that rule on purpose, and each one is a
// property the browser cannot have:
//
//   - **The desk-level file is outside the project.** `GET /api/file` resolves
//     every path through the project's pinned `os.Root`, which is exactly what
//     stops it reading `~/.config`. The desk-level file is therefore not
//     reachable by the file API and never will be — so it gets its own
//     read-only endpoint rather than a hole in the containment argument.
//   - **A key must never be pasted into a project file.** A project is a
//     shared checkout; a key committed to one is a key published to every
//     clone. So the key cannot go through the file API either, and Admin gains
//     the one write control it has: `PUT /api/assistant/key`.
//   - **The key must not reach the page.** It is never returned by any
//     endpoint and never sent to the browser, so the request that presents it
//     to the endpoint has to be made here. That is what the probe is.
//
// # What the chassis does not decide
//
// It reads exactly one member of the desk-level file — `assistant.endpoint`,
// the endpoint it is being asked to reach — and forms no opinion about the
// rest. The verdict on the file as a whole is the page's: `deskConfig.ts`
// decodes it, refuses it whole for one bad key, names every problem, and Admin
// renders them. This file is not a second configuration schema; it is the
// smallest read that lets an outbound request be built without the browser
// naming its destination.
//
// **The browser naming the destination is the thing this avoids.** If the
// probe took a URL from its request body, anything holding the session token
// could point this chassis — and the key it holds — at a host of its choosing.
// The destination therefore comes from a file on this machine, and a request
// body cannot move it.

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"
	"unicode/utf8"
)

/* Where this machine keeps its own things ---------------------------------- */

// The desk-level directory, and the two things inside it.
//
// `~/.config/jpack-desk` on every platform this desk targets, with
// `XDG_CONFIG_HOME` honoured — resolved here rather than through
// `os.UserConfigDir`, which answers `~/Library/Application Support` on macOS.
// The README names one path; a build that read a different one there would
// make the README false on that platform without saying so.
const (
	deskDirName      = "jpack-desk"
	deskConfigName   = "desk.json"
	secretsDirName   = "secrets"
	assistantKeyName = "assistant"
)

// The modes the secret and its directory are held to.
//
// Set on **every** store rather than only at creation: `MkdirAll` applies the
// umask, and a directory that already existed with a wider mode would
// otherwise be trusted as found. The file mode is set explicitly for the
// mirror-image reason — `O_CREATE`'s mode is masked too, and a umask can only
// ever take bits away, so an explicit `Chmod` is what makes 0600 exact rather
// than "0600 or narrower".
const (
	secretsDirMode = 0o700
	secretMode     = 0o600
)

// maxKeyBytes bounds a stored key. An API key is tens of characters; four
// kibibytes is far past every format anyone issues, and a request past it is
// not a key that got long, it is a body that is not a key.
const maxKeyBytes = 4 << 10

// minFingerprintable is the shortest key this will fingerprint.
//
// The fingerprint is four characters from each end. A key of eight characters
// would therefore be disclosed in full by its own fingerprint, and one of ten
// would be disclosed but for two. Twelve leaves at least a third of the value
// unshown, and anything shorter gets an empty fingerprint rather than a
// disclosure dressed as a redaction.
const minFingerprintable = 12

// probeTimeout bounds one reachability check end to end.
//
// A var rather than a const for exactly one reason: a test shortens it, so
// that the bound can be shown to *apply* — against an endpoint that never
// answers — in a suite that finishes. Nothing else writes it, and a test
// asserts its default is ten seconds.
var probeTimeout = 10 * time.Second

// maxProbeBody bounds how much of an endpoint's answer is read for its
// sentence. The answer is quoted, not parsed for meaning, and a megabyte of it
// says nothing the first few kilobytes do not.
const maxProbeBody = 8 << 10

// maxDetail is how much of that sentence travels to the page, in runes.
const maxDetail = 200

// configDirFor resolves the desk-level directory.
//
// An explicit directory (tests, and nothing else) wins. Otherwise
// `XDG_CONFIG_HOME` where it is **absolute** — the specification says a
// relative value is to be ignored, and honouring one would resolve this
// against whatever directory the desk happened to be started in — and
// `$HOME/.config` where it is not set.
//
// An empty answer means there is no home directory to speak of. It is not an
// error here: it becomes one at the point of use, where it can be reported as
// a sentence rather than as a failure to start the desk.
func configDirFor(explicit string) string {
	if explicit != "" {
		return explicit
	}
	if xdg := os.Getenv("XDG_CONFIG_HOME"); filepath.IsAbs(xdg) {
		return filepath.Join(xdg, deskDirName)
	}
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return ""
	}
	return filepath.Join(home, ".config", deskDirName)
}

func (s *Server) deskConfigPath() string {
	return filepath.Join(s.configDir, deskConfigName)
}

func (s *Server) secretsDir() string {
	return filepath.Join(s.configDir, secretsDirName)
}

func (s *Server) assistantKeyPath() string {
	return filepath.Join(s.secretsDir(), assistantKeyName)
}

// errNoConfigDir is the one thing that stops every endpoint here.
var errNoConfigDir = errors.New(
	"this machine has no configuration directory: set XDG_CONFIG_HOME or HOME")

/* The desk-level configuration file ---------------------------------------- */

// DeskLevelConfig is what `GET /api/desk-config` answers.
//
// **Absence is a 200, not a 404**, and that is the whole shape of this
// endpoint. The question it answers is "what is the desk-level configuration
// on this machine, and where would it be" — and "there is none, and it would
// be at this path" is a complete answer to that question rather than a
// failure to answer it. Admin needs the path in both cases: it is what it
// tells the reader to write. A *failed* read is still a refusal, because then
// the answer is genuinely not known.
type DeskLevelConfig struct {
	// Path is absolute, on this machine.
	Path    string `json:"path"`
	Present bool   `json:"present"`
	// Content is the file's bytes, present exactly where Present is true.
	Content string `json:"content,omitempty"`
}

// handleDeskConfig reads the desk-level file.
//
// Read-only, and there is no writing counterpart. Everything in that file
// except the key is ordinary configuration that a person edits in an editor,
// which is what every other configuration surface on this desk already
// assumes; the key is the one thing that cannot be written that way, and it
// has its own endpoint below precisely because it is not in this file.
//
// A symlinked `desk.json` is followed rather than refused. It is a fixed path
// inside the reader's own configuration directory — not a path anyone sent us
// — and a dotfile tree assembled out of symlinks is how a great many people
// keep their configuration.
func (s *Server) handleDeskConfig(w http.ResponseWriter, r *http.Request) {
	if !s.guard(w, r) {
		return
	}
	if s.configDir == "" {
		writeJSONCoded(w, http.StatusInternalServerError, CodeInternal, errNoConfigDir.Error())
		return
	}
	path := s.deskConfigPath()
	info, err := os.Stat(path)
	if errors.Is(err, os.ErrNotExist) {
		writeJSON(w, http.StatusOK, DeskLevelConfig{Path: path, Present: false})
		return
	}
	if err != nil {
		s.refuseDeskRead(w, path, err)
		return
	}
	if !info.Mode().IsRegular() {
		writeJSONCoded(w, http.StatusBadRequest, CodeNotAFile,
			fmt.Sprintf("%s is not a regular file", path))
		return
	}
	if info.Size() > maxFileBytes {
		writeJSONCoded(w, http.StatusRequestEntityTooLarge, CodeTooLarge,
			fmt.Sprintf("%s is %d bytes, past the %d this desk reads", path, info.Size(), maxFileBytes))
		return
	}
	data, err := os.ReadFile(path)
	if err != nil {
		s.refuseDeskRead(w, path, err)
		return
	}
	if !utf8.Valid(data) {
		writeJSONCoded(w, http.StatusUnsupportedMediaType, CodeNotUTF8,
			fmt.Sprintf("%s is not UTF-8 text", path))
		return
	}
	writeJSON(w, http.StatusOK, DeskLevelConfig{Path: path, Present: true, Content: string(data)})
}

// refuseDeskRead answers a read that found something and could not use it.
//
// Kept apart from absence for the reason the page states on Admin: a file that
// exists and was not read is not the same fact as no file, and reporting it as
// the defaults describes the desk as unconfigured when it is merely unread.
func (s *Server) refuseDeskRead(w http.ResponseWriter, path string, err error) {
	if errors.Is(err, os.ErrPermission) {
		writeJSONCoded(w, http.StatusForbidden, CodeForbidden,
			fmt.Sprintf("%s could not be read: permission denied", path))
		return
	}
	writeJSONCoded(w, http.StatusInternalServerError, CodeInternal,
		fmt.Sprintf("%s could not be read: %v", path, err))
}

/* The key this machine keeps ----------------------------------------------- */

// AssistantKeyState is everything any endpoint here will say about the key.
//
// **There is no member carrying the key, and that is the design rather than an
// omission.** Nothing in this package returns it, logs it, or puts it in a
// response body; the only place it goes is into the outbound request the probe
// makes. A page that could read it back would be a page that could be made to
// send it somewhere, and the second half of that sentence is why the first
// half is not offered.
type AssistantKeyState struct {
	Present bool `json:"present"`
	// Fingerprint is four characters from each end, or empty — for an absent
	// key, and for one too short to fingerprint without disclosing it.
	Fingerprint string `json:"fingerprint"`
}

// fingerprint is enough of a key to recognise and not enough to use.
//
// Runes rather than bytes: a key is not promised to be ASCII, and slicing a
// UTF-8 sequence in half would put replacement characters on the page and call
// them a fingerprint.
func fingerprint(key string) string {
	runes := []rune(key)
	if len(runes) < minFingerprintable {
		return ""
	}
	return string(runes[:4]) + "…" + string(runes[len(runes)-4:])
}

// readAssistantKey answers the stored key, or the empty string where there is
// none. An absent key is not an error: it is the state every desk starts in.
func (s *Server) readAssistantKey() (string, error) {
	if s.configDir == "" {
		return "", errNoConfigDir
	}
	data, err := os.ReadFile(s.assistantKeyPath())
	if errors.Is(err, os.ErrNotExist) {
		return "", nil
	}
	if err != nil {
		return "", err
	}
	// Trimmed on the way out as well as in, so a file a person wrote by hand
	// with a trailing newline presents the same key this desk would have
	// stored from the same paste.
	return strings.TrimSpace(string(data)), nil
}

// storeAssistantKey writes the key, atomically, owner-only.
//
// The same replace discipline the file API uses — stage in the destination
// directory, set the mode, flush, rename — for the same reason: a reader
// during the write sees the old key or the new one, never half of one. The
// modes are re-asserted on every store rather than assumed from creation.
func (s *Server) storeAssistantKey(key string) error {
	if s.configDir == "" {
		return errNoConfigDir
	}
	dir := s.secretsDir()
	if err := os.MkdirAll(dir, secretsDirMode); err != nil {
		return err
	}
	// MkdirAll applies the umask and does nothing at all to a directory that
	// already exists. Neither of those produces 0700 on its own.
	if err := os.Chmod(dir, secretsDirMode); err != nil {
		return err
	}
	staged, err := os.CreateTemp(dir, ".assistant-*.tmp")
	if err != nil {
		return err
	}
	name := staged.Name()
	remove := func() { _ = os.Remove(name) }
	if _, err := staged.WriteString(key); err != nil {
		staged.Close()
		remove()
		return err
	}
	if err := staged.Chmod(secretMode); err != nil {
		staged.Close()
		remove()
		return err
	}
	if err := staged.Sync(); err != nil {
		staged.Close()
		remove()
		return err
	}
	if err := staged.Close(); err != nil {
		remove()
		return err
	}
	if err := os.Rename(name, s.assistantKeyPath()); err != nil {
		remove()
		return err
	}
	if d, derr := os.Open(dir); derr == nil {
		_ = d.Sync()
		_ = d.Close()
	}
	return nil
}

// removeAssistantKey deletes the key. Deleting one that is not there is not a
// failure — the caller asked for a state, and that state already holds.
func (s *Server) removeAssistantKey() error {
	if s.configDir == "" {
		return errNoConfigDir
	}
	err := os.Remove(s.assistantKeyPath())
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	return err
}

// keyRequest is the body of a store.
type keyRequest struct {
	Key string `json:"key"`
}

func (s *Server) handleAssistantKeyRead(w http.ResponseWriter, r *http.Request) {
	if !s.guard(w, r) {
		return
	}
	key, err := s.readAssistantKey()
	if err != nil {
		writeJSONCoded(w, http.StatusInternalServerError, CodeInternal,
			fmt.Sprintf("the assistant key could not be read: %v", err))
		return
	}
	writeJSON(w, http.StatusOK, AssistantKeyState{Present: key != "", Fingerprint: fingerprint(key)})
}

func (s *Server) handleAssistantKeyWrite(w http.ResponseWriter, r *http.Request) {
	if !s.guard(w, r) {
		return
	}
	// Bounded before it is buffered. The envelope allowance is what keeps a
	// key of exactly the maximum length from being refused for the quotes and
	// braces around it.
	r.Body = http.MaxBytesReader(w, r.Body, maxKeyBytes+1024)
	var req keyRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		var tooLarge *http.MaxBytesError
		if errors.As(err, &tooLarge) {
			writeJSONCoded(w, http.StatusRequestEntityTooLarge, CodeTooLarge,
				fmt.Sprintf("a key must be at most %d bytes", maxKeyBytes))
			return
		}
		// The decoder's own sentence is not quoted back. It reports where in
		// the body it gave up, and the body is a key.
		writeJSONCoded(w, http.StatusBadRequest, CodeBadRequest,
			"the request body must be JSON of the shape {\"key\": \"…\"}")
		return
	}
	key := strings.TrimSpace(req.Key)
	if key == "" {
		writeJSONCoded(w, http.StatusBadRequest, CodeBadRequest,
			"key must be a non-empty string; nothing was stored")
		return
	}
	if len(key) > maxKeyBytes {
		writeJSONCoded(w, http.StatusRequestEntityTooLarge, CodeTooLarge,
			fmt.Sprintf("a key must be at most %d bytes; nothing was stored", maxKeyBytes))
		return
	}
	// **A control character in a credential is refused rather than carried.**
	// The key is presented in a request header, and a carriage return or a
	// newline inside one is header injection — the outbound request would be a
	// different request from the one this code reads. It is also the shape a
	// mis-paste takes, so refusing it is the friendly answer as well as the
	// safe one. The character is named by position, never by value.
	if index := strings.IndexFunc(key, isControl); index >= 0 {
		writeJSONCoded(w, http.StatusBadRequest, CodeBadRequest,
			fmt.Sprintf("a key may not contain a control character; there is one at position %d, "+
				"and nothing was stored", index))
		return
	}
	if err := s.storeAssistantKey(key); err != nil {
		writeJSONCoded(w, http.StatusInternalServerError, CodeInternal,
			fmt.Sprintf("the assistant key could not be stored: %v", err))
		return
	}
	// The event, never the value. This line is what the "never in the log"
	// test is measured against: a log with nothing in it proves nothing about
	// a handler that never ran.
	s.log.Printf("desk: the assistant key was stored on this machine")
	writeJSON(w, http.StatusOK, AssistantKeyState{Present: true, Fingerprint: fingerprint(key)})
}

func (s *Server) handleAssistantKeyDelete(w http.ResponseWriter, r *http.Request) {
	if !s.guard(w, r) {
		return
	}
	if err := s.removeAssistantKey(); err != nil {
		writeJSONCoded(w, http.StatusInternalServerError, CodeInternal,
			fmt.Sprintf("the assistant key could not be removed: %v", err))
		return
	}
	s.log.Printf("desk: the assistant key was removed from this machine")
	writeJSON(w, http.StatusOK, AssistantKeyState{Present: false, Fingerprint: ""})
}

// isControl reports the characters a header value may not carry.
func isControl(r rune) bool { return r < 0x20 || r == 0x7f }

/* The configured endpoint -------------------------------------------------- */

// AssistantKinds is the closed set of wire protocols this desk can speak.
//
// **`kind` is a protocol, not a vendor**, and the distinction is the reason
// this member is allowed to exist at all beside the identity slot's rule that
// there is no discriminator. The identity slot has none because an issuer
// someone else operates and an issuer you run are the *same request* with a
// different URL in it — there is nothing to branch on. Here there genuinely
// is: the two protocols put the credential in different headers and the
// request on different paths, so a single code path could not send either one.
// It branches on how bytes are shaped and never on who is at the other end:
// an endpoint someone operates for you and one you run yourself are the same
// object in the same slot, and nothing here reads the host.
var AssistantKinds = []string{"openai-compatible", "anthropic"}

// AssistantTools is the closed set of runtime tools the assistant may be
// configured to call.
//
// Held here as well as in the page's decoder because both sides refuse by it,
// and a test reads this declaration to hold the two lists to one answer. Every
// one of them is a **read** of the runtime: three questions and a rehearsal.
// There is no write tool on the list because the runtime has none, and no file
// tool because proposing an edit is the assistant's whole reach.
var AssistantTools = []string{"get_schema", "get_example", "validate", "experimental_evaluate"}

// assistantEndpoint is the one member of the desk-level file this reads.
type assistantEndpoint struct {
	url   string
	kind  string
	model string
	tools []string
}

// endpointMembers is the endpoint object's key set, exactly.
var endpointMembers = []string{"url", "kind", "model", "tools"}

// configuredEndpoint reads `assistant.endpoint` out of the desk-level file.
//
// Strict about that object and silent about everything else in the file: an
// unknown member here is refused by name, because a probe built from an object
// this desk does not fully understand is a request to a destination nobody
// checked. The rest of the file is the page's to judge.
func (s *Server) configuredEndpoint() (assistantEndpoint, error) {
	var zero assistantEndpoint
	if s.configDir == "" {
		return zero, withCode(CodeAssistantUnconfigured, errNoConfigDir)
	}
	path := s.deskConfigPath()
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return zero, withCode(CodeAssistantUnconfigured, fmt.Errorf(
			"no assistant endpoint is configured: there is no %s", path))
	}
	if err != nil {
		return zero, withCode(CodeAssistantUnconfigured, fmt.Errorf(
			"no assistant endpoint could be read: %s could not be read: %v", path, err))
	}
	var file struct {
		Assistant *struct {
			Endpoint json.RawMessage `json:"endpoint"`
		} `json:"assistant"`
	}
	if err := json.Unmarshal(data, &file); err != nil {
		return zero, withCode(CodeAssistantUnconfigured, fmt.Errorf(
			"no assistant endpoint is configured: %s is not JSON this desk could read", path))
	}
	if file.Assistant == nil || len(file.Assistant.Endpoint) == 0 ||
		string(file.Assistant.Endpoint) == "null" {
		return zero, withCode(CodeAssistantUnconfigured, errors.New(
			"no assistant endpoint is configured: assistant.endpoint is absent or null"))
	}
	var members map[string]json.RawMessage
	if err := json.Unmarshal(file.Assistant.Endpoint, &members); err != nil {
		return zero, withCode(CodeAssistantUnconfigured, errors.New(
			"assistant.endpoint must be an object with url, kind, model and tools"))
	}
	allowed := make(map[string]bool, len(endpointMembers))
	for _, member := range endpointMembers {
		allowed[member] = true
	}
	for member := range members {
		if !allowed[member] {
			return zero, withCode(CodeAssistantUnconfigured, fmt.Errorf(
				"assistant.endpoint.%s is not a member this desk understands; it accepts %s",
				member, strings.Join(endpointMembers, ", ")))
		}
	}
	var endpoint struct {
		URL   string   `json:"url"`
		Kind  string   `json:"kind"`
		Model string   `json:"model"`
		Tools []string `json:"tools"`
	}
	if err := json.Unmarshal(file.Assistant.Endpoint, &endpoint); err != nil {
		return zero, withCode(CodeAssistantUnconfigured, fmt.Errorf(
			"assistant.endpoint could not be read: %v", err))
	}
	if !contains(AssistantKinds, endpoint.Kind) {
		return zero, withCode(CodeAssistantUnconfigured, fmt.Errorf(
			"assistant.endpoint.kind must be one of %s", strings.Join(AssistantKinds, ", ")))
	}
	if endpoint.Model == "" {
		return zero, withCode(CodeAssistantUnconfigured, errors.New(
			"assistant.endpoint.model must be a non-empty string"))
	}
	for _, tool := range endpoint.Tools {
		if !contains(AssistantTools, tool) {
			return zero, withCode(CodeAssistantUnconfigured, fmt.Errorf(
				"assistant.endpoint.tools names %q, which is not a tool the assistant may call; "+
					"it accepts %s", tool, strings.Join(AssistantTools, ", ")))
		}
	}
	if err := acceptableEndpointURL(endpoint.URL); err != nil {
		return zero, withCode(CodeAssistantUnconfigured, err)
	}
	return assistantEndpoint{
		url:   strings.TrimRight(endpoint.URL, "/"),
		kind:  endpoint.Kind,
		model: endpoint.Model,
		tools: endpoint.Tools,
	}, nil
}

// acceptableEndpointURL holds the endpoint to a transport, and to nothing else.
//
// `https:`, or `http:` on loopback so a locally-run endpoint works — the same
// rule the identity slot's issuer gets, and it is a rule about **transport**
// rather than about who is at the other end. Nothing here reads the host,
// compares it to a list, or behaves differently for one endpoint than another.
// The key is a bearer credential, and sending one in clear text over a network
// is the one thing a desk that holds a key must not do quietly.
func acceptableEndpointURL(raw string) error {
	parsed, err := url.Parse(raw)
	if err != nil || parsed.Host == "" {
		return fmt.Errorf("assistant.endpoint.url must be an absolute URL; found %q", raw)
	}
	if parsed.Scheme == "https" {
		return nil
	}
	if parsed.Scheme == "http" && isLoopbackHost(parsed.Hostname()) {
		return nil
	}
	return fmt.Errorf(
		"assistant.endpoint.url must be an https: URL, or an http: URL on localhost or 127.0.0.1; "+
			"found %q — a key sent in clear text over a network is a key given away", raw)
}

func isLoopbackHost(host string) bool {
	return host == "localhost" || host == "127.0.0.1" || host == "::1"
}

func contains(haystack []string, needle string) bool {
	for _, candidate := range haystack {
		if candidate == needle {
			return true
		}
	}
	return false
}

/* The probe ---------------------------------------------------------------- */

// ProbeResult is what one reachability check establishes.
//
// **`reachable` means the endpoint answered this request successfully**, not
// that a socket opened. A 401 is a host that is there and a credential it will
// not take, and calling that reachable would report a desk that cannot make a
// single call as ready.
type ProbeResult struct {
	Reachable bool `json:"reachable"`
	// Status is the HTTP status, or 0 where no response arrived at all.
	Status    int   `json:"status"`
	LatencyMs int64 `json:"latencyMs"`
	// Detail is the endpoint's own sentence, or the transport's. Empty where
	// the endpoint answered successfully: there is nothing to quote.
	Detail string `json:"detail"`
}

// handleAssistantProbe makes the smallest legitimate request the configured
// protocol defines, and reports what came back.
//
// **A probe that reaches nothing still answers 200.** The question is "is this
// endpoint reachable", and "no" is an answer to it rather than a failure to
// answer. The refusals here are the two states in which the question cannot be
// asked at all, and each names which one it is: no endpoint to reach, or no
// credential to present.
func (s *Server) handleAssistantProbe(w http.ResponseWriter, r *http.Request) {
	if !s.guard(w, r) {
		return
	}
	endpoint, err := s.configuredEndpoint()
	if err != nil {
		writeJSONError(w, statusForRefusal(err), err)
		return
	}
	key, err := s.readAssistantKey()
	if err != nil {
		writeJSONCoded(w, http.StatusInternalServerError, CodeInternal,
			fmt.Sprintf("the assistant key could not be read: %v", err))
		return
	}
	if key == "" {
		writeJSONCoded(w, http.StatusConflict, CodeAssistantNoKey,
			"no key is stored on this machine, so there is nothing to present to the endpoint")
		return
	}
	result := probeEndpoint(r.Context(), endpoint, key)
	s.log.Printf("desk: assistant probe %s answered %d in %dms",
		endpoint.url, result.Status, result.LatencyMs)
	writeJSON(w, http.StatusOK, result)
}

// probeClient is the client every probe uses.
//
// **Redirects are not followed.** Go strips `Authorization` on a cross-host
// redirect and knows nothing about `x-api-key`, so an endpoint that answered
// 302 could walk the anthropic-protocol credential to a host nobody
// configured. Returning the redirect as the answer makes that a visible 3xx
// on the page instead of a silent second request.
var probeClient = &http.Client{
	CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
}

// probeEndpoint sends one request and times it.
//
// The request is the smallest legitimate one each protocol defines. For an
// OpenAI-compatible endpoint that is the model listing — a `GET`, which
// creates nothing and costs nothing. For an anthropic endpoint there is no
// such listing on the messages path, so it is a messages call bounded to a
// single output token: the smallest request that protocol has, and one that
// exercises the credential rather than merely the route.
//
// `url` is the base the endpoint documents for its own protocol: for
// `openai-compatible` the base that carries `/models` and `/chat/completions`,
// which usually ends in `/v1`; for `anthropic` the base that carries
// `/v1/messages`. The desk appends the path the protocol prescribes and
// nothing else — it does not guess a version segment, because guessing one
// would be the desk deciding what an endpoint's address is.
func probeEndpoint(ctx context.Context, endpoint assistantEndpoint, key string) ProbeResult {
	ctx, cancel := context.WithTimeout(ctx, probeTimeout)
	defer cancel()

	request, err := probeRequest(ctx, endpoint, key)
	if err != nil {
		return ProbeResult{Detail: truncate(scrub(err.Error(), key))}
	}
	started := time.Now()
	response, err := probeClient.Do(request)
	elapsed := time.Since(started).Milliseconds()
	if err != nil {
		// The transport's own sentence, scrubbed and bounded like any other.
		// It names the host and the failure; it never carries the credential,
		// and it is put through the scrub anyway rather than on that belief.
		return ProbeResult{Status: 0, LatencyMs: elapsed, Detail: truncate(scrub(err.Error(), key))}
	}
	defer response.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(response.Body, maxProbeBody))
	reachable := response.StatusCode >= 200 && response.StatusCode < 300
	detail := ""
	if !reachable {
		detail = truncate(scrub(sentenceOf(body, response.Status), key))
	}
	return ProbeResult{
		Reachable: reachable,
		Status:    response.StatusCode,
		LatencyMs: elapsed,
		Detail:    detail,
	}
}

func probeRequest(ctx context.Context, endpoint assistantEndpoint, key string) (*http.Request, error) {
	switch endpoint.kind {
	case "openai-compatible":
		request, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint.url+"/models", nil)
		if err != nil {
			return nil, err
		}
		request.Header.Set("Authorization", "Bearer "+key)
		return request, nil
	case "anthropic":
		payload, err := json.Marshal(map[string]any{
			"model":      endpoint.model,
			"max_tokens": 1,
			"messages":   []map[string]string{{"role": "user", "content": "ping"}},
		})
		if err != nil {
			return nil, err
		}
		request, err := http.NewRequestWithContext(ctx, http.MethodPost,
			endpoint.url+"/v1/messages", strings.NewReader(string(payload)))
		if err != nil {
			return nil, err
		}
		request.Header.Set("x-api-key", key)
		// The version this protocol requires on every request. It is a
		// property of the wire, not a model or a vendor choice, and an
		// endpoint speaking this protocol refuses a request without it.
		request.Header.Set("anthropic-version", "2023-06-01")
		request.Header.Set("content-type", "application/json")
		return request, nil
	default:
		// Unreachable: `configuredEndpoint` refuses every other kind by name.
		return nil, fmt.Errorf("no probe is defined for %q", endpoint.kind)
	}
}

// sentenceOf pulls the endpoint's own message out of its answer.
//
// Both protocols put it in `{"error": {"message": …}}`, and some things that
// speak neither put a bare string in `{"error": …}`. Anything else is quoted
// as it arrived: the point is to show the reader what the endpoint said, and
// paraphrasing it would be the desk speaking for a service it does not own.
func sentenceOf(body []byte, status string) string {
	trimmed := strings.TrimSpace(string(body))
	if trimmed == "" {
		return status
	}
	var structured struct {
		Error json.RawMessage `json:"error"`
	}
	if err := json.Unmarshal(body, &structured); err == nil && len(structured.Error) > 0 {
		var nested struct {
			Message string `json:"message"`
		}
		if err := json.Unmarshal(structured.Error, &nested); err == nil && nested.Message != "" {
			return nested.Message
		}
		var bare string
		if err := json.Unmarshal(structured.Error, &bare); err == nil && bare != "" {
			return bare
		}
	}
	return trimmed
}

// scrub takes the key out of anything about to leave this process.
//
// An endpoint that echoes the credential back in its error — and some do, in
// the name of being helpful — would otherwise have the desk paint it on the
// page and put it in the log. The key is never *supposed* to be in any of
// these strings; this is what makes that a property rather than a hope.
func scrub(text, key string) string {
	if key == "" {
		return text
	}
	return strings.ReplaceAll(text, key, "…")
}

// truncate bounds a quoted sentence, in runes.
func truncate(text string) string {
	runes := []rune(text)
	if len(runes) <= maxDetail {
		return text
	}
	return string(runes[:maxDetail]) + "…"
}
