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
	"bytes"
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
	// configStagingPrefix names the files a store creates while replacing the
	// desk-level file. Distinct from the key's for the reason that one is
	// distinct from the project API's: these live in a different directory,
	// and one prefix read as covering both would be one exclusion doing two
	// jobs.
	configStagingPrefix = ".desk-"
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
func stagingName() (string, error) { return randomStagingName(keyStagingPrefix) }

// configStagingName is one unused name for a staged desk-level file write.
func configStagingName() (string, error) { return randomStagingName(configStagingPrefix) }

func randomStagingName(prefix string) (string, error) {
	var raw [12]byte
	if _, err := rand.Read(raw[:]); err != nil {
		return "", err
	}
	return prefix + hex.EncodeToString(raw[:]) + ".tmp", nil
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
	// SHA256 is the digest of those bytes, bare hex — the payload-member
	// convention, not the `sha256:` prefixed form — and **the empty string
	// where there is no file**.
	//
	// It is here so the page can send it back on `PUT /api/desk-config` and
	// have the write refused if the file moved underneath it. The empty string
	// is the same sentinel the file API uses for `baseSha256`: "I believe
	// there is nothing there". A zero-byte file is not that — it digests to
	// the hash of no bytes, which is a value — so the two states are never
	// confused.
	SHA256 string `json:"sha256"`
	// Project and Runtime are **facts about this process**, not members of any
	// file, and they are here because the page must not invent either.
	//
	// Admin prints where the project's own configuration file is and which
	// runtime binary the desk was launched with. A page that joined a
	// directory to a file name would be asserting a location on a filesystem
	// it cannot see — and would be wrong the first time somebody's project was
	// reached through a symlink, because what the chassis pinned is the
	// resolved path and not the one it was handed. Neither is in the
	// configuration schema at any depth: `relay.go` runs the binary it was
	// given, so a config-supplied path would be a way to run code on this
	// machine by editing a file.
	Project ProjectPaths `json:"project"`
	Runtime RuntimePaths `json:"runtime"`
}

// ProjectPaths is the project this desk is open on, as the chassis resolved it.
type ProjectPaths struct {
	// Dir is the project root, absolute and with its symlinks resolved — the
	// same path every part of the chassis operates on, pinned once at startup.
	Dir string `json:"dir"`
	// File is the project's own configuration file inside it, absolute and
	// **whether or not it is there**: a page that has to say where to write one
	// needs the path in both states.
	File string `json:"file"`
}

// RuntimePaths is the runtime binary this desk was launched with.
type RuntimePaths struct {
	// Bin is the flag's value verbatim: a path, or a name resolved on PATH.
	// It is not resolved here, because what a reader has to check against the
	// command line is what the command line said.
	Bin string `json:"bin"`
}

// DeskConfigWrite is the body of a desk-level write.
//
// **Two members and no path**, for the reason the probe takes no URL: this
// route writes exactly one file, the one on this machine, and a request body
// that could name another would be a way to write anywhere with the desk's own
// authority.
type DeskConfigWrite struct {
	// Assistant is the `assistant` object exactly as the whole-file decoder
	// accepts it, carried as **raw bytes rather than a decoded object**.
	//
	// That is the canonical-bytes discipline this repository decides
	// everything else by: what is checked has to be what is written. A decode
	// into `map[string]any` and a re-encode would turn `1e2` into `100` and a
	// large integer into a float, so the bytes examined would not be the bytes
	// stored. These are re-indented and never re-serialised.
	Assistant json.RawMessage `json:"assistant"`
	// Project is the `project` object, on the same terms.
	//
	// **A member that is present is replaced and a member that is absent is
	// untouched**, which is the rule that lets two Admin cards write two
	// members of one file without either of them having to send the other's.
	// Sending neither is a 400: a conditional commit that changes nothing is a
	// request with no meaning, and answering it 200 would report a write that
	// did not happen.
	Project json.RawMessage `json:"project"`
	// IfMatch is the digest of the desk.json bytes the page last read, bare
	// hex, or the empty string for "there was no file".
	IfMatch string `json:"ifMatch"`
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
	return s.assistant.readConfigFile()
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
	// `SHA256` is left empty below, which is this route's sentinel for "there
	// is no file" and is what a page sends back as `ifMatch` to create one.
	if !present {
		writeJSON(w, http.StatusOK, DeskLevelConfig{
			Path: path, Present: false, Project: s.projectPaths(), Runtime: s.runtimePaths()})
		return
	}
	if !validUTF8(data) {
		writeJSONCoded(w, http.StatusUnsupportedMediaType, CodeNotUTF8,
			fmt.Sprintf("%s is not UTF-8 text", path))
		return
	}
	writeJSON(w, http.StatusOK, DeskLevelConfig{
		Path: path, Present: true, Content: string(data), SHA256: digestOf(data),
		Project: s.projectPaths(), Runtime: s.runtimePaths()})
}

// projectPaths is the resolved root and the project file inside it.
//
// Composed here, once, from the path the chassis pinned — which is why the
// page never composes it: `projectDir` is `ProjectDir` with its symlinks
// resolved, and a page joining the directory it was told about to a file name
// would be naming a path this desk does not read.
func (s *Server) projectPaths() ProjectPaths {
	return ProjectPaths{
		Dir:  s.projectDir,
		File: filepath.Join(s.projectDir, projectConfigName),
	}
}

