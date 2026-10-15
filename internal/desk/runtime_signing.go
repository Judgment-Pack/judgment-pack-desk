package desk

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
)

// The expectation is taken from Desk's independent list, never by deriving a
// new identity from the seed at its path. Environment binding supports v6
// projects without rewriting their configuration. Explicit startup-project
// choices remain the owner's. An unreadable list for a managed key produces
// a deliberately invalid nonempty expectation, so Runtime refuses signing.
func (s *Server) runtimeSigningExpectation() string {
	if s.assistant == nil {
		return ""
	}
	if s.usesStartupRuntimeEnvironment() && (strings.TrimSpace(os.Getenv(runtimeConfigEnv)) != "" || s.inheritsSigningKey() || os.Getenv(runtimeSigningPublicKeyEnv) != "") {
		return ""
	}
	raw, err := s.readReviewFileWithin(runtimeConfigName, reviewTextLimit)
	if err != nil {
		return ""
	} // Runtime also refuses an unreadable configuration.
	var cfg struct {
		Audit *struct {
			SigningKey string          `json:"signingKey"`
			Public     json.RawMessage `json:"signingPublicKey"`
		} `json:"audit"`
	}
	if json.Unmarshal(raw, &cfg) != nil || cfg.Audit == nil {
		return ""
	}
	if cfg.Audit.Public != nil {
		return ""
	}
	id := s.signingKeyName()
	if id == "" || cfg.Audit.SigningKey != filepath.Join(s.assistant.dir, "secrets", "signing", id+seedSuffix) {
		return ""
	}
	const unavailable = "desk-retained-public-key-unavailable"
	dir, err := s.assistant.openSigning(false)
	if err != nil {
		return unavailable
	}
	defer dir.Close()
	if dir.namesHeld() != nil {
		return unavailable
	}
	keys, found, err := dir.readKeys(id + keysSuffix)
	if err != nil || !found || len(keys) == 0 {
		return unavailable
	}
	return keys[len(keys)-1].PublicKey
}

// A reviewed rotation changes the expectation only when its retained list is
// updated by the existing transaction. New relay processes must then receive
// that value; one-shot commands read it again for every invocation. An older
// process can only refuse signing under its old key, never bless the new seed.
func (s *Server) reconnectAfterKeyRotation() {
	s.mu.Lock()
	defer s.mu.Unlock()
	for c := range s.conns {
		c.stop()
		if c.cancel != nil {
			c.cancel()
		}
	}
}
