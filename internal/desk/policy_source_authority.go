package desk

// Recurring reads are an explicit grant by this installation's administrator,
// independent of an ordinary browser logout. No session bearer is persisted.
import (
	"encoding/json"
	"errors"
	"os"
)

const policySourceAuthorityFile = "policy-source-authority.json"

type policySourceAuthority struct {
	Version int    `json:"version"`
	Epoch   string `json:"epoch"`
}

func (a *assistantStore) readPolicySourceAuthority() (string, error) {
	if !a.usable() {
		return "", errors.New("source authority unavailable")
	}
	raw, err := readPrivateData(a.secrets, policySourceAuthorityFile, 1024)
	var record policySourceAuthority
	if err != nil || rejectDuplicateMembers(raw) != nil || decodeDataJSON(raw, &record) != nil || record.Version != 1 || !retainedArtifactDigest.MatchString(record.Epoch) {
		return "", errors.New("source authority unavailable")
	}
	return record.Epoch, nil
}

func (a *assistantStore) rotatePolicySourceAuthority() error {
	if !a.usable() {
		return errors.New("source authority unavailable")
	}
	// Rotation precedes any authentication-policy write, even one that fails.
	// A reset followed by identical settings cannot revive an old grant.
	token, err := randomStagingName("")
	if err != nil {
		return err
	}
	raw, _ := json.Marshal(policySourceAuthority{Version: 1, Epoch: digestOf([]byte(token))})
	return writePrivateData(a.secrets, policySourceAuthorityFile, raw)
}

func (s *Server) initPolicySourceAuthority() {
	if !s.assistant.usable() {
		return
	}
	if _, err := s.assistant.secrets.Lstat(policySourceAuthorityFile); errors.Is(err, os.ErrNotExist) {
		// Legacy installations have no grants. A new random identity also makes
		// a restored project from another installation fail closed.
		_ = s.assistant.rotatePolicySourceAuthority()
	}
}

func (s *Server) policySourceAuthorityBasis() (string, error) {
	epoch, err := s.assistant.readPolicySourceAuthority()
	if err != nil || s.signIn == nil {
		return "", errors.New("source authority unavailable")
	}
	s.signIn.mu.Lock()
	defer s.signIn.mu.Unlock()
	if s.signIn.problem {
		return "", errors.New("source authority unavailable")
	}
	raw, _ := json.Marshal(s.signIn.active)
	return digestOf(append([]byte(epoch+"\x00"), raw...)), nil
}
