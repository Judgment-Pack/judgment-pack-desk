package desk

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
)

// These synthetic executable byte fixtures are supplied by the test. No
// installed Runtime, shell, compiler or other external program is executed.
const assistantRuntimeArtifactFixture = "abc"
const assistantRuntimeArtifactFixtureSHA256 = "ba7816bf8f01cfea414140de5dae2223b00361a396177a9cb410ff61f20015ad"

func runtimeArtifactFixture(t *testing.T) string {
	t.Helper()
	source := filepath.Join(t.TempDir(), "runtime")
	if err := os.WriteFile(source, []byte(assistantRuntimeArtifactFixture), 0700); err != nil {
		t.Fatal(err)
	}
	return source
}

func runtimeArtifactOpenFixture(t *testing.T) *assistantRuntimeArtifact {
	t.Helper()
	a, err := openAssistantRuntimeArtifact(context.Background(), runtimeArtifactFixture(t), assistantRuntimeArtifactFixtureSHA256)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := a.Close(); err != nil {
			t.Error(err)
		}
	})
	return a
}

func TestAssistantRuntimeArtifactVerifiedPrivateCopy(t *testing.T) {
	source := runtimeArtifactFixture(t)
	a, err := openAssistantRuntimeArtifact(context.Background(), source, assistantRuntimeArtifactFixtureSHA256)
	if err != nil {
		t.Fatal(err)
	}
	defer a.Close()
	if a.path == source || filepath.Dir(a.path) != a.dir {
		t.Fatal("artifact did not retain its private launch path")
	}
	if err := a.Current(); err != nil {
		t.Fatal(err)
	}
	for path, mode := range map[string]os.FileMode{a.dir: 0700, a.path: 0500} {
		info, err := os.Stat(path)
		if err != nil || info.Mode().Perm() != mode {
			t.Fatalf("private mode for %s: %v, %v", path, info, err)
		}
	}
	// Replacing, then deleting the installation path cannot retarget custody.
	if err := os.Rename(source, source+".old"); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(source, []byte("untrusted replacement"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(source); err != nil {
		t.Fatal(err)
	}
	if err := a.Current(); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(a.path)
	if err != nil || string(got) != assistantRuntimeArtifactFixture {
		t.Fatalf("private bytes changed: %q, %v", got, err)
	}
}

func TestAssistantRuntimeArtifactRefusesUntrustedDigest(t *testing.T) {
	for _, digest := range []string{"", "abc", strings.Repeat("g", 64), strings.ToUpper(assistantRuntimeArtifactFixtureSHA256), strings.Repeat("0", 64)} {
		t.Run(digest, func(t *testing.T) {
			source := runtimeArtifactFixture(t)
			privateRoot := t.TempDir()
			t.Setenv("TMPDIR", privateRoot)
			a, err := openAssistantRuntimeArtifact(context.Background(), source, digest)
			if err == nil || a != nil {
				t.Fatalf("accepted untrusted digest: %v, %v", a, err)
			}
			entries, err := os.ReadDir(privateRoot)
			if err != nil || len(entries) != 0 {
				t.Fatalf("failed open leaked private resources: %v, %v", entries, err)
			}
		})
	}
}

func TestAssistantRuntimeArtifactRefusesNonregularSource(t *testing.T) {
	dir := t.TempDir()
	fifo := filepath.Join(dir, "fifo")
	if err := syscall.Mkfifo(fifo, 0600); err != nil {
		t.Fatal(err)
	}
	for _, source := range []string{dir, fifo} {
		if a, err := openAssistantRuntimeArtifact(context.Background(), source, assistantRuntimeArtifactFixtureSHA256); err == nil || a != nil {
			t.Fatalf("accepted nonregular source: %v, %v", a, err)
		}
	}
}

func TestAssistantRuntimeArtifactCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if a, err := openAssistantRuntimeArtifact(ctx, runtimeArtifactFixture(t), assistantRuntimeArtifactFixtureSHA256); !errors.Is(err, context.Canceled) || a != nil {
		t.Fatalf("cancelled open: %v, %v", a, err)
	}
	reader := assistantRuntimeArtifactReader{ctx: ctx, reader: strings.NewReader("abc")}
	if n, err := reader.Read(make([]byte, 3)); n != 0 || !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled copy read: %d, %v", n, err)
	}
}

