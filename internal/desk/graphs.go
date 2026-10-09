package desk

// A project's graphs: the findings and the plan (ADR-0011, section 3, row 1).
//
// Desk's Go runs the runtime's experimental graph commands that only the CLI
// serves, through `runRuntime` and its bounds, and hands the page what the
// runtime printed:
//
//   - `GET /api/graphs/findings` runs `experimental graph validate`.
//   - `GET /api/graphs/plan?id=<configured id>` runs `experimental graph list`
//     to turn the id into the path the listing reports, and then
//     `experimental graph explain <that path>`.
//
// **The page names a graph by its configured id and never by a path.** A path
// from the page is not read, so none can reach the runtime. An id the listing
// does not have is refused, and so is a listed path that is not a path inside
// the project's own folder.
//
// **Exact bytes stay exact.** The answer is embedded as the runtime printed
// it. The only change is a redaction, and it changes only the string it
// redacts (`shownGraphAnswer`): every other byte of the answer is the
// runtime's, including its order, its numbers and its escapes.
//
// **No absolute path reaches the page** (ADR-0011, section 2): the
// path-bearing members (`path`, `rowsPath`, `graphPath`, `configPath`) and
// every `message` and `detail` pass the redaction the decision record uses
// (`withoutPathsUnder`), and so does any other string that names this
// project's folder.
//
// Nothing here writes, and nothing here reads Desk's own storage.

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"path/filepath"
	"regexp"
	"strings"
)

// graphOutputVersion is the output version of the graph commands Desk reads.
const graphOutputVersion = "2"

// graphPathMembers are the members of a graph payload that hold a path.
var graphPathMembers = map[string]bool{"path": true, "rowsPath": true, "graphPath": true, "configPath": true}

// graphMessageMembers are the members of a graph payload that hold a sentence.
var graphMessageMembers = map[string]bool{"message": true, "detail": true}

// drivePath is a path from a drive letter.
var drivePath = regexp.MustCompile(`^[A-Za-z]:[\\/]`)

// graphRuntime is the directory and refusal for running the graph commands
// over this desk's project: as the review step has it, a startup desk whose
// runtime reads another configuration than this project's does not run them.
func (s *Server) graphRuntime() (heldDir, string) {
	dir, named, ok := s.projectRuntime()
	switch {
	case !ok:
		return heldDir{}, "This desk holds no project whose graphs could be checked."
	case named != "":
		return dir, "This project's runtime reads the configuration that JPACK_CONFIG names where Desk was started, and not this project's " + runtimeConfigName + ", so Desk does not check its graphs here."
	}
	return dir, ""
}

// graphDiagnostics is the part of an answer the refusals read.
type graphDiagnostics struct {
	OutputVersion string              `json:"outputVersion"`
	Command       string              `json:"command"`
	Status        string              `json:"status"`
	Diagnostics   []runtimeDiagnostic `json:"diagnostics"`
}

// runGraph runs one graph command and returns the answer as printed, after
// checking it is the command's answer in the output version Desk reads. The
// answer is read whatever the exit: `validate` exits 1 when any check failed,
// and `explain` of a graph the runtime cannot plan exits non-zero with the
// runtime's diagnostics.
func (s *Server) runGraph(ctx context.Context, dir heldDir, command string, args ...string) (json.RawMessage, graphDiagnostics, error) {
	return s.runGraphInput(ctx, dir, command, nil, args...)
}

func (s *Server) runGraphInput(ctx context.Context, dir heldDir, command string, input []byte, args ...string) (json.RawMessage, graphDiagnostics, error) {
	full := append([]string{"experimental", "graph", command}, args...)
	full = append(full, "--config", runtimeConfigName, "--format", "json")
	out, runErr := runRuntimeInput(ctx, s.cfg.JpackBin, dir, input, full...)
	var head graphDiagnostics
	if len(bytes.TrimSpace(out)) == 0 {
		if runErr == nil {
			runErr = fmt.Errorf("its graph %s did not answer as documented", command)
		}
		return nil, head, runErr
	}
	trimmed := bytes.TrimSpace(out)
	if !json.Valid(trimmed) || json.Unmarshal(trimmed, &head) != nil || head.Command != "experimental graph "+command || head.Status == "" {
		if runErr == nil {
			runErr = fmt.Errorf("its graph %s did not answer as documented", command)
		}
		return nil, head, runErr
	}
	if head.OutputVersion != graphOutputVersion {
		return nil, head, fmt.Errorf("its graph %s answered in an output version Desk does not read", command)
	}
	return json.RawMessage(trimmed), head, nil
}

// graphsAnswer is what both routes answer: `{"id":…,"answer":<the runtime's
// answer, as it printed it>}`, the id being the plan's and absent from the
// findings. See writeGraphAnswer.

