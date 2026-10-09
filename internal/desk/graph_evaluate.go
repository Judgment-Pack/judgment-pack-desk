package desk

// Rehearsals keep inputs and answers only on the page (ADR-0011, section 6).
import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
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
	if refusal != "" {
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
		s.graphRehearsalFailed(w, err)
		return
	}
	var head struct {
		Rehearsal bool `json:"rehearsal"`
	}
	shown := s.shownGraphAnswer(answer)
	if json.Unmarshal(answer, &head) != nil || !head.Rehearsal {
		// Older runtimes and gate refusals may have no rehearsal member. Show their
		// words as an error, never as a result, and never retry without the flag.
		writeJSONCoded(w, http.StatusBadGateway, CodeInternal, "The runtime did not return rehearsal: true. The runtime says: "+string(shown))
		return
	}
	s.writeShownGraphAnswer(w, id, shown)
}

func (s *Server) graphRehearsalFailed(w http.ResponseWriter, err error) {
	writeJSONCoded(w, http.StatusInternalServerError, CodeInternal, "The rehearsal could not be run: "+s.redactionFor().text(err.Error()))
}