func TestAssistantRuntimeArtifactRefusesPrivateDrift(t *testing.T) {
	t.Run("file replacement", func(t *testing.T) {
		a := runtimeArtifactOpenFixture(t)
		original := a.path + ".original"
		if err := os.Rename(a.path, original); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(a.path, []byte(assistantRuntimeArtifactFixture), 0500); err != nil {
			t.Fatal(err)
		}
		if err := a.Current(); err == nil {
			t.Fatal("accepted same-byte replacement inode")
		}
		if err := os.Remove(a.path); err != nil {
			t.Fatal(err)
		}
		if err := os.Rename(original, a.path); err != nil {
			t.Fatal(err)
		}
	})
	t.Run("directory replacement", func(t *testing.T) {
		a := runtimeArtifactOpenFixture(t)
		original := a.dir + ".original"
		if err := os.Rename(a.dir, original); err != nil {
			t.Fatal(err)
		}
		if err := os.Mkdir(a.dir, 0700); err != nil {
			t.Fatal(err)
		}
		if err := a.Current(); err == nil {
			t.Fatal("accepted replacement directory")
		}
		if err := os.Remove(a.dir); err != nil {
			t.Fatal(err)
		}
		if err := os.Rename(original, a.dir); err != nil {
			t.Fatal(err)
		}
	})
	t.Run("same inode changed bytes", func(t *testing.T) {
		a := runtimeArtifactOpenFixture(t)
		if err := os.Chmod(a.path, 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(a.path, []byte("abd"), 0500); err != nil {
			t.Fatal(err)
		}
		if err := os.Chmod(a.path, 0500); err != nil {
			t.Fatal(err)
		}
		if err := a.Current(); err == nil {
			t.Fatal("accepted same-size mutation of held bytes")
		}
	})
	t.Run("writable file", func(t *testing.T) {
		a := runtimeArtifactOpenFixture(t)
		if err := os.Chmod(a.path, 0700); err != nil {
			t.Fatal(err)
		}
		if err := a.Current(); err == nil {
			t.Fatal("accepted writable private artifact")
		}
	})
	t.Run("missing path", func(t *testing.T) {
		a := runtimeArtifactOpenFixture(t)
		original := a.path + ".original"
		if err := os.Rename(a.path, original); err != nil {
			t.Fatal(err)
		}
		if err := a.Current(); err == nil {
			t.Fatal("accepted missing launch path")
		}
		if err := os.Rename(original, a.path); err != nil {
			t.Fatal(err)
		}
	})
}

func TestAssistantRuntimeArtifactCloseExactlyOnce(t *testing.T) {
	a := runtimeArtifactOpenFixture(t)
	file, directory := a.file, a.directory
	if err := a.Close(); err != nil {
		t.Fatal(err)
	}
	if err := a.Close(); err != nil {
		t.Fatalf("second close repeated resource close: %v", err)
	}
	if err := a.Current(); err == nil {
		t.Fatal("closed artifact remained current")
	}
	for _, path := range []string{a.path, a.dir} {
		if _, err := os.Lstat(path); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("close did not remove exact resource %s: %v", path, err)
		}
	}
	for _, handle := range []*os.File{file, directory} {
		if _, err := handle.Stat(); !errors.Is(err, os.ErrClosed) {
			t.Fatalf("close did not release held handle: %v", err)
		}
	}
}

func TestAssistantRuntimeArtifactClosePreservesUnrelatedEntry(t *testing.T) {
	a, err := openAssistantRuntimeArtifact(context.Background(), runtimeArtifactFixture(t), assistantRuntimeArtifactFixtureSHA256)
	if err != nil {
		t.Fatal(err)
	}
	extra := filepath.Join(a.dir, "unrelated")
	if err := os.WriteFile(extra, []byte("leave intact"), 0600); err != nil {
		t.Fatal(err)
	}
	first := a.Close()
	if first == nil || a.Close() != first {
		t.Fatal("cleanup error was not retained across exactly-once Close")
	}
	if data, err := os.ReadFile(extra); err != nil || string(data) != "leave intact" {
		t.Fatalf("cleanup deleted unrelated entry: %q, %v", data, err)
	}
	// This fixture owns these exact resources, including the injected entry.
	if err := os.Remove(extra); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(a.dir); err != nil {
		t.Fatal(err)
	}
}

func TestAssistantRuntimeArtifactClosePreservesReplacement(t *testing.T) {
	a, err := openAssistantRuntimeArtifact(context.Background(), runtimeArtifactFixture(t), assistantRuntimeArtifactFixtureSHA256)
	if err != nil {
		t.Fatal(err)
	}
	original := a.path + ".original"
	if err := os.Rename(a.path, original); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(a.path, []byte("replacement"), 0500); err != nil {
		t.Fatal(err)
	}
	if err := a.Close(); err == nil {
		t.Fatal("replacement cleanup did not refuse")
	}
	if data, err := os.ReadFile(a.path); err != nil || string(data) != "replacement" {
		t.Fatalf("replacement removed: %q, %v", data, err)
	}
	// The test owns both exact files; Close must leave the replacement alone.
	for _, path := range []string{a.path, original, a.dir} {
		if err := os.Remove(path); err != nil {
			t.Fatal(err)
		}
	}
}
