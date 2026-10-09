package desk

// Rehearsals keep inputs and answers only on the page (ADR-0011, section 6).
import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"regexp"
	"strconv"
	"strings"
)

// The body is the runtime's inputs document, keyed by node id. The query names
// only a configured id; neither inputs nor runtime diagnostics enter the log.
func (s *Server) handleGraphEvaluate(w http.ResponseWriter, r *http.Request) {
	if !s.guard(w, r) {
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	input, err := io.ReadAll(io.LimitReader(r.Body, maxFileBytes+1))
	if len(input) > maxFileBytes {
		writeJSONCoded(w, http.StatusRequestEntityTooLarge, CodeTooLarge, "The rehearsal inputs exceed 4 MiB.")
		return
	}
	trimmed := bytes.TrimSpace(input)
	if err != nil || !json.Valid(trimmed) || len(trimmed) == 0 || trimmed[0] != '{' {
		writeJSONCoded(w, http.StatusBadRequest, CodeBadRequest, "The rehearsal inputs must be a JSON object keyed by node id.")
		return
	}
	id := r.URL.Query().Get("id")
	if id == "" {
		writeJSONCoded(w, http.StatusBadRequest, CodeBadRequest, "A rehearsal is asked for by a graph's configured id.")
		return
	}
	dir, refusal := s.graphRuntime()
	if refusal != "" { // another configuration serves this desk
		writeJSONCoded(w, http.StatusConflict, CodeBadRequest, refusal)
		return
	}
	path, status, refusal, err := s.graphPathOf(r.Context(), dir, id)
	if err != nil {
		s.graphRehearsalFailed(w, err)
		return
	}
	if refusal != "" {
		writeJSONCoded(w, status, CodeBadRequest, refusal)
		return
	}
	answer, _, err := s.runGraphInput(r.Context(), dir, "evaluate", input, path, "--inputs", "-", "--rehearsal")
	if err != nil {
		s.graphRehearsalFailed(w, err) // the runtime refused or failed
		return
	}
	var head struct {
		Rehearsal bool `json:"rehearsal"`
	}
	shown := s.shownGraphAnswer(answer, input)
	var refusalHead struct {
		Status      string              `json:"status"`
		Diagnostics []runtimeDiagnostic `json:"diagnostics"`
	}
	if json.Unmarshal(shown, &refusalHead) == nil && refusalHead.Status != "evaluated" && len(refusalHead.Diagnostics) > 0 {
		message := "The runtime refused the rehearsal:"
		for _, diagnostic := range refusalHead.Diagnostics {
			message += "\n" + diagnostic.Code + ": " + diagnostic.Message
		}
		writeJSONCoded(w, http.StatusBadGateway, CodeInternal, message)
		return
	}
	if json.Unmarshal(answer, &head) != nil || !head.Rehearsal {
		// Never show an unmarked answer as a result or retry without the flag.
		writeJSONCoded(w, http.StatusBadGateway, CodeInternal, "The runtime did not return rehearsal: true. The runtime says: "+string(shown))
		return
	}
	s.writeShownGraphAnswer(w, id, shown)
}

func (s *Server) graphRehearsalFailed(w http.ResponseWriter, err error) {
	writeJSONCoded(w, http.StatusInternalServerError, CodeInternal, "The rehearsal could not be run: "+s.redactionFor().text(err.Error()))
}

// Only quoted pointers that resolve into the supplied document or a node's
// facts/evidence are held out of path redaction. Other absolute paths still go.
var quotedRehearsalPointer = regexp.MustCompile(`"(?:[^"\\]|\\.)*"`)

func (r graphRedaction) rehearsalText(message string) string {
	prefix := "\x01pointer"
	for strings.Contains(message, prefix) {
		prefix += "_"
	}
	var held []string
	message = quotedRehearsalPointer.ReplaceAllStringFunc(message, func(quoted string) string {
		var pointer string
		if json.Unmarshal([]byte(quoted), &pointer) != nil || !strings.HasPrefix(pointer, "/") {
			return quoted
		}
		for _, root := range r.pointerRoots {
			if rehearsalPointerResolves(root, pointer) {
				token := prefix + strconv.Itoa(len(held)) + "\x02"
				held = append(held, quoted)
				return token
			}
		}
		return quoted
	})
	message = r.s.withoutPathsUnder(replaceSpans(message, r.spans), r.auditDir)
	for i, quoted := range held {
		message = strings.ReplaceAll(message, prefix+strconv.Itoa(i)+"\x02", quoted)
	}
	return message
}

func rehearsalPointerResolves(value any, pointer string) bool {
	for _, part := range strings.Split(pointer[1:], "/") {
		// Decode RFC 6901 escapes; malformed escapes do not name a pointer.
		for i := 0; i < len(part); i++ {
			if part[i] == '~' {
				if i+1 == len(part) || (part[i+1] != '0' && part[i+1] != '1') {
					return false
				}
				i++
			}
		}
		part = strings.ReplaceAll(strings.ReplaceAll(part, "~1", "/"), "~0", "~")
		switch v := value.(type) {
		case map[string]any:
			var ok bool
			value, ok = v[part]
			if !ok {
				return false
			}
		case []any:
			index, err := strconv.Atoi(part)
			if err != nil || index < 0 || index >= len(v) || strconv.Itoa(index) != part {
				return false
			}
			value = v[index]
		default:
			return false
		}
	}
	return true
}
