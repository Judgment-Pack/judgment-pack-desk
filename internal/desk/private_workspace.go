package desk

import (
	"errors"
	"net/http"
)

// Briefs and saved test suites retain private generated prose and exact source
// snapshots after their originating drafts or chats are deleted. Their access
// boundary belongs to the whole store, rather than caller-supplied source tags.
func (s *Server) authorizePrivateWorkspace(r *http.Request) error {
	if _, ok := s.sessionOf(r); !ok || r.Context().Err() != nil {
		return withCode(CodeUnauthorized, errors.New("Sign in before accessing private workspace data."))
	}
	return nil
}

func (s *Server) guardPrivateWorkspace(w http.ResponseWriter, r *http.Request) bool {
	if err := s.authorizePrivateWorkspace(r); err != nil {
		writeJSONCoded(w, statusForRefusal(err), codeOf(err), err.Error())
		return false
	}
	return true
}

func privateWorkspaceStorageFile(name string) bool {
	return assistantRunFileName.MatchString(name) || graphDraftFileName.MatchString(name) || briefFileName.MatchString(name) || packTestsFileName.MatchString(name)
}