func (s *Server) runtimePaths() RuntimePaths {
	return RuntimePaths{Bin: s.cfg.JpackBin}
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

/* The one write to the desk-level file ------------------------------------- */

// maxDeskConfigBytes bounds a desk-level write.
//
// The same bound the read is held to, because a file this route wrote and then
// could not read back would be a route that breaks its own configuration. The
// envelope allowance covers the JSON around the object.
const maxDeskConfigBytes = maxFileBytes

// deskConfigWritten is what a successful write answers.
//
// **The new digest and the decoded assistant object**, both taken from the
// bytes that landed rather than from the request: the point of the answer is
// that the page can verify what is on disk, and an echo verifies only that the
// request survived the trip out. It is the same discipline `PUT /api/file`
// follows for a project file.
type deskConfigWritten struct {
	Path   string `json:"path"`
	SHA256 string `json:"sha256"`
	// Assistant is the slot as the shared decoder read it **out of the file
	// that landed**, defaults applied — not the object the request carried.
	// A page that took its own request as the outcome would be reporting what
	// it asked for, and the two differ wherever a default filled a member in.
	Assistant AssistantSlotView `json:"assistant"`
	// Project is the `project` slot as the shared decoder read it out of the
	// file that landed, on the same terms as `Assistant`: the file's answer,
	// never the request's.
	Project ProjectSlotView `json:"project"`
	// Created is true exactly where the write brought the file into existence.
	Created bool `json:"created"`
	// KeyRebindRequired is true where a key is stored on this machine and is
	// **not** the key for the endpoint this write just configured.
	//
	// **The write moves the endpoint and never the credential.** A key is
	// bound to the scheme, host and wire protocol it was entered for; changing
	// either of those leaves the stored key in place and unusable, and the
	// probe and the relay refuse with `assistant-key-unbound` rather than
	// presenting it somewhere new. This member is how the page learns that
	// without having to make a request that fails: it says "the endpoint is
	// written, and somebody has to enter the key for it".
	KeyRebindRequired bool `json:"keyRebindRequired"`
}

// deskConfigUnmoved reports whether the digest a page stated is the file that
// is there now.
//
// **One predicate, called at both ends of the transaction.** It is compared
// twice — once against the bytes this transaction read, and again after
// staging and immediately before the rename — and round 2 caught the cost of
// writing the comparison out twice: breaking either copy left the other
// answering the same 409 with the same digests, so the mutation row for
// "the digest is ignored" could not fail. Two spellings of one rule are
// invisible to a harness that breaks one of them, which is the lesson
// `ownerOnlyFile` already carries. One spelling, one row.
//
// The empty string is the sentinel for "I believe there is no file", and
// `actual` is empty in exactly that state, so the two meet without a special
// case. Case-folded because a digest is hex and a client may send either.
func deskConfigUnmoved(ifMatch, actual string) bool {
	return strings.EqualFold(strings.TrimSpace(ifMatch), actual)
}

// deskConfigMoved is the file changing under a write that was already staged.
//
// A type rather than a sentinel because the answer carries the digest that is
// actually on disk now, and the caller reports both.
type deskConfigMoved struct {
	actual string
	exists bool
}

func (d *deskConfigMoved) Error() string {
	return "the desk-level configuration on disk changed while this write was being staged"
}

// AssistantSlotView is a decoded `assistant` object, as an answer.
//
// It is the decode and never the request: `engine` and `thinking` carry their
// defaults where the file names neither, and `endpoint` is null where there is
// none. There is no member for a key, here as everywhere else.
type AssistantSlotView struct {
	Endpoint *AssistantEndpointView `json:"endpoint"`
	Engine   string                 `json:"engine"`
	Thinking string                 `json:"thinking"`
}

// ProjectSlotView is a decoded `project` object, as an answer.
//
// `file` is null where the file names none, which is the same value the schema
// admits — so the page re-seeds its field from this without a second rule
// about what an empty string would have meant.
type ProjectSlotView struct {
	File *string `json:"file"`
}

// projectView renders one accepted decode's project slot as an answer.
func projectView(decoded deskDecode) ProjectSlotView {
	if decoded.ProjectFile == "" {
		return ProjectSlotView{}
	}
	file := decoded.ProjectFile
	return ProjectSlotView{File: &file}
}

// AssistantEndpointView is the four members an endpoint has, decoded.
type AssistantEndpointView struct {
	URL   string   `json:"url"`
	Kind  string   `json:"kind"`
	Model string   `json:"model"`
	Tools []string `json:"tools"`
}

// slotView renders one accepted decode as an answer.
func slotView(decoded deskDecode) AssistantSlotView {
	view := AssistantSlotView{Engine: decoded.Engine, Thinking: decoded.Thinking}
	if decoded.Endpoint == nil {
		return view
	}
	// An empty list rather than null: `[]` means an assistant that may call
	// nothing, which is a state this schema has, and `null` is not it.
	tools := decoded.Endpoint.tools
	if tools == nil {
		tools = []string{}
	}
	view.Endpoint = &AssistantEndpointView{
		URL:   decoded.Endpoint.url,
		Kind:  decoded.Endpoint.kind,
		Model: decoded.Endpoint.model,
		Tools: tools,
	}
	return view
}

// deskConfigRefusal is a 422: the composed bytes, decoded, were not a
// configuration this desk reads.
//
// It carries the decoder's own problems key by key, because the page's next
// action is to say which member is wrong — and because these are the same
// problems Admin already renders for a file somebody wrote by hand.
type deskConfigRefusal struct {
	Error    string        `json:"error"`
	Code     string        `json:"code"`
	Problems []deskProblem `json:"problems"`
}

// handleDeskConfigWrite replaces the `assistant` object in the desk-level file.
//
// # What this route is, and what it deliberately is not
//
// It is **not** "Admin can now PUT configuration". It writes exactly one
// member of exactly one file, the one on this machine, and everything else in
// that file is carried across untouched. The reason it exists at all is the
// reason the key endpoint exists: the page is going to let an author choose a
// model and a thinking tier, and the alternative is telling them to edit a
// file in `~/.config` by hand between every attempt.
//
// # The four things that make it safe to have
//
//   - **The destination cannot come from the page.** No path in the body, and
//     the write goes through the pinned custody root — the same descriptor the
//     key is written through, validated once at startup.
//   - **A conditional commit.** The page sends the digest of the bytes it last
//     read; a file that moved underneath it is a 409 and nothing is written.
//     There is no `override`: this file names the endpoint a credential is
//     presented to, and "write anyway" is not a choice a page should be able
//     to make about it.
//   - **The bytes are composed here and decoded before they are written.** Not
//     the object the page sent — the *file* this desk would store — through the
//     same whole-file decoder the browser shares. A key-shaped member, an
//     unknown kind, a missing `tools`: each refuses the write with the
//     decoder's own problems, and nothing reaches the disk. Deciding on the
//     canonical bytes rather than on an object that is then serialised is the
//     rule this repository has arrived at everywhere else.
//   - **Every other member survives.** `identity`, `deskConfigVersion` and
//     anything else present are carried across by their own raw bytes, in
//     their own order, re-indented and never re-serialised.
func (s *Server) handleDeskConfigWrite(w http.ResponseWriter, r *http.Request) {
	if !s.guard(w, r) {
		return
	}
	if s.refuseUnusableStore(w) {
		return
	}
	// **Read whole and bounded first, then read three ways about it**, because
	// each of the three is a property of the bytes rather than of the object
	// they parse to — and round 1 found all three missing.
	raw, err := readBounded(r.Body, maxDeskConfigBytes+1024)
	if err != nil {
		writeJSONCoded(w, http.StatusRequestEntityTooLarge, CodeTooLarge,
			fmt.Sprintf("a desk-level write is at most %d bytes; nothing was written",
				maxDeskConfigBytes))
		return
	}
	// **UTF-8, before anything is decoded.** Go's JSON decoder replaces an
	// invalid byte inside a string while decoding, and `json.RawMessage` keeps
	// the original — so a `0xff` in the model name decoded clean, validated
	// clean, and was written verbatim into a file every later read then refused
	// as not UTF-8. The route would have composed a file its own reader will
	// not open, which is the pre-write decode failing at the one thing it is
	// for.
	if !validUTF8(raw) {
		writeJSONCoded(w, http.StatusUnsupportedMediaType, CodeNotUTF8,
			"a desk-level write must be UTF-8 text; nothing was written")
		return
	}
	var req DeskConfigWrite
	decoder := json.NewDecoder(bytes.NewReader(raw))
	if err := decoder.Decode(&req); err != nil {
		// The decoder's own sentence names where in the body it gave up, and
		// the body is a configuration somebody is editing; the shape is what a
		// caller can act on.
		writeJSONCoded(w, http.StatusBadRequest, CodeBadRequest,
			`the request body must be JSON of the shape {"assistant": {…}, "ifMatch": "…"}`)
		return
	}
	// **Exactly one JSON value, and nothing after it.** A decoder that stops at
	// the first value accepts a second one behind it — a body two readers
	// disagree about, which is the class this desk refuses everywhere else.
	if _, err := decoder.Token(); !errors.Is(err, io.EOF) {
		writeJSONCoded(w, http.StatusBadRequest, CodeBadRequest,
			"the request body must be exactly one JSON object, with nothing after it; "+
				"nothing was written")
		return
	}
	// **At least one member, and a member that is absent is untouched.** Two
	// Admin cards write two members of this file; neither sends the other's,
	// and a request that names neither is a conditional commit that would
	// change nothing — answering it 200 would report a write that did not
	// happen.
	if len(req.Assistant) == 0 && len(req.Project) == 0 {
		writeJSONCoded(w, http.StatusBadRequest, CodeBadRequest,
			"assistant or project is required; write null for a member this desk configures none of")
		return
	}

	// The whole compare-and-commit under the same mutex every other write on
	// this desk takes. One mutex and not one per path, for the reason
	// `Server.writes` gives: a per-path key is a spelling.
	s.writes.Lock()
	status, body := s.commitDeskConfigLocked(req)
	s.writes.Unlock()
	writeJSON(w, status, body)
}

// commitDeskConfigLocked is the whole transaction: read what is there, compare
// it to the digest the page stated, compose the new file, decode it, write it,
// and read back what landed.
//
// It touches no ResponseWriter, which is what keeps client-speed I/O out of the
// critical section.
func (s *Server) commitDeskConfigLocked(req DeskConfigWrite) (int, any) {
	path := s.deskConfigPath()
	present, current, err := s.readDeskFile()
	if err != nil {
		// Every refusal the read makes is a refusal to write as well: a
		// symlinked `desk.json`, one somebody else may write, one that changed
		// between being inspected and being opened. Treating any of them as
		// absence is what would let this route replace an out-pointing symlink
		// with a regular file — and this is the file that names where a
		// credential goes.
		refusal := withCode(codeOf(err), fmt.Errorf(
			"%s could not be read, so it was not written: %w", path, err))
		return statusForRefusal(refusal), errorBody(refusal)
	}
	if present && !validUTF8(current) {
		return http.StatusUnsupportedMediaType, codedBody(CodeNotUTF8,
			fmt.Sprintf("%s is not UTF-8 text, so it was not rewritten", path))
	}
	actual := ""
	if present {
		actual = digestOf(current)
	}
	// **Conditional, and never optional.** The page states the bytes it read;
	// a disagreement means the file changed underneath it, and the answer
	// carries both digests so the page can show what happened rather than
	// overwrite a change nobody saw. The empty string means "I believe there
	// is no file", which is the same sentinel `PUT /api/file` uses.
	if !deskConfigUnmoved(req.IfMatch, actual) {
		return http.StatusConflict, conflict{
			Error: "the desk-level configuration on disk is not the one this page read; " +
				"read it again and decide about what is actually in it",
			Code:           CodeDeskConfigChanged,
			Path:           path,
			ExpectedSHA256: strings.ToLower(strings.TrimSpace(req.IfMatch)),
			ActualSHA256:   actual,
			Exists:         present,
		}
	}

	composed, problems := composeDeskFile(current, present, req.Assistant, req.Project)
	if len(problems) > 0 {
		return http.StatusUnprocessableEntity, deskConfigRefusal{
			Error: fmt.Sprintf(
				"the configuration this would write could not be composed, so nothing was "+
					"written: %s", describeProblems(problems)),
			Code:     CodeDeskConfigRefused,
			Problems: problems,
		}
	}
	// **Bounded, and UTF-8, before a byte is staged.** Both are properties of
	// the composed file rather than of the request, and round 1 found each of
	// them able to put a file on disk that this desk's own reader refuses: an
	// envelope inside the request bound can compose past it, and the read
	// bound is what every later read applies. A file written and then
	// unreadable is worse than a write refused.
	if len(composed) > maxDeskConfigBytes {
		return http.StatusRequestEntityTooLarge, codedBody(CodeTooLarge, fmt.Sprintf(
			"the configuration this would write is %d bytes, past the %d this desk reads; "+
				"nothing was written", len(composed), maxDeskConfigBytes))
	}
	if !validUTF8(composed) {
		return http.StatusUnsupportedMediaType, codedBody(CodeNotUTF8,
			"the configuration this would write is not UTF-8 text; nothing was written")
	}
	// **The round trip, and it is the whole safety argument.** What is decoded
	// is the file this desk would store, byte for byte, under the same
	// contract the browser applies — so a key-shaped member, an unknown kind
	// or a missing tool list refuses the *write*, and the page cannot store a
	// configuration Admin would then report as refused. Nothing it sent is
	// written without passing this.
	decoded := decodeDeskFile(composed)
	if decoded.refused() {
		return http.StatusUnprocessableEntity, deskConfigRefusal{
			Error: fmt.Sprintf(
				"the configuration this would write is not one this desk reads, so nothing "+
					"was written: %s", describeProblems(decoded.Problems)),
			Code:     CodeDeskConfigRefused,
			Problems: decoded.Problems,
		}
	}

	// **The digest, compared again immediately before the rename.** The
	// comparison above is against the bytes this transaction read; round 1
	// found the window between the two open to an ordinary editor, whose write
	// this desk's mutex knows nothing about — it landed in between, was
	// overwritten, and the route reported success. `writeConfigFile` runs this
	// after staging and before publishing, so all of that window but the
	// rename itself is closed.
	var moved *deskConfigMoved
	if err := s.assistant.writeConfigFile(composed, func() error {
		nowPresent, now, rerr := s.readDeskFile()
		if rerr != nil {
			return rerr
		}
		digest := ""
		if nowPresent {
			digest = digestOf(now)
		}
		if !deskConfigUnmoved(req.IfMatch, digest) {
			moved = &deskConfigMoved{actual: digest, exists: nowPresent}
			return moved
		}
		return nil
	}); err != nil {
		if moved != nil {
			return http.StatusConflict, conflict{
				Error: "the desk-level configuration on disk changed while this write was " +
					"being staged; nothing was written",
				Code:           CodeDeskConfigChanged,
				Path:           path,
				ExpectedSHA256: strings.ToLower(strings.TrimSpace(req.IfMatch)),
				ActualSHA256:   moved.actual,
				Exists:         moved.exists,
			}
		}
		return http.StatusInternalServerError, errorBody(fmt.Errorf(
			"%s could not be written: %w", path, err))
	}

	// Read back from the disk rather than echo the request, for the reason the
	// file API does it: the point of the answer is that the page can verify
	// what landed. Decoded again on the way out, off those same bytes, so the
	// answer is the desk's reading of the file and not the page's of its own
	// request.
	wrotePresent, wrote, err := s.readDeskFile()
	if err != nil || !wrotePresent {
		return http.StatusInternalServerError, errorBody(fmt.Errorf(
			"%s was written and could not be read back: %v", path, err))
	}
	// The read-back is held to the read's own contract, UTF-8 included: a file
	// this desk wrote and its own reader would refuse is a defect to report
	// rather than a success to announce.
	if !validUTF8(wrote) {
		return http.StatusInternalServerError, errorBody(fmt.Errorf(
			"%s was written and does not read back as UTF-8 text", path))
	}
	landed := decodeDeskFile(wrote)
	if landed.refused() {
		// Unreachable: the same bytes decoded clean a moment ago. It is an
		// answer rather than a panic because the alternative is a desk that
		// wrote a file and then said nothing about it.
		return http.StatusInternalServerError, errorBody(fmt.Errorf(
			"%s was written and does not read back as a configuration: %s",
			path, describeProblems(landed.Problems)))
	}
	// Whether the key on this machine is still the key for what was just
	// configured. Read after the write, off the file that landed, so it is a
	// statement about the desk's actual state rather than about the request.
	// A read that fails is reported as "rebind required": the honest answer
	// where the binding cannot be established is that it has not been.
	rebind := false
	if stored, kerr := s.assistant.readKey(); kerr != nil {
		rebind = true
	} else if stored.present {
		rebind = landed.Endpoint == nil || bindingProblem(stored, *landed.Endpoint) != ""
	}
	s.log.Printf("desk: the desk-level assistant configuration was written on this machine")
	return http.StatusOK, deskConfigWritten{
		Path:              path,
		SHA256:            digestOf(wrote),
		Assistant:         slotView(landed),
		Project:           projectView(landed),
		Created:           !present,
		KeyRebindRequired: rebind,
	}
}

// composeDeskFile builds the bytes this desk would store.
//
// **Every member's own bytes, in the file's own order, re-indented and never
// re-serialised.** `json.Indent` rewrites the whitespace between tokens and
// nothing else, so a number stays the number that was written — `1e2` does not
// become `100`, and an integer past a float64's precision is not rounded on
// its way through a `map[string]any`. That is the same canonical-bytes rule
// the rest of this repository decides by: what is checked must be what is
// stored.
//
// The `assistant` member is replaced where it is, or appended where the file
// had none, so a file's own layout survives a write to one member of it.
//
// An absent file becomes the smallest one this decoder accepts: the version
// this desk declares, and the assistant object. The version is the chassis'
// own constant rather than something the page supplies, because a page that
// could choose it could ask this desk to write a file it will not read.
func composeDeskFile(
	current []byte, present bool, assistant, project json.RawMessage,
) ([]byte, []deskProblem) {
	// **Only the members this request named.** One that was not sent is not
	// replaced, not removed and not re-rendered — it is copied across with
	// every other member of the file, which is what lets the Project card save
	// without sending the assistant slot and the Assistant form save without
	// sending the project one.
	written := map[string]json.RawMessage{}
	for _, member := range []struct {
		name string
		raw  json.RawMessage
	}{{"assistant", assistant}, {"project", project}} {
		if len(member.raw) == 0 {
			continue
		}
		var indented bytes.Buffer
		if err := json.Indent(&indented, member.raw, "  ", "  "); err != nil {
			return nil, []deskProblem{{Key: member.name,
				Reason: "is not JSON, so nothing was written"}}
		}
		written[member.name] = json.RawMessage(indented.String())
	}
	members := []deskMember{}
	if present {
		var err error
		var duplicate string
		members, duplicate, err = topLevelMembers(current)
		if duplicate != "" {
			// Named by its own key, the way every other refusal on this route
			// is: whoever has to repair the file needs the member, not a
			// sentence about parsing.
			return nil, []deskProblem{{Key: duplicate, Reason: fmt.Sprintf(
				"appears twice at the top level of %s, and two readers of that file would "+
					"not agree which value it has; this desk will not compose over one",
				deskConfigName)}}
		}
		if err != nil {
			return nil, []deskProblem{{Key: "", Reason: fmt.Sprintf(
				"the file on disk could not be read member by member, so nothing was "+
					"written: %v", err)}}
		}
	} else {
		members = append(members, deskMember{
			name: "deskConfigVersion",
			raw:  json.RawMessage(fmt.Sprintf("%d", deskConfigVersion)),
		})
	}

	// In the file's own order, so a member this request names keeps its place
	// and one it does not name is never moved.
	for _, name := range []string{"assistant", "project"} {
		raw, sent := written[name]
		if !sent {
			continue
		}
		replaced := false
		for index, member := range members {
			if member.name != name {
				continue
			}
			members[index].raw = raw
			members[index].rendered = true
			replaced = true
		}
		if !replaced {
			members = append(members, deskMember{name: name, raw: raw})
		}
	}

	var out bytes.Buffer
	out.WriteString("{\n")
	for index, member := range members {
		out.WriteString("  ")
		encoded, err := json.Marshal(member.name)
		if err != nil {
			return nil, []deskProblem{{Key: member.name,
				Reason: "could not be written back as a member name"}}
		}
		out.Write(encoded)
		out.WriteString(": ")
		// **The member's own bytes, unless this route wrote them.** Round 1
		// found the earlier version passing every retained member through
		// `json.Indent`, which rewrites the whitespace inside it — so the
		// "byte for byte" claim held only for a file that was already in
		// exactly the shape `json.Indent` emits, which is the shape the one
		// test happened to use. A member this route was not asked about is now
		// copied verbatim; only `assistant`, which it *was* asked about, is
		// rendered.
		out.Write(member.raw)
		if index < len(members)-1 {
			out.WriteString(",")
		}
		out.WriteString("\n")
	}
	out.WriteString("}\n")
	return out.Bytes(), nil
}

// deskMember is one top-level member, by name and by its own bytes.
//
// `rendered` marks the one member this route composed itself; every other
// member's `raw` is a slice of the file on disk and is written back unchanged.
type deskMember struct {
	name     string
	raw      json.RawMessage
	rendered bool
}

// topLevelMembers is the top-level members of a JSON object, in the order the
// file writes them and **with each value's own bytes**.
//
// **Order, because a member's place in a file somebody wrote is theirs**, and
// bytes for the same reason: a rewrite that reordered `identity` and
// `assistant`, or reflowed the whitespace inside one, would be this desk
// editing a file it was asked to leave alone.
//
// **A duplicate top-level name is an error rather than a collapse.** The
// earlier version took values from a map and positions from the walk, so two
// spellings of one name became the *last* value written at the *first*
// position — a rewrite that silently changed what the file says. `encoding/json`
// keeps the last, a JSON reader in another language may keep the first, and a
// file two readers disagree about is one this desk will not compose over. The
// caller refuses the write and names the member.
func topLevelMembers(data []byte) ([]deskMember, string, error) {
	decoder := json.NewDecoder(bytes.NewReader(data))
	opening, err := decoder.Token()
	if err != nil {
		return nil, "", err
	}
	if delimiter, ok := opening.(json.Delim); !ok || delimiter != '{' {
		return nil, "", errors.New("not a JSON object")
	}
	var members []deskMember
	seen := map[string]bool{}
	for decoder.More() {
		name, err := decoder.Token()
		if err != nil {
			return nil, "", err
		}
		key, ok := name.(string)
		if !ok {
			return nil, "", errors.New("a member name is not a string")
		}
		if seen[key] {
			return nil, key, nil
		}
		seen[key] = true
		// The value's own bytes. `json.RawMessage` is handed the slice the
		// decoder read, whitespace inside it included.
		var value json.RawMessage
		if err := decoder.Decode(&value); err != nil {
			return nil, "", err
		}
		members = append(members, deskMember{name: key, raw: value})
	}
	// **The closing brace, and then nothing.** Round 2 found this returning as
	// soon as `More` said there was no next member — which is true of a
	// *truncated* object as well as a closed one, and says nothing about what
	// follows. So `{"deskConfigVersion":1` and `{"deskConfigVersion":1} junk`
	// were both walked happily and rewritten into well-formed JSON with the
	// malformed or trailing bytes silently dropped. That is a repair, and this
	// route replaces one member; it does not tidy a file up on the way past.
	closing, err := decoder.Token()
	if err != nil {
		return nil, "", fmt.Errorf("the object is not closed: %w", err)
	}
	if delimiter, ok := closing.(json.Delim); !ok || delimiter != '}' {
		return nil, "", errors.New("the object is not closed")
	}
	// `Decoder.Token` answers `io.EOF` where only whitespace is left, so a
	// file that ends in a newline is not a file with something after it.
	if _, err := decoder.Token(); !errors.Is(err, io.EOF) {
		return nil, "", errors.New("there is something after the object")
	}
	return members, "", nil
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
	// Origin and Kind are the destination this key was entered for: the
	// scheme and host of the endpoint configured at the moment it was stored,
	// and that endpoint's wire protocol. Empty where there is no key.
	//
	// **Reported so the page can say where the key goes**, which is the whole
	// user-facing half of the binding: a form that says "key stored for
	// api.example.invalid" is a form whose reader can see that changing the
	// endpoint means entering it again. Neither member is a secret — the
	// origin is already in the file the page reads.
	Origin string `json:"origin"`
	Kind   string `json:"kind"`
	// ConfiguredOrigin is the origin of the endpoint this desk is configured
	// for **right now**, computed here, and empty where none is configured or
	// the configured URL has no origin to take.
	//
	// Bound is this desk's own verdict about the pair: whether the stored key
	// would be presented to the endpoint the file names. **Both are here
	// because the page must not compute either.** It tried, with the browser's
	// `URL`, which drops an explicit `:443` where Go's `url.Parse` keeps it —
	// so a key stored for a host and a configuration naming the same host with
	// its default port written out showed as bound on the page while the relay
	// answered `assistant-key-unbound` and sent nothing. Two implementations
	// of one rule is one implementation too many, and the one that decides is
	// the one that presents the credential.
	ConfiguredOrigin string `json:"configuredOrigin"`
	// ConfiguredKind is that endpoint's wire protocol, empty alongside an
	// empty origin. It is here for the same reason the origin is: the row that
	// names *both* destinations — the one the key was entered for and the one
	// this desk is configured for — should read both of them from the desk
	// that decides, and not half from the desk and half from a configuration
	// the page may not have been able to read.
	ConfiguredKind string `json:"configuredKind"`
	Bound          bool   `json:"bound"`
}

// keyState renders one stored key as the answer the page gets, together with
// this desk's verdict about where it may go.
//
// `configured` is the endpoint the file names now, or the zero value where
// there is none. A desk with no endpoint is never `Bound`: there is nothing to
// present the key to, and storing one requires something to bind it to.
func keyState(stored storedKey, configured assistantEndpoint) AssistantKeyState {
	origin, ok := endpointOrigin(configured.url)
	kind := configured.kind
	if !ok {
		origin, kind = "", ""
	}
	if !stored.present {
		return AssistantKeyState{ConfiguredOrigin: origin, ConfiguredKind: kind}
	}
	return AssistantKeyState{
		Present:          true,
		Fingerprint:      fingerprint(stored.key),
		Origin:           stored.origin,
		Kind:             stored.kind,
		ConfiguredOrigin: origin,
		ConfiguredKind:   kind,
		// **The relay's own predicate, not a second reading of it.** A verdict
		// computed any other way here would be a third implementation of the
		// rule two others already hold.
		Bound: origin != "" && bindingProblem(stored, configured) == "",
	}
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
	stored, err := s.assistant.readKey()
	if err != nil {
		s.refuseKeyRead(w, err)
		return
	}
	// **A configuration this desk cannot read is not an endpoint.** The zero
	// value gives no origin and no binding, which is the honest answer: a
	// refused file authorises no relayed request either, so a key it names
	// nothing for is a key that goes nowhere.
	configured, _ := s.configuredEndpoint()
	writeJSON(w, http.StatusOK, keyState(stored, configured))
}

// refuseKeyRead answers a key read that found something and could not use it.
//
// A refusal carrying its own code answers with it — the code-to-status matrix
// is the one place that decides — and everything else is genuinely a failure
// here. The same shape `refuseDeskRead` has, for the same reason.
func (s *Server) refuseKeyRead(w http.ResponseWriter, err error) {
	if code := codeOf(err); code != CodeInternal {
		writeJSONError(w, statusForRefusal(err), err)
		return
	}
	writeJSONCoded(w, http.StatusInternalServerError, CodeInternal,
		fmt.Sprintf("the assistant key could not be read: %v", err))
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
	// **Storing a key binds it**, and that is why an endpoint is required to
	// store one at all. The scheme, host and wire protocol of the endpoint
	// configured at this instant are kept beside the key, and neither the
	// probe nor the relay presents it anywhere else — so a configuration write
	// can move the endpoint and cannot move the credential.
	//
	// Refused where there is no endpoint, and refused with the same code and
	// sentence Admin already renders for that state: a key with nothing to be
	// bound to would be a key bound to whatever is configured next, which is
	// the arrangement this replaces.
	endpoint, err := s.configuredEndpoint()
	if err != nil {
		writeJSONError(w, statusForRefusal(err), err)
		return
	}
	origin, ok := endpointOrigin(endpoint.url)
	if !ok {
		writeJSONCoded(w, http.StatusConflict, CodeAssistantUnconfigured,
			"the configured endpoint is not an address a key can be bound to")
		return
	}
	bound := storedKey{present: true, key: key, origin: origin, kind: endpoint.kind}
	if err := s.assistant.storeKey(bound); err != nil {
		writeJSONCoded(w, http.StatusInternalServerError, CodeInternal,
			fmt.Sprintf("the assistant key could not be stored: %v", err))
		return
	}
	// The event and the destination, never the value. This line is what the
	// "never in the log" test is measured against: a log with nothing in it
	// proves nothing about a handler that never ran. The origin is the same
	// scheme-and-host every other line here carries.
	s.log.Printf("desk: the assistant key was stored on this machine for %s", origin)
	writeJSON(w, http.StatusOK, keyState(bound, endpoint))
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
	// The endpoint is still configured after a removal — the two are separate,
	// which is the sentence Admin carries — so the origin travels and the
	// verdict is the one a desk with no key has.
	configured, _ := s.configuredEndpoint()
	writeJSON(w, http.StatusOK, keyState(storedKey{}, configured))
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
//
// **`gemini` is the third protocol and not a third vendor.** It names Google's
// native Gemini wire — the one that carries thought parts, thought signatures
// across tool turns and an explicit thinking budget, none of which exist on
// that vendor's OpenAI-compatibility layer — and an endpoint speaking it is
// configured in the same four fields as any other, at whatever URL its
// operator documents. Nothing here reads the host, and a self-hosted or
// proxied endpoint speaking that wire is this kind too.
var AssistantKinds = []string{"openai-compatible", "anthropic", "gemini"}

// AssistantTools is the closed set of runtime tools the assistant may be
// configured to call.
//
// Held here as well as in the page's decoder because both sides refuse by it,
// and a test reads this declaration to hold the two lists to one answer. Every
// one of them is a **read** of the runtime: four questions and a rehearsal.
// There is no write tool on the list because the runtime has none, and no file
// tool because proposing an edit is the assistant's whole reach.
//
// **`list_examples` is on it because the runtime's own prompt calls it.** The
// `author_pack` prompt tells the model to list the examples before asking for
// one, so a list without it grants a capability the prompt then asks for and
// cannot have — an assistant refused by its own instructions. It is a read
// like the rest: the names of the examples the runtime serves.
var AssistantTools = []string{
	"get_schema", "list_examples", "get_example", "validate", "experimental_evaluate",
}

// AssistantEngines is the closed set of loops that may run the assistant.
//
// **A name, and no vendor discriminator** — the identity slot's precedent, for
// the identity slot's reason. ADR-0001 makes the engine a slot precisely so
// that the desk's promises (propose-only, rehearsal-only, the tool allow-list,
// key custody) are held *below* whatever runs the loop, and an engine ships
// only once it has passed the desk's own conformance session. A value outside
// this list is therefore refused by name rather than ignored: a configuration
// naming an engine nobody certified is a configuration asking for one.
//
// `vercel` is the default and `builtin` is the keyless fallback. Nothing in
// this release reads either: the member is stored and shown.
var AssistantEngines = []string{"vercel", "builtin"}

// AssistantThinkingTiers is the closed set of depths the engine may be asked
// to run the model's reasoning at.
//
// `off` is the default. The two states the tier cannot express — a model that
// always thinks, and an endpoint that offers no thinking at all — are the
// desk's to report at the moment they are discovered, not settings for anyone
// to choose. Nothing in this release acts on this member either.
var AssistantThinkingTiers = []string{"off", "on", "ultra"}

// The values a file that names neither member decodes to.
//
// Declared here rather than left implicit at the decoder, because they are part
// of the contract the browser shares: `DESK_DEFAULTS.assistant` in
// `deskConfig.ts` carries the same two, and the shared fixture corpus now
// asserts the decoded values on both sides, so a default changed on one side
// and not the other fails on both.
const (
	defaultAssistantEngine   = "vercel"
	defaultAssistantThinking = "off"
)

// assistantSlot is what a decode of the `assistant` section yields: the
// endpoint if there is a usable one, and the two settings with their defaults
// applied.
type assistantSlot struct {
	endpoint *assistantEndpoint
	engine   string
	thinking string
}

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
	stored, err := s.assistant.readKey()
	if err != nil {
		s.refuseKeyRead(w, err)
		return
	}
	if !stored.present {
		writeJSONCoded(w, http.StatusConflict, CodeAssistantNoKey,
			"no key is stored on this machine, so there is nothing to present to the endpoint")
		return
	}
	// **The binding, checked before a socket is opened.** The key on this
	// machine was entered for one destination; a configuration that now names
	// another gets no credential at all, and this refusal rather than a
	// request.
	if reason := bindingProblem(stored, endpoint); reason != "" {
		writeJSONCoded(w, http.StatusConflict, CodeAssistantKeyUnbound, reason)
		return
	}
	result := probeEndpoint(r.Context(), endpoint, stored.key)
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
	return probeAddressWithQuery(base, suffix, "")
}

// probeAddressWithQuery is probeAddress with one raw pair of the desk's own
// added after the configured query.
//
// **The configured query first, then the pair.** It is the endpoint's own
// routing and it keeps its place; what this desk adds goes after it, which is
// the same order `relayTarget` uses for the one query pair the relay admits.
// One order, written once, so the probe and the relay cannot disagree about
// what a configured `?route=eu` endpoint is sent.
//
// `ForceQuery` is cleared because a base written as `https://gw/v1?` would
// otherwise emit `?` and then this pair after it, which is a query two readers
// could disagree about — the one thing this desk will not send.
func probeAddressWithQuery(base, suffix, pair string) string {
	parsed, err := url.Parse(base)
	if err != nil {
		if pair == "" {
			return base + suffix
		}
		return base + suffix + "?" + pair
	}
	appendPath(parsed, suffix)
	if pair != "" {
		parsed.RawQuery = appendQueryPair(parsed.RawQuery, pair)
		parsed.ForceQuery = false
	}
	return parsed.String()
}

// appendPath adds a path segment to a URL **without re-encoding what is
// already there**.
//
// `u.Path` is the *decoded* path, and writing to it alone leaves `u.RawPath`
// describing something else — which `String` then resolves by re-encoding from
// the decoded form. For a base of `/tenant%2Fone` that turns one segment into
// two: the request goes to `/tenant/one/models`, a different resource from the
// one configured, with the credential attached. `%2e` is the same defect in
// another dress.
//
// So both fields are set together, from the escaped form: the escaped path is
// what the endpoint was configured with, and it travels unchanged.
func appendPath(u *url.URL, suffix string) {
	escaped := strings.TrimRight(u.EscapedPath(), "/") + suffix
	decoded, err := url.PathUnescape(escaped)
	if err != nil {
		// Not decodable, which means the configured path is not something Go
		// will re-encode faithfully either. Setting Opaque is not available
		// here, so the escaped form is used for both and `String` emits it
		// verbatim.
		u.Path = escaped
		u.RawPath = escaped
		return
	}
	u.Path = decoded
	// RawPath is honoured only where it is a valid encoding of Path; setting
	// both from the same source is what makes it so.
	u.RawPath = escaped
}

// endpointOrigin is the part of a configured URL a key is bound to: its scheme
// and its host, and nothing else.
//
// **Scheme and host, not the whole URL**, and the line is worth stating
// exactly. A path or a query is the endpoint's own routing and an author
// changes one without changing who is at the other end — binding to the whole
// URL would mean re-entering the key to add `?route=eu`, which is a rule
// nobody would keep. A *host* change is a different party. So the binding is
// the origin, and the port is in it: `https://gw.example:8443` and
// `https://gw.example` are two destinations.
//
// Lower-cased, because a host is case-insensitive and a binding that answered
// differently for `API.example` than for `api.example` would be a binding two
// readers disagree about. `ok` is false for a URL with no host, which
// `endpointURLProblem` already refuses.
func endpointOrigin(raw string) (string, bool) {
	parsed, err := url.Parse(raw)
	if err != nil || parsed == nil || parsed.Host == "" {
		return "", false
	}
	return strings.ToLower(parsed.Scheme) + "://" + strings.ToLower(parsed.Host), true
}

// bindingProblem is why a stored key may not be presented to this endpoint, or
// the empty string where it may.
//
// **The whole of HIGH 1's answer is these six lines.** Page code can write the
// desk-level file, so it can name any endpoint it likes — and that is fine,
// because the credential does not follow. What the key travels to is the
// destination it was entered for, and entering one is something only a person
// at the keyboard can do: no endpoint returns the key, nothing in this package
// sends it to the browser, and the store endpoint takes a value the page must
// already have.
func bindingProblem(stored storedKey, endpoint assistantEndpoint) string {
	origin, ok := endpointOrigin(endpoint.url)
	if !ok {
		return "the configured endpoint is not an address a key can be presented to"
	}
	if stored.origin == origin && stored.kind == endpoint.kind {
		return ""
	}
	// The stored binding is named because the reader has to be able to see
	// which of the two moved. Neither half is a secret: both are in the file
	// the page already reads.
	return fmt.Sprintf(
		"the key on this machine was entered for %s over %q, and this desk is configured for "+
			"%s over %q; nothing was sent. A key travels only to the endpoint it was entered "+
			"for — store it again on Admin › Assistant to bind it to this one",
		stored.origin, stored.kind, origin, endpoint.kind)
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
	// Transport is nil, which is `http.DefaultTransport` — TLS verification
	// and all. It is a field rather than an omission so that a test can count
	// what actually leaves this process: "no outbound request was made" is a
	// claim about the transport, and the only way to check it is at the
	// transport. A stub the fixture never names cannot establish it.
	Transport: nil,
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
	// **Drained to the end, and discarded.** Both halves are deliberate and
	// the first one was previously overstated: this used to stop at 8 KiB and
	// the README said "drained", which is not the same thing — a body longer
	// than the bound left the connection unreusable. The drain is bounded by
	// the request's own ten-second deadline rather than by a byte count, so an
	// endpoint that streams for ever is cut off by the timeout that governs
	// everything else here. Nothing read is repeated to anybody.
	_, _ = io.Copy(io.Discard, response.Body)
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

// attachCredential presents the key the way this protocol requires.
//
// **The table it reads is `credentialHeader`, in `modelrelay.go`, and it is
// the only one.** The probe and the relay present the same credential to the
// same endpoint; two tables would be two answers about what an `anthropic`
// endpoint is sent, and the one that was wrong would be wrong with a key in
// it. A kind neither of them defines attaches nothing at all rather than
// guessing — `decodeDeskFile` refuses every such kind by name, so reaching
// here with one is a bug rather than a configuration.
func attachCredential(header http.Header, kind, key string) {
	name, value, ok := credentialHeader(kind, key)
	if !ok {
		return
	}
	header.Set(name, value)
}

func probeRequest(ctx context.Context, endpoint assistantEndpoint, key string) (*http.Request, error) {
	switch endpoint.kind {
	case "openai-compatible":
		request, err := http.NewRequestWithContext(ctx, http.MethodGet,
			probeAddress(endpoint.url, "/models"), nil)
		if err != nil {
			return nil, err
		}
		attachCredential(request.Header, endpoint.kind, key)
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
		attachCredential(request.Header, endpoint.kind, key)
		// The version this protocol requires on every request. It is a
		// property of the wire, not a model or a vendor choice, and an
		// endpoint speaking this protocol refuses a request without it.
		request.Header.Set("anthropic-version", "2023-06-01")
		request.Header.Set("content-type", "application/json")
		return request, nil
	case "gemini":
		// The model listing, bounded to one entry. A `GET`, which creates
		// nothing and costs nothing, and which exercises the credential rather
		// than merely the route — this listing answers 401 without one.
		//
		// **`pageSize=1` and not the whole catalogue**: a reachability check
		// has no use for the rest of it, and a probe that pulled every model
		// on every press would be a probe with an opinion about how much of
		// somebody's quota a button may spend.
		request, err := http.NewRequestWithContext(ctx, http.MethodGet,
			probeAddressWithQuery(endpoint.url, "/v1beta/models", "pageSize=1"), nil)
		if err != nil {
			return nil, err
		}
		attachCredential(request.Header, endpoint.kind, key)
		return request, nil
	default:
		// Unreachable: `decodeDeskFile` refuses every other kind by name.
		return nil, fmt.Errorf("no probe is defined for %q", endpoint.kind)
	}
}
