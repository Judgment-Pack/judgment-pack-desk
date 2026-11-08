package desk

import (
	"errors"
	"os"
	"path/filepath"
)

// storageLineEnv turns the storage line (first-use enrollment, the owner
// record and catalog, census, custody, admission, limits and storage-aware
// transfers and moves) on for a process. Off by default: this release keeps
// v0.13.2's storage behaviour, and the storage line ships in a later one.
// It is trusted host configuration, read once per New, never a project or
// browser setting.
const storageLineEnv = "JPACK_DESK_STORAGE_LINE"

// storageLineEnabled reports whether a Server built from cfg runs the storage
// line. A desk Desk made inherits its parent's choice, so one installation
// never mixes enrolled and legacy servers.
func storageLineEnabled(cfg Config) bool {
	if cfg.parent != nil {
		return cfg.parent.storageLine
	}
	return cfg.StorageLine || os.Getenv(storageLineEnv) == "on"
}

// openLegacyInfrastructure is v0.13.2's startup for the private store: the
// configuration directory validated and pinned, a pending home migration
// refused, the shared home lease taken, and the account/service folders made.
// No census, capacity reservation or exclusive startup lock: two Desks may
// share one installation, as they could before.
func (s *Server) openLegacyInfrastructure() error {
	s.assistant = openAssistantStore(s.configDir)
	if s.cfg.parent == nil && s.assistant.usable() {
		tx, err := readHomeTransaction(homeJournalPath(s.configDir))
		if err != nil || tx != nil && !tx.Complete {
			if err != nil {
				return err
			}
			return errors.New("Restart Desk to recover its pending home migration.")
		}
		lease, err := openHomeLease(s.configDir, false)
		if err != nil {
			return err
		}
		s.homeLease = lease
	}
	if s.home != nil && s.assistant.usable() {
		for _, name := range []string{"accounts", "services"} {
			root, err := openPrivateDataRoot(filepath.Join(s.configDir, name), true)
			if err != nil {
				return err
			}
			root.Close()
		}
	}
	return nil
}
