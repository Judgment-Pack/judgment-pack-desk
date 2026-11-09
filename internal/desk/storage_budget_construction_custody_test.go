package desk

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/Judgment-Pack/judgment-pack-desk/internal/storagebudget"
)

func TestStorageConstructionOriginalInfrastructureCustody(t *testing.T) {
	cfg := ownerActualConfig(t)
	// A safe ancestor need not be private. Pin this supported mode explicitly
	// instead of relying on testing.TempDir's process-umask-dependent mode.
	if err := os.Chmod(filepath.Dir(cfg.DeskConfigDir), 0755); err != nil {
		t.Fatal(err)
	}
	s, c, _ := ownerCrashConstruction(t, cfg)
	originalHome := s.homeLease
	if c.home != s.storageInfrastructure.home || c.home.lease != originalHome || !c.home.exclusive {
		t.Fatal("construction replaced original home EX custody")
	}
	if old, err := storagePinOriginal(cfg.DeskConfigDir); old != nil || !errors.Is(err, errStorageCustody) {
		if old != nil {
			_ = old.close()
		}
		t.Fatal("standalone private-parent prerequisite changed", err)
	}
	p := s.storageInfrastructure
	copy := *p
	if o, err := copy.pinConstructionOriginal(t.Context()); o != nil || !errors.Is(err, storagebudget.ErrAuthority) {
		if o != nil {
			_ = o.close()
		}
		t.Fatal("copied infrastructure minted original custody", err)
	}
	duplicate, err := p.pinConstructionOriginal(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	a, err := duplicate.config.Stat(".")
	if err != nil {
		_ = duplicate.close()
		t.Fatal(err)
	}
	b, err := s.assistant.root.Stat(".")
	if err != nil || !os.SameFile(a, b) {
		_ = duplicate.close()
		t.Fatal("construction selected another config object", err)
	}
	if err = duplicate.close(); err != nil {
		t.Fatal(err)
	}
	if err = c.current(t.Context(), true); err != nil {
		t.Fatal("independent close revoked launch/construction witnesses", err)
	}
	if s.homeLease != originalHome || c.home.lease != originalHome {
		t.Fatal("independent close replaced original home descriptor")
	}
	if contender, err := openHomeLeaseExistingRoot(c.original.config, false); err == nil {
		_ = contender.Close()
		t.Fatal("independent close released original EX")
	}
	if err = c.Close(); err != nil {
		t.Fatal(err)
	}
	if err = p.home.requireFixedZero(t.Context()); err != nil {
		t.Fatal("construction close revoked infrastructure custody", err)
	}
	if contender, err := openHomeLeaseExistingRoot(p.config.root, false); err == nil {
		_ = contender.Close()
		t.Fatal("construction close released original EX")
	}
}

func TestStorageConstructionOriginalInfrastructureCurrentness(t *testing.T) {
	for _, kind := range []string{"ancestor-name", "ancestor-writable", "config-mode", "home-name"} {
		t.Run(kind, func(t *testing.T) {
			cfg := ownerActualConfig(t)
			parent := filepath.Dir(cfg.DeskConfigDir)
			if err := os.Chmod(parent, 0755); err != nil {
				t.Fatal(err)
			}
			_, c, _ := ownerCrashConstruction(t, cfg)
			switch kind {
			case "ancestor-name":
				if err := os.Rename(parent, parent+"-original"); err != nil {
					t.Fatal(err)
				}
				if err := os.Mkdir(parent, 0755); err != nil {
					t.Fatal(err)
				}
			case "ancestor-writable":
				if err := os.Chmod(parent, 0777); err != nil {
					t.Fatal(err)
				}
			case "config-mode":
				if err := os.Chmod(cfg.DeskConfigDir, 0755); err != nil {
					t.Fatal(err)
				}
			case "home-name":
				path := filepath.Join(cfg.DeskConfigDir, ".home-use.lock")
				if err := os.Rename(path, path+"-original"); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(path, nil, 0600); err != nil {
					t.Fatal(err)
				}
			}
			if err := c.current(t.Context(), true); err == nil {
				t.Fatal("changed original custody remained current", err)
			}
		})
	}
}
