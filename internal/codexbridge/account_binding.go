package codexbridge

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"reflect"
)

// AccountBinding is an opaque custody snapshot. The managed profile is private
// and leased exclusively; only its native peer owns credential writes. Still,
// external replacement/mutation must invalidate a captured diagnostic. Bind
// file identity and modification metadata, without reading or hashing secrets.
// Token renewal conservatively invalidates a diagnostic too. No native mutex is
// acquired: physical admission callbacks already hold that mutex.
func (m *Manager) AccountBinding() (string, error) {
	if m.life.Err() != nil {
		return "", ErrUnavailable
	}
	if err := m.profile.check(); err != nil {
		return "", err
	}
	state := map[string]any{"epoch": m.accountRevision(), "present": false}
	info, err := m.profile.root.Lstat("profile/auth.json")
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return "", ErrProfile
	}
	if err == nil {
		if !privateInfo(info, false) {
			return "", ErrProfile
		}
		state["present"], state["size"], state["modified"] = true, info.Size(), info.ModTime().UnixNano()
		// Platform stat identity and change-time fields only. Access time changes
		// when the native peer reads credentials and is deliberately excluded.
		value := reflect.Indirect(reflect.ValueOf(info.Sys()))
		if value.IsValid() && value.Kind() == reflect.Struct {
			for _, name := range []string{"Dev", "Ino", "Ctim", "Ctimespec", "FileIndexHigh", "FileIndexLow"} {
				if field := value.FieldByName(name); field.IsValid() && field.CanInterface() {
					state[name] = field.Interface()
				}
			}
		}
	}
	raw, err := json.Marshal(state)
	if err != nil {
		return "", ErrProfile
	}
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:]), nil
}
