package codexbridge

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

// Read-only root fixtures, not native qualification or provider execution.
func TestStorageObservationLazyRetainedProfileNoInitialization(t *testing.T) {
	parent := t.TempDir()
	dir := filepath.Join(parent, "codex")
	if err := os.Mkdir(dir, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "credential-sentinel"), []byte("never read"), 0000); err != nil {
		t.Fatal(err)
	}
	manager := NewLazyManager(Options{ProfileDir: dir, ProjectDir: t.TempDir()})
	called := 0
	if err := manager.ObserveStorage(t.Context(), func(root *os.Root) error {
		called++
		info, err := root.Stat("credential-sentinel")
		if err != nil || info.Size() != 10 {
			t.Fatal("metadata selection", err)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if called != 1 || manager.manager != nil {
		t.Fatal("observation initialized native account")
	}
	names, err := os.ReadDir(dir)
	if err != nil || len(names) != 1 {
		t.Fatal("observation created profile state", err)
	}
	manager.Close()
	if err := manager.ObserveStorage(t.Context(), func(*os.Root) error { t.Fatal("closed callback"); return nil }); !errors.Is(err, ErrUnavailable) {
		t.Fatal("closed manager observed", err)
	}
	absent := NewLazyManager(Options{ProfileDir: filepath.Join(parent, "absent")})
	if err := absent.ObserveStorage(t.Context(), func(*os.Root) error { t.Fatal("absent callback"); return nil }); err == nil {
		t.Fatal("absent profile claimed present")
	}
	if _, err := os.Lstat(filepath.Join(parent, "absent")); !os.IsNotExist(err) {
		t.Fatal("absent profile was created", err)
	}
}
func TestStorageObservationCloseWaitsAndNamesRemainOriginal(t *testing.T) {
	parent := t.TempDir()
	dir := filepath.Join(parent, "codex")
	if err := os.Mkdir(dir, 0700); err != nil {
		t.Fatal(err)
	}
	manager := NewLazyManager(Options{ProfileDir: dir})
	entered, release, observed, closed := make(chan struct{}), make(chan struct{}), make(chan error, 1), make(chan struct{})
	go func() {
		observed <- manager.ObserveStorage(context.Background(), func(*os.Root) error { close(entered); <-release; return nil })
	}()
	<-entered
	go func() { manager.Close(); close(closed) }()
	released := false
	defer func() {
		if !released {
			close(release)
		}
		<-closed
	}()
	select {
	case <-closed:
		t.Fatal("Close released callback lifetime")
	default:
	}
	close(release)
	released = true
	if err := <-observed; err != nil {
		t.Fatal(err)
	}
	<-closed
	manager = NewLazyManager(Options{ProfileDir: dir})
	err := manager.ObserveStorage(t.Context(), func(*os.Root) error {
		if err := os.Rename(dir, dir+"-original"); err != nil {
			t.Fatal(err)
		}
		return os.Mkdir(dir, 0700)
	})
	if !errors.Is(err, ErrProfile) {
		t.Fatal("replacement profile accepted", err)
	}
	// An existing manager uses its already-owned original root, never the name.
	root, err := os.OpenRoot(dir + "-original")
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	active := &Manager{profile: &profile{root: root, dir: dir + "-original"}}
	active.mu.Lock()
	if err := active.ObserveStorage(t.Context(), func(*os.Root) error { t.Fatal("busy callback"); return nil }); !errors.Is(err, ErrBusy) {
		active.mu.Unlock()
		t.Fatal("busy owner waited", err)
	}
	active.mu.Unlock()
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if err := active.ObserveStorage(ctx, func(*os.Root) error { t.Fatal("cancelled callback"); return nil }); !errors.Is(err, ErrUnavailable) {
		t.Fatal("cancelled owner observed", err)
	}
	if err := active.ObserveStorage(t.Context(), func(held *os.Root) error {
		a, _ := held.Stat(".")
		b, _ := root.Stat(".")
		if !os.SameFile(a, b) {
			t.Fatal("different original")
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}

func TestStorageObservationAncestorWitnessesInitializedAndRetained(t *testing.T) {
	for _, initialized := range []bool{false, true} {
		for _, cut := range []string{"before-symlink", "during-symlink", "during-directory-replacement"} {
			name := "retained/" + cut
			if initialized {
				name = "initialized/" + cut
			}
			t.Run(name, func(t *testing.T) {
				base, err := filepath.EvalSymlinks(t.TempDir())
				if err != nil {
					t.Fatal(err)
				}
				ancestor := filepath.Join(base, "ancestor")
				config := filepath.Join(ancestor, "config")
				dir := filepath.Join(config, "codex")
				if err := os.MkdirAll(dir, 0700); err != nil {
					t.Fatal(err)
				}
				original, err := os.OpenRoot(dir)
				if err != nil {
					t.Fatal(err)
				}
				defer original.Close()
				manager := &Manager{profile: &profile{root: original, dir: dir}}
				apply := func() error {
					if err := os.Rename(ancestor, ancestor+"-original"); err != nil {
						return err
					}
					if cut == "during-directory-replacement" {
						if err := os.Mkdir(ancestor, 0700); err != nil {
							return err
						}
						return os.Rename(filepath.Join(ancestor+"-original", "config"), config)
					}
					return os.Symlink(ancestor+"-original", ancestor)
				}
				if cut == "before-symlink" {
					if err := apply(); err != nil {
						t.Fatal(err)
					}
				}
				called := false
				visit := func(root *os.Root) error {
					called = true
					actual, e := root.Stat(".")
					expected, ee := original.Stat(".")
					if e != nil || ee != nil || !os.SameFile(actual, expected) {
						return ErrProfile
					}
					return apply()
				}
				if initialized {
					err = manager.ObserveStorage(t.Context(), visit)
				} else {
					err = ObserveRetainedStorage(t.Context(), dir, visit)
				}
				if !errors.Is(err, ErrProfile) {
					t.Fatal("ancestor cut accepted", err)
				}
				if (cut == "before-symlink") == called {
					t.Fatal("callback timing did not exercise requested cut", called)
				}
				// No profile initialization/lease/native program was used, and the held
				// original remains an actual directory even after the name was refused.
				if info, e := original.Stat("."); e != nil || !info.IsDir() {
					t.Fatal("original description released", e)
				}
			})
		}
	}
}
