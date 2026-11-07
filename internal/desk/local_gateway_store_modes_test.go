//go:build unix

package desk

import (
	"os"
	"path/filepath"
	"testing"
)

// **A local gateway store an earlier Desk left at the inherited mask is made
// owner-only before the census.** Before the worker set an owner-only mask,
// the gateway made its store's folders 0755 and files 0644 inside Desk's
// config folder; the census refuses any entry there that is not private, so
// every later start was read-only. Start narrows that store, and only that
// store: an entry group or others may write, a link's target and a file
// outside the store keep their modes, and the census still refuses them.
func TestLocalGatewayStoreFromAnEarlierDeskIsNarrowedBeforeTheCensus(t *testing.T) {
	type entry struct {
		path string
		mode os.FileMode
		dir  bool
	}
	seeded := func(t *testing.T, extra ...entry) string {
		t.Helper()
		config := filepath.Join(t.TempDir(), "config")
		storageTestMust(t, os.Mkdir(config, 0o700))
		// Desk makes the store folder and each run folder owner-only; the
		// gateway made what is inside a run at the inherited mask.
		for _, e := range append([]entry{
			{"local-gateway", 0o700, true},
			{"local-gateway/run-earlier", 0o700, true},
			{"local-gateway/run-earlier/store", 0o755, true},
			{"local-gateway/run-earlier/store/receipts", 0o755, true},
			{"local-gateway/run-earlier/store/receipts/one.json", 0o644, false},
			{"local-gateway/run-earlier/registry.json", 0o644, false},
		}, extra...) {
			path := filepath.Join(config, filepath.FromSlash(e.path))
			if e.dir {
				storageTestMust(t, os.Mkdir(path, 0o700))
			} else {
				storageTestMust(t, os.WriteFile(path, []byte("{}"), 0o600))
			}
			storageTestMust(t, os.Chmod(path, e.mode))
		}
		return config
	}
	modeOf := func(t *testing.T, path string) os.FileMode {
		t.Helper()
		info, err := os.Lstat(path)
		if err != nil {
			t.Fatal(err)
		}
		return info.Mode().Perm()
	}

	t.Run("narrowed", func(t *testing.T) {
		config := seeded(t)
		s, _, _ := assistantServerIn(t, config)
		if s.storageReadOnly || s.storageEnrollment == nil {
			t.Fatalf("a local gateway store at 0755/0644 started read-only: %v", s.storageProblem)
		}
		for path, want := range map[string]os.FileMode{
			"local-gateway/run-earlier/store":                   0o700,
			"local-gateway/run-earlier/store/receipts":          0o700,
			"local-gateway/run-earlier/store/receipts/one.json": 0o600,
			"local-gateway/run-earlier/registry.json":           0o600,
		} {
			if got := modeOf(t, filepath.Join(config, filepath.FromSlash(path))); got != want {
				t.Fatalf("%s is %#o, want %#o", path, got, want)
			}
		}
	})
	t.Run("not ours to repair", func(t *testing.T) {
		target := filepath.Join(t.TempDir(), "elsewhere.json")
		storageTestMust(t, os.WriteFile(target, []byte("{}"), 0o600))
		storageTestMust(t, os.Chmod(target, 0o644))
		config := seeded(t,
			entry{"local-gateway/run-earlier/store/receipts/two.json", 0o664, false},
			entry{"local-gateway/run-earlier/shared", 0o775, true},
			entry{"local-gateway/run-earlier/shared/three.json", 0o644, false},
			entry{"notes.json", 0o644, false},
		)
		storageTestMust(t, os.Symlink(target, filepath.Join(config, "local-gateway", "run-earlier", "store", "link.json")))
		s, _, _ := assistantServerIn(t, config)
		if !s.storageReadOnly {
			t.Fatal("entries the repair leaves alone were measured")
		}
		for path, want := range map[string]os.FileMode{
			filepath.Join(config, "local-gateway/run-earlier/store/receipts/two.json"): 0o664,
			filepath.Join(config, "local-gateway/run-earlier/shared"):                  0o775,
			filepath.Join(config, "local-gateway/run-earlier/shared/three.json"):       0o644,
			filepath.Join(config, "notes.json"):                                        0o644,
			target:                                                                     0o644,
		} {
			if got := modeOf(t, path); got != want {
				t.Fatalf("%s was changed to %#o, want %#o", path, got, want)
			}
		}
	})
}
