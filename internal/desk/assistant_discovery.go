package desk

import (
	"io"
	"net/http"
)

// handleAssistantModels is a closed metadata action. The saved connection owns
// the family and destination; callers cannot supply a path, query or model call.
// Reuse the relay's existing listing-only transport and custody checks so its
// bounded JSON/key scan, cancellation, queue and deadlines remain one boundary.
func (s *Server) handleAssistantModels(w http.ResponseWriter, r *http.Request) {
	if !s.guard(w, r) {
		return
	}
	body, err := io.ReadAll(io.LimitReader(r.Body, 1))
	if err != nil || len(body) != 0 || (r.URL.RawQuery != "" || r.URL.ForceQuery) {
		writeJSONCoded(w, http.StatusBadRequest, CodeAssistantRelayPath, "Model discovery accepts no body or query.")
		return
	}
	endpoint, _, connection, err := s.endpointForConnection(r, false)
	if err != nil {
		writeJSONError(w, statusForRefusal(err), err)
		return
	}
	if connection != nil && r.Header.Get(aiRevisionHeader) == "" {
		writeJSONCoded(w, http.StatusConflict, CodeAssistantUnconfigured, "Refresh the saved AI connection before loading models.")
		return
	}
	suffix, ok := relayListingSuffix[endpoint.kind]
	if !ok {
		writeJSONCoded(w, http.StatusConflict, CodeAssistantUnconfigured, "Select an API connection.")
		return
	}
	fixed := r.Clone(r.Context())
	fixed.Header = make(http.Header)
	for _, name := range []string{"Authorization", aiConnectionHeader, aiRevisionHeader} {
		fixed.Header[name] = append([]string(nil), r.Header.Values(name)...)
	}
	fixed.Header.Set("Accept", "application/json")
	if endpoint.kind == "anthropic" {
		fixed.Header.Set("Anthropic-Version", "2023-06-01")
	}
	fixed.Method = http.MethodGet
	fixed.URL.Path = relayPrefix + suffix
	fixed.URL.RawPath = ""
	fixed.URL.RawQuery = ""
	fixed.Body = http.NoBody
	fixed.ContentLength = 0
	s.handleModelRelay(w, fixed)
}
