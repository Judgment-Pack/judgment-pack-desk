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
// # Where the two halves of the argument live
//
// Custody — the validated, pinned directory the key lives in — is `custody.go`.
// The configuration contract — the whole-file decode this shares with the
// browser, so that a file Admin refuses cannot authorise an outbound request —
// is `deskfile.go`. Each has its own long comment; what is left here is the
// five handlers and the probe.
//
// **The browser naming the destination is the thing this avoids.** If the
// probe took a URL from its request body, anything holding the session token
// could point this chassis — and the key it holds — at a host of its choosing.
// The destination comes from a file on this machine, decoded under the same
// contract the page decodes it under, and a request body cannot move it.

import (
	"context"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"
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
	// keyStagingPrefix names the files a store creates while replacing the
	// key. Distinct from the project API's staging prefix: nothing walks this
	// directory, and a shared name would invite one list of exclusions to be
	// read as covering both.
	keyStagingPrefix = ".assistant-"
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

// maxProbeBody bounds how much of an endpoint's answer is read.
//
// It is read and **discarded**: the body is drained so the connection can be
// reused and then thrown away, because nothing an endpoint writes is repeated
// to anybody. See `ProbeResult`.
const maxProbeBody = 8 << 10

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

// secretsDir and assistantKeyPath are **names, for diagnostics and for tests
// that inspect the filesystem afterwards**. Nothing opens anything through
// them: every operation goes through the pinned descriptors in `custody.go`,
// which is the whole point of that file.
func (s *Server) secretsDir() string {
	return filepath.Join(s.configDir, secretsDirName)
}

func (s *Server) assistantKeyPath() string {
	return filepath.Join(s.secretsDir(), assistantKeyName)
}

// stagingName is one unused name for a staged key write.
func stagingName() (string, error) {
	var raw [12]byte
	if _, err := rand.Read(raw[:]); err != nil {
		return "", err
	}
	return keyStagingPrefix + hex.EncodeToString(raw[:]) + ".tmp", nil
}

// readBounded reads at most limit bytes and refuses anything longer.
func readBounded(reader io.Reader, limit int) ([]byte, error) {
	data, err := io.ReadAll(io.LimitReader(reader, int64(limit)+1))
	if err != nil {
		return nil, err
	}
	if len(data) > limit {
		return nil, fmt.Errorf("more than %d bytes", limit)
	}
	return data, nil
}

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

// readDeskFile reads the desk-level file through the validated, pinned
// directory, and answers whether there was one.
//
// Through the store's own descriptor rather than a pathname, for the reason
// `custody.go` gives at length: this file names the endpoint a credential is
// presented to, so a directory in which its name can be replaced is a
// directory in which the destination can be. A symlinked `desk.json` is
// refused along with everything else — the store's validation is what a
// dotfile tree assembled out of symlinks now has to satisfy, and refusing is
// the safe answer where it does not.
func (s *Server) readDeskFile() (present bool, data []byte, err error) {
	if !s.assistant.usable() {
		return false, nil, s.assistant.problem
	}
	info, err := s.assistant.root.Lstat(deskConfigName)
	if errors.Is(err, os.ErrNotExist) {
		return false, nil, nil
	}
	if err != nil {
		return false, nil, err
	}
	if !info.Mode().IsRegular() {
		// Coded, so the handler answers the status this refusal deserves
		// rather than the internal one every unclassified read failure gets.
		// A symlink lands here too: `Lstat` does not follow one, so its mode
		// is the link's and a link is not a regular file.
		return false, nil, withCode(CodeNotAFile,
			fmt.Errorf("%s is not a regular file", s.deskConfigPath()))
	}
	file, err := s.assistant.root.OpenFile(
		deskConfigName, os.O_RDONLY|openNoFollow|openNonBlocking, 0)
	if err != nil {
		return false, nil, err
	}
	defer file.Close()
	data, err = readBounded(file, maxFileBytes)
	if err != nil {
		return false, nil, err
	}
	return true, data, nil
}

// handleDeskConfig reads the desk-level file.
//
// Read-only, and there is no writing counterpart. Everything in that file
// except the key is ordinary configuration that a person edits in an editor,
// which is what every other configuration surface on this desk already
// assumes; the key is the one thing that cannot be written that way, and it
// has its own endpoint below precisely because it is not in this file.
func (s *Server) handleDeskConfig(w http.ResponseWriter, r *http.Request) {
	if !s.guard(w, r) {
		return
	}
	if !s.assistant.usable() {
		// The custody refusal, verbatim and in full. Admin renders it: a desk
		// that will not keep a key must say which directory is the reason, or
		// nobody can repair it.
		writeJSONCoded(w, http.StatusConflict, CodeAssistantUnusableStore,
			s.assistant.problem.Error())
		return
	}
	path := s.deskConfigPath()
	present, data, err := s.readDeskFile()
	if err != nil {
		s.refuseDeskRead(w, path, err)
		return
	}
	if !present {
		writeJSON(w, http.StatusOK, DeskLevelConfig{Path: path, Present: false})
		return
	}
	if !validUTF8(data) {
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
	// A refusal that carries its own code answers with it: the code-to-status
	// matrix is the one place that decides, and a call site that picked a
	// status of its own is how a code and a status came to disagree once.
	if code := codeOf(err); code != CodeInternal {
		writeJSONError(w, statusForRefusal(err), err)
		return
	}
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

// keyRequest is the body of a store.
type keyRequest struct {
	Key string `json:"key"`
}

// refuseUnusableStore answers every key endpoint where custody was not
// established. One place, so a new handler cannot forget it.
func (s *Server) refuseUnusableStore(w http.ResponseWriter) bool {
	if s.assistant.usable() {
		return false
	}
	writeJSONCoded(w, http.StatusConflict, CodeAssistantUnusableStore,
		"this desk is not keeping a key: "+s.assistant.problem.Error())
	return true
}

func (s *Server) handleAssistantKeyRead(w http.ResponseWriter, r *http.Request) {
	if !s.guard(w, r) {
		return
	}
	if s.refuseUnusableStore(w) {
		return
	}
	key, err := s.assistant.readKey()
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
	if s.refuseUnusableStore(w) {
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

	// **The control check runs on the value as it arrived, before anything is
	// trimmed.** It used to run after `TrimSpace`, which meant a key with a
	// leading newline or a trailing tab was silently *repaired* into an
	// acceptable one — so the contract said "no control character" while the
	// implementation said "no control character in the middle". A credential
	// is presented in a request header, and a carriage return or newline
	// inside one is header injection; it is also the shape a mis-paste takes,
	// so refusing it is the friendly answer as well as the safe one. The
	// character is named by position, never by value.
	if index := strings.IndexFunc(req.Key, isControl); index >= 0 {
		writeJSONCoded(w, http.StatusBadRequest, CodeBadRequest,
			fmt.Sprintf("a key may not contain a control character; there is one at position %d, "+
				"and nothing was stored", index))
		return
	}
	// Only ordinary whitespace is normalised, and only after the check above.
	// A space either side of a pasted key is a slip; a newline is not, and is
	// no longer treated as one.
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
	if err := s.assistant.storeKey(key); err != nil {
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
	if s.refuseUnusableStore(w) {
		return
	}
	if err := s.assistant.removeKey(); err != nil {
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

// assistantEndpoint is what a clean decode of the whole file yields.
type assistantEndpoint struct {
	url   string
	kind  string
	model string
	tools []string
}

// configuredEndpoint decodes the whole desk-level file and answers the
// endpoint only where nothing at all in that file was refused.
//
// **This is the fix for the disagreement, and the sentence worth keeping in
// mind is this one**: the page refuses the whole file for one bad key, so a
// chassis that read only its own member could probe with a configuration the
// desk had visibly rejected — sending the stored credential on the authority
// of a file nobody accepted. The two now apply one contract, held together by
// fixtures both sides read.
func (s *Server) configuredEndpoint() (assistantEndpoint, error) {
	var zero assistantEndpoint
	if !s.assistant.usable() {
		return zero, withCode(CodeAssistantUnusableStore, s.assistant.problem)
	}
	path := s.deskConfigPath()
	present, data, err := s.readDeskFile()
	if err != nil {
		return zero, withCode(CodeAssistantUnconfigured, fmt.Errorf(
			"no assistant endpoint could be read: %s could not be read: %v", path, err))
	}
	if !present {
		return zero, withCode(CodeAssistantUnconfigured, fmt.Errorf(
			"no assistant endpoint is configured: there is no %s", path))
	}
	if !validUTF8(data) {
		return zero, withCode(CodeAssistantUnconfigured, fmt.Errorf(
			"no assistant endpoint is configured: %s is not UTF-8 text", path))
	}
	decoded := decodeDeskFile(data)
	if decoded.refused() {
		// Named, and named the same way Admin names them, so the reader is not
		// asked to reconcile two accounts of one file.
		return zero, withCode(CodeAssistantUnconfigured, fmt.Errorf(
			"%s was refused, so no endpoint in it is configured: %s",
			path, describeProblems(decoded.Problems)))
	}
	if decoded.Endpoint == nil {
		return zero, withCode(CodeAssistantUnconfigured, errors.New(
			"no assistant endpoint is configured: assistant.endpoint is absent or null"))
	}
	return *decoded.Endpoint, nil
}

// describeProblems renders a decode's refusals as one sentence.
func describeProblems(problems []deskProblem) string {
	rendered := make([]string, 0, len(problems))
	for _, problem := range problems {
		if problem.Key == "" {
			rendered = append(rendered, problem.Reason)
			continue
		}
		rendered = append(rendered, problem.Key+": "+problem.Reason)
	}
	return strings.Join(rendered, "; ")
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

// The diagnostic vocabulary: the whole of what a probe will say about an
// endpoint's answer.
//
// **Nothing an endpoint writes is repeated to anybody**, and that is a change
// from quoting its own error sentence. The reason is narrow and worth stating:
// the desk holds a credential, and a body under the endpoint's control can
// carry a *derived* representation of it — base64, percent-encoded,
// JSON-escaped, hex, or half of it — which no substitution can reliably find.
// A scrub that removed the literal value and nothing else was a categorical
// promise held by a `strings.ReplaceAll`. So the body is drained and
// discarded, and what travels is one word from this list.
//
// The cost is real and is accepted: a reader debugging a misconfigured gateway
// no longer sees its sentence and must look at the endpoint's own logs. That
// is the trade, stated rather than glossed.
const (
	DiagnosticUnauthorized = "unauthorized"
	DiagnosticForbidden    = "forbidden"
	DiagnosticNotFound     = "not-found"
	DiagnosticTimeout      = "timeout"
	DiagnosticTLS          = "tls"
	DiagnosticRefused      = "refused"
	DiagnosticDNS          = "dns"
	DiagnosticUnexpected   = "unexpected-status"
)

// AssistantDiagnostics is the vocabulary, for the test that holds the page's
// copy of it to this one.
var AssistantDiagnostics = []string{
	DiagnosticUnauthorized, DiagnosticForbidden, DiagnosticNotFound,
	DiagnosticTimeout, DiagnosticTLS, DiagnosticRefused, DiagnosticDNS,
	DiagnosticUnexpected,
}

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
	// Diagnostic is one word from the fixed vocabulary above, or empty where
	// the endpoint answered successfully. It is never text the endpoint wrote.
	Diagnostic string `json:"diagnostic"`
}

// handleAssistantProbe makes the smallest legitimate request the configured
// protocol defines, and reports what came back.
//
// **A probe that reaches nothing still answers 200.** The question is "is this
// endpoint reachable", and "no" is an answer to it rather than a failure to
// answer. The refusals here are the states in which the question cannot be
// asked at all, and each names which one it is: no usable place to keep a key,
// no endpoint to reach — including a file that was refused — or no credential
// to present. **In every one of those, no outbound request is made.**
func (s *Server) handleAssistantProbe(w http.ResponseWriter, r *http.Request) {
	if !s.guard(w, r) {
		return
	}
	if s.refuseUnusableStore(w) {
		return
	}
	endpoint, err := s.configuredEndpoint()
	if err != nil {
		writeJSONError(w, statusForRefusal(err), err)
		return
	}
	key, err := s.assistant.readKey()
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
	// **Scheme and host only.** A configured URL may legitimately carry a
	// query string — some gateways route on one — and a query string is a
	// place people put credentials, deliberately or by pasting a presigned
	// link. Logging the whole URL therefore falsified "the key is never
	// logged" for a configuration this desk accepts. The origin is enough to
	// tell one endpoint from another in a log.
	s.log.Printf("desk: assistant probe %s answered %d in %dms",
		loggableOrigin(endpoint.url), result.Status, result.LatencyMs)
	writeJSON(w, http.StatusOK, result)
}

// probeAddress appends the protocol's path to the configured base.
//
// **To the path, and not to the string.** A configured URL may carry a query
// string — some gateways route on one — so `base + "/models"` puts the
// protocol's path *after* the query and sends `GET /v1?route=eu/models`,
// which is a request to a resource nobody named. The query is carried through
// untouched, because it is the endpoint's own routing and none of this desk's
// business.
func probeAddress(base, suffix string) string {
	parsed, err := url.Parse(base)
	if err != nil {
		return base + suffix
	}
	parsed.Path = strings.TrimRight(parsed.Path, "/") + suffix
	return parsed.String()
}

// loggableOrigin is the most of a configured URL that is ever written down:
// its scheme, its host and its port.
//
// Userinfo and a fragment are refused at decode, so they cannot be here; the
// query is dropped rather than refused, because it is allowed and is not
// something to write into a log.
func loggableOrigin(raw string) string {
	parsed, err := url.Parse(raw)
	if err != nil || parsed == nil || parsed.Host == "" {
		return "the configured endpoint"
	}
	return parsed.Scheme + "://" + parsed.Host
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
		return ProbeResult{Diagnostic: DiagnosticUnexpected}
	}
	started := time.Now()
	response, err := probeClient.Do(request)
	elapsed := time.Since(started).Milliseconds()
	if err != nil {
		return ProbeResult{Status: 0, LatencyMs: elapsed, Diagnostic: transportDiagnostic(err)}
	}
	defer response.Body.Close()
	// Drained so the connection can be reused, and discarded because nothing
	// an endpoint writes is repeated to anybody.
	_, _ = io.Copy(io.Discard, io.LimitReader(response.Body, maxProbeBody))
	reachable := response.StatusCode >= 200 && response.StatusCode < 300
	diagnostic := ""
	if !reachable {
		// One word, chosen from the status. Never the bytes just discarded.
		diagnostic = statusDiagnostic(response.StatusCode)
	}
	return ProbeResult{
		Reachable:  reachable,
		Status:     response.StatusCode,
		LatencyMs:  elapsed,
		Diagnostic: diagnostic,
	}
}

// statusDiagnostic is the word for a status the endpoint answered with.
func statusDiagnostic(status int) string {
	switch status {
	case http.StatusUnauthorized:
		return DiagnosticUnauthorized
	case http.StatusForbidden:
		return DiagnosticForbidden
	case http.StatusNotFound:
		return DiagnosticNotFound
	default:
		return DiagnosticUnexpected
	}
}

// transportDiagnostic is the word for a request that never got an answer.
//
// Classified from the error's **type** wherever Go offers one, and only then
// from its text: a message is a moving target across Go releases and platforms
// and is not a thing to branch on. Anything unrecognised is
// `unexpected-status`, which is the residual of this closed vocabulary and is
// named as such rather than growing an "other" nobody defined.
func transportDiagnostic(err error) string {
	if errors.Is(err, context.DeadlineExceeded) || os.IsTimeout(err) {
		return DiagnosticTimeout
	}
	var dns *net.DNSError
	if errors.As(err, &dns) {
		return DiagnosticDNS
	}
	var verification *tls.CertificateVerificationError
	var unknownAuthority x509.UnknownAuthorityError
	var hostname x509.HostnameError
	var expired x509.CertificateInvalidError
	var recordHeader tls.RecordHeaderError
	if errors.As(err, &verification) || errors.As(err, &unknownAuthority) ||
		errors.As(err, &hostname) || errors.As(err, &expired) ||
		errors.As(err, &recordHeader) {
		return DiagnosticTLS
	}
	if errors.Is(err, connectionRefused) {
		return DiagnosticRefused
	}
	var opError *net.OpError
	if errors.As(err, &opError) && opError.Timeout() {
		return DiagnosticTimeout
	}
	return DiagnosticUnexpected
}

func probeRequest(ctx context.Context, endpoint assistantEndpoint, key string) (*http.Request, error) {
	switch endpoint.kind {
	case "openai-compatible":
		request, err := http.NewRequestWithContext(ctx, http.MethodGet,
			probeAddress(endpoint.url, "/models"), nil)
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
			probeAddress(endpoint.url, "/v1/messages"), strings.NewReader(string(payload)))
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
		// Unreachable: `decodeDeskFile` refuses every other kind by name.
		return nil, fmt.Errorf("no probe is defined for %q", endpoint.kind)
	}
}