// handleGraphFindings answers `GET /api/graphs/findings`.
func (s *Server) handleGraphFindings(w http.ResponseWriter, r *http.Request) {
	if !s.guard(w, r) {
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	dir, refusal := s.graphRuntime()
	if refusal != "" {
		writeJSONCoded(w, http.StatusConflict, CodeBadRequest, refusal)
		return
	}
	answer, _, err := s.runGraph(r.Context(), dir, "validate")
	if err != nil {
		s.graphFailed(w, "The graphs could not be checked", err)
		return
	}
	s.writeGraphAnswer(w, "", answer)
}

// handlePlan answers `GET /api/graphs/plan?id=<configured id>`.
func (s *Server) handleGraphPlan(w http.ResponseWriter, r *http.Request) {
	if !s.guard(w, r) {
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	id := r.URL.Query().Get("id")
	if id == "" {
		writeJSONCoded(w, http.StatusBadRequest, CodeBadRequest, "A plan is asked for by a graph's configured id.")
		return
	}
	dir, refusal := s.graphRuntime()
	if refusal != "" {
		writeJSONCoded(w, http.StatusConflict, CodeBadRequest, refusal)
		return
	}
	path, status, refusal, err := s.graphPathOf(r.Context(), dir, id)
	if err != nil {
		s.graphFailed(w, "The plan could not be read", err)
		return
	}
	if refusal != "" {
		writeJSONCoded(w, status, CodeBadRequest, refusal)
		return
	}
	answer, _, err := s.runGraph(r.Context(), dir, "explain", path)
	if err != nil {
		s.graphFailed(w, "The plan could not be read", err)
		return
	}
	s.writeGraphAnswer(w, id, answer)
}

// graphFailed answers a command that gave no answer: with Desk's own words
// and the cause, passed through the redaction.
func (s *Server) graphFailed(w http.ResponseWriter, what string, err error) {
	s.log.Printf("desk: %s: %v", strings.ToLower(what[:1])+what[1:], err)
	writeJSONCoded(w, http.StatusInternalServerError, CodeInternal, what+": "+strings.TrimRight(s.withoutPaths(err.Error()), ".")+".")
}

// writeGraphAnswer sends the runtime's answer for the page, redacted.
func (s *Server) writeGraphAnswer(w http.ResponseWriter, id string, answer json.RawMessage) {
	shown := s.shownGraphAnswer(answer)
	if !bytes.Equal(answer, shown) {
		// The page is told no path; the owner's own log keeps them.
		s.log.Printf("desk: the graph answer, as the runtime said it: %s", answer)
	}
	// **Written by hand, not marshalled.** `encoding/json` rewrites a
	// RawMessage it embeds (it escapes "<", ">", "&" and U+2028 in it), and
	// the runtime's bytes are to reach the page as printed.
	var body bytes.Buffer
	body.WriteByte('{')
	if id != "" {
		body.WriteString(`"id":`)
		body.Write(mustMarshal(id))
		body.WriteByte(',')
	}
	body.WriteString(`"answer":`)
	body.Write(shown)
	body.WriteString("}\n")
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(body.Bytes())
}

// graphListing is the part of `experimental graph list` this reads.
type graphListing struct {
	Graphs []struct {
		ID     string `json:"id"`
		Path   string `json:"path"`
		Detail string `json:"detail"`
	} `json:"graphs"`
}

// graphPathOf turns a configured graph id into the path to pass to `explain`.
//
// It runs `experimental graph list` afresh for this request, and takes the
// `path` the listing reports for exactly that id. The page's text is compared
// with ids and passed to nothing. It is refused, in Desk's words, when:
//
//   - the listing could not be read as the runtime's answer (the cause is
//     returned as an error);
//   - the listing has no such id, or more than one;
//   - the listing carries a `detail` for it: the runtime could not read the
//     document inside the project's own folder, and Desk asks it for no plan;
//   - the path is not a path inside the project's folder (`graphPathInside`).
func (s *Server) graphPathOf(ctx context.Context, dir heldDir, id string) (path string, status int, refusal string, err error) {
	raw, head, err := s.runGraph(ctx, dir, "list")
	if err != nil {
		return "", 0, "", err
	}
	var listing graphListing
	if head.Status != "resolved" || json.Unmarshal(raw, &listing) != nil {
		message := "the runtime's graph list did not resolve"
		for _, d := range head.Diagnostics {
			message = d.Message
			break
		}
		return "", 0, "", errors.New(message)
	}
	found := -1
	for i, graph := range listing.Graphs {
		if graph.ID != id {
			continue
		}
		if found >= 0 {
			return "", http.StatusConflict, "The project's configuration lists more than one graph with that id, so Desk asks for no plan.", nil
		}
		found = i
	}
	if found < 0 {
		return "", http.StatusNotFound, "The project's configuration declares no graph with that id.", nil
	}
	entry := listing.Graphs[found]
	// **Containment first, and on the path as the runtime's listing read it.**
	// The listing cleans a path before it reads it, so "sub/link/../g.json"
	// is "sub/g.json" there; `explain` opens its argument as written, and the
	// kernel would follow the link and then "..". So the cleaned spelling is
	// what is checked and what is passed. A path from outside the project is
	// refused in Desk's words, never in a sentence of the runtime's that
	// names it.
	clean := filepath.Clean(entry.Path)
	if !graphPathInside(entry.Path) || !graphPathInside(clean) {
		return "", http.StatusConflict, "This graph is declared at a path that is not inside the project's folder, so Desk asks for no plan.", nil
	}
	if entry.Detail != "" {
		return "", http.StatusConflict, "The runtime could not read this graph's document, so Desk asks for no plan. The runtime says: " + s.redactionFor().text(entry.Detail), nil
	}
	// A name that starts with "-" is a name, not a flag.
	if strings.HasPrefix(clean, "-") {
		return "./" + clean, 0, "", nil
	}
	return clean, 0, "", nil
}

// graphPathInside reports whether p, as the listing reports it, is a path
// from the project's folder down: not empty, not from a root or a drive, and
// not climbing out.
func graphPathInside(p string) bool {
	if p == "" || strings.ContainsRune(p, 0) || strings.HasPrefix(p, "/") || strings.HasPrefix(p, `\`) || drivePath.MatchString(p) {
		return false
	}
	return filepath.IsLocal(p)
}

// shownGraphAnswer is the runtime's answer with no path in it that reaches
// from a root. It returns the answer unchanged, byte for byte, where nothing
// needed redacting.
//
//  1. **A path member that starts from a root is replaced whole** (`path`,
//     `rowsPath`, `graphPath`, `configPath`, at any depth): by "…", or, where
//     it is under this project's folder, by the redaction every message
//     passes (`withoutPathsUnder`). Not by that redaction alone: it takes a
//     path up to its first space, and a folder can hold a space, a tab or a
//     line separator.
//  2. **Every such path is also replaced whole wherever a sentence names it**
//     (`message`, `detail` and any other string), as the runtime printed it
//     and as it prints a control character (`displayedPath`), before the
//     message passes the redaction.
//  3. **A relative path is shown as given**, unless it holds an absolute path
//     inside it, which is then redacted whole.
func (s *Server) shownGraphAnswer(answer json.RawMessage) json.RawMessage {
	var spans []pathSpan
	var collect func(value any, key string)
	collect = func(value any, key string) {
		switch v := value.(type) {
		case map[string]any:
			for name, member := range v {
				collect(member, name)
			}
		case []any:
			for _, member := range v {
				collect(member, key)
			}
		case string:
			if graphPathMembers[key] && startsFromARoot(v) {
				spans = append(spans, pathSpan{value: v, with: "…"})
				if shown := displayedPath(v); shown != v {
					spans = append(spans, pathSpan{value: shown, with: "…"})
				}
			}
		}
	}
	var decoded any
	if json.Unmarshal(answer, &decoded) == nil {
		collect(decoded, "")
	}
	red := s.redactionFor(spans...)
	return s.shownGraphValue(answer, "", red)
}

// graphRedaction is what one answer is redacted with, held once for the whole
// answer: the audit directory `jpack.json` declares and the spans that every
// absolute path it declares makes. Read per string, the file would be read
// hundreds of times for a large answer, and could change under it.
type graphRedaction struct {
	s        *Server
	spans    []pathSpan
	auditDir string
}

// redactionFor reads `jpack.json` once. Every absolute path it declares for a
// pack, a graph or its rows is a span replaced whole: a sentence of the
// runtime's can name a pack's path that no member of a graph answer carries.
func (s *Server) redactionFor(spans ...pathSpan) graphRedaction {
	red := graphRedaction{s: s, spans: spans}
	config, err := s.readReviewFileWithin(runtimeConfigName, reviewTextLimit)
	if err != nil {
		return red
	}
	red.auditDir, _, _ = auditDirOf(config)
	var declared struct {
		Packs map[string]struct {
			Path string `json:"path"`
		} `json:"packs"`
		Graphs map[string]struct {
			Path string `json:"path"`
			Rows string `json:"rows"`
		} `json:"graphs"`
	}
	if json.Unmarshal(config, &declared) != nil {
		return red
	}
	add := func(p string) {
		if startsFromARoot(p) {
			red.spans = append(red.spans, pathSpan{value: p, with: "…"})
			if shown := displayedPath(p); shown != p {
				red.spans = append(red.spans, pathSpan{value: shown, with: "…"})
			}
		}
	}
	for _, pack := range declared.Packs {
		add(pack.Path)
	}
	for _, graph := range declared.Graphs {
		add(graph.Path)
		add(graph.Rows)
	}
	return red
}

// text is a sentence with no path from a root in it.
func (r graphRedaction) text(message string) string {
	return r.s.withoutPathsUnder(replaceSpans(message, r.spans), r.auditDir)
}

// startsFromARoot reports whether p starts from the root of a file system,
// Unix or a drive.
func startsFromARoot(p string) bool {
	return strings.HasPrefix(p, "/") || strings.HasPrefix(p, `\`) || drivePath.MatchString(p)
}

// shownPathMember is one path member's value, shown.
func (r graphRedaction) shownPathMember(p string) string {
	s := r.s
	if startsFromARoot(p) {
		for _, name := range []string{s.projectDir, s.cfg.ProjectDir, displayedPath(s.projectDir), displayedPath(s.cfg.ProjectDir)} {
			if name != "" && strings.HasPrefix(p, name+"/") {
				return r.text(p)
			}
		}
		return "…"
	}
	if r.text(p) != p {
		return "…"
	}
	return p
}

// shownGraphValue redacts one JSON value: members are visited in the order
// they were printed, and a value is rebuilt only where something under it
// changed, so what did not change is the runtime's own bytes.
func (s *Server) shownGraphValue(raw json.RawMessage, key string, red graphRedaction) json.RawMessage {
	raw = bytes.TrimSpace(raw)
	if len(raw) == 0 {
		return raw
	}
	switch raw[0] {
	case '"':
		var text string
		if json.Unmarshal(raw, &text) != nil {
			return raw
		}
		var shown string
		switch {
		case graphPathMembers[key]:
			shown = red.shownPathMember(text)
		case graphMessageMembers[key] || s.namesProject(text) || namesSpan(text, red.spans):
			shown = red.text(text)
		default:
			return raw
		}
		if shown == text {
			return raw
		}
		return mustMarshal(shown)
	case '[':
		var items []json.RawMessage
		if json.Unmarshal(raw, &items) != nil {
			return raw
		}
		changed := false
		for i, item := range items {
			if shown := s.shownGraphValue(item, key, red); !bytes.Equal(shown, bytes.TrimSpace(item)) {
				items[i], changed = shown, true
			}
		}
		if !changed {
			return raw
		}
		return joinJSON('[', ']', nil, items)
	case '{':
		decoder := json.NewDecoder(bytes.NewReader(raw))
		if _, err := decoder.Token(); err != nil {
			return raw
		}
		var keys []string
		var values []json.RawMessage
		changed := false
		for decoder.More() {
			name, err := decoder.Token()
			text, ok := name.(string)
			if err != nil || !ok {
				return raw
			}
			var value json.RawMessage
			if decoder.Decode(&value) != nil {
				return raw
			}
			shown := s.shownGraphValue(value, text, red)
			if !bytes.Equal(shown, bytes.TrimSpace(value)) {
				changed = true
			}
			keys, values = append(keys, text), append(values, shown)
		}
		if !changed {
			return raw
		}
		return joinJSON('{', '}', keys, values)
	}
	return raw
}

// namesSpan reports whether text holds any of the spans.
func namesSpan(text string, spans []pathSpan) bool {
	for _, span := range spans {
		if span.value != "" && strings.Contains(text, span.value) {
			return true
		}
	}
	return false
}

// joinJSON is values as one array or object, compact.
func joinJSON(open, closing byte, keys []string, values []json.RawMessage) json.RawMessage {
	var out bytes.Buffer
	out.WriteByte(open)
	for i, value := range values {
		if i > 0 {
			out.WriteByte(',')
		}
		if keys != nil {
			out.Write(mustMarshal(keys[i]))
			out.WriteByte(':')
		}
		out.Write(value)
	}
	out.WriteByte(closing)
	return out.Bytes()
}

// mustMarshal is a string as JSON, without HTML escaping.
func mustMarshal(text string) json.RawMessage {
	var out bytes.Buffer
	encoder := json.NewEncoder(&out)
	encoder.SetEscapeHTML(false)
	_ = encoder.Encode(text)
	return bytes.TrimRight(out.Bytes(), "\n")
}

// namesProject reports whether text holds a name of this project's folder, as
// the runtime prints it or as Desk holds it.
func (s *Server) namesProject(text string) bool {
	names := []string{s.projectDir, s.cfg.ProjectDir}
	if real, err := filepath.EvalSymlinks(s.projectDir); err == nil {
		names = append(names, real)
	}
	for _, name := range names {
		if name == "" {
			continue
		}
		if strings.Contains(text, name) || strings.Contains(text, displayedPath(name)) {
			return true
		}
	}
	return procFD.MatchString(text)
}
