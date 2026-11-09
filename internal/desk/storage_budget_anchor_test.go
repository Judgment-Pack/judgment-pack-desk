package desk

import (
	"bufio"
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/Judgment-Pack/judgment-pack-desk/internal/storagebudget"
)

func storageTestMust(t *testing.T, e error) {
	t.Helper()
	if e != nil {
		t.Fatal(e)
	}
}
func storageTestOptions() storageBootstrapOptions {
	v := storagebudget.Vector{Bytes: 1 << 30, Files: 4096, Directories: 4096}
	p := storagebudget.Policy{ObservationMaxAge: time.Minute}
	p.Installation.Total = v
	p.Workspace.Total = v
	for i := range p.Installation.Category {
		p.Installation.Category[i] = v
		p.Workspace.Category[i] = v
	}
	return storageBootstrapOptions{Policy: p, HeadroomBytes: 16 << 20, HeadroomInodes: 100}
}
func storageTestConfig(t *testing.T) string {
	t.Helper()
	parent := filepath.Join(t.TempDir(), "machine")
	storageTestMust(t, os.Mkdir(parent, 0700))
	config := filepath.Join(parent, "config")
	storageTestMust(t, os.Mkdir(config, 0700))
	storageTestMust(t, os.WriteFile(filepath.Join(config, ".home-use.lock"), nil, 0600))
	return config
}
func storageTestInstallation(t *testing.T) (*storageInstallationRef, string) {
	t.Helper()
	if runtime.GOOS != "linux" {
		t.Skip("first bootstrap requires Linux descriptor byte/inode observation; no macOS qualification")
	}
	config := storageTestConfig(t)
	ref, e := bootstrapStorageInstallation(t.Context(), config, "synthetic-original-home", storageTestOptions(), nil)
	storageTestMust(t, e)
	t.Cleanup(func() { storageTestMust(t, ref.Close()) })
	return ref, config
}

// Only synthetic fixture contents are read; never call this on selected real data.
func storageTestTree(t *testing.T, root string) map[string]string {
	t.Helper()
	out := map[string]string{}
	storageTestMust(t, filepath.WalkDir(root, func(path string, d os.DirEntry, e error) error {
		if e != nil {
			return e
		}
		name, e := filepath.Rel(root, path)
		if e != nil {
			return e
		}
		info, e := d.Info()
		if e != nil {
			return e
		}
		value := fmt.Sprintf("%v:%d", info.Mode(), info.Size())
		if info.Mode().IsRegular() {
			f, e := os.Open(path)
			if e != nil {
				return e
			}
			h := sha256.New()
			_, e = io.Copy(h, f)
			ce := f.Close()
			if e != nil {
				return e
			}
			if ce != nil {
				return ce
			}
			value += fmt.Sprintf(":%x", h.Sum(nil))
		}
		if info.Mode()&os.ModeSymlink != 0 {
			target, e := os.Readlink(path)
			if e != nil {
				return e
			}
			value += ":" + target
		}
		out[name] = value
		return nil
	}))
	return out
}
func storageTestWitness(ref *storageInstallationRef, name string) *os.File {
	for _, n := range ref.cell.original.files {
		if n.name == name {
			return n.file
		}
	}
	return nil
}

func TestStorageInstallationOriginalParentSharing(t *testing.T) {
	parent, config := storageTestInstallation(t)
	child, e := parent.borrow()
	storageTestMust(t, e)
	defer child.Close()
	sibling, e := parent.borrow()
	storageTestMust(t, e)
	defer sibling.Close()
	storageTestMust(t, sibling.Close())
	stillParent, e := parent.acquire()
	storageTestMust(t, e)
	storageTestMust(t, stillParent.Close())
	if child.cell != parent.cell || sibling.cell != parent.cell || child.owner || sibling.owner {
		t.Fatal("children did not borrow original owner")
	}
	other, e := openStorageInstallation(t.Context(), config, "synthetic-original-home")
	storageTestMust(t, e)
	defer other.Close()
	if other.cell == parent.cell || other.cell.record.Installation != parent.cell.record.Installation || other.cell.binding != parent.cell.binding {
		t.Fatal("separate opener lost original identity")
	}
	a, b := storageTestWitness(parent, "lock"), storageTestWitness(other, "lock")
	ai, e := a.Stat()
	storageTestMust(t, e)
	bi, e := b.Stat()
	storageTestMust(t, e)
	if a.Fd() == b.Fd() || !os.SameFile(ai, bi) {
		t.Fatal("lock descriptions are not independent descriptions of one inode")
	}
	storageTestMust(t, lockPrivateData(a, true))
	if e = lockPrivateData(b, true); e == nil || !lockHeld(e) {
		t.Fatalf("independent description bypassed flock: %v", e)
	}
	storageTestMust(t, storageUnlock(a))
	storageTestMust(t, lockPrivateData(b, true))
	storageTestMust(t, storageUnlock(b))
	copied := *child
	if _, e = copied.borrow(); !errors.Is(e, errStorageCustody) {
		t.Fatalf("copied wrapper borrowed: %v", e)
	}
	if e = copied.Close(); !errors.Is(e, errStorageCustody) {
		t.Fatalf("copied wrapper closed original: %v", e)
	}
	foreign := &storageInstallationRef{cell: parent.cell}
	if _, e = foreign.acquire(); !errors.Is(e, errStorageCustody) {
		t.Fatalf("unsealed construction acquired: %v", e)
	}
	distinct, _ := storageTestInstallation(t)
	if distinct.cell.record.Installation == parent.cell.record.Installation {
		t.Fatal("independent fixture reused storage ID")
	}
	if _, e = os.Stat(filepath.Join(config, ".desk-private", storageAnchorName)); !errors.Is(e, os.ErrNotExist) {
		t.Fatal("child-local ledger exists")
	}
}

func TestStorageInstallationLifetime(t *testing.T) {
	parent, config := storageTestInstallation(t)
	child, e := parent.borrow()
	storageTestMust(t, e)
	active, e := child.acquire()
	storageTestMust(t, e)
	storageTestMust(t, parent.Close())
	if parent.cell.closed || !parent.cell.closing {
		t.Fatal("parent did not retain active custody")
	}
	if _, e = parent.borrow(); !errors.Is(e, os.ErrClosed) {
		t.Fatal("closed parent borrowed")
	}
	if _, e = child.borrow(); !errors.Is(e, os.ErrClosed) {
		t.Fatal("draining installation borrowed")
	}
	storageTestMust(t, child.Close())
	if parent.cell.closed {
		t.Fatal("child released active use")
	}
	storageTestMust(t, active.Current())
	storageTestMust(t, active.Close())
	if !parent.cell.closed {
		t.Fatal("last use did not close original cell")
	}
	// Repeated and copied closes cannot act on a newly reused descriptor number.
	f, e := os.OpenFile(filepath.Join(config, "reuse-probe"), os.O_CREATE|os.O_EXCL|os.O_RDWR, 0600)
	storageTestMust(t, e)
	defer f.Close()
	copyUse := *active
	if e = copyUse.Close(); !errors.Is(e, errStorageCustody) {
		t.Fatal("copied use closed custody")
	}
	storageTestMust(t, parent.Close())
	storageTestMust(t, child.Close())
	storageTestMust(t, active.Close())
	_, e = f.Write([]byte("synthetic"))
	storageTestMust(t, e)
	if e = active.Current(); !errors.Is(e, os.ErrClosed) {
		t.Fatal("released use currentness accepted")
	}
}

func TestStorageInstallationConstructorFailureCustody(t *testing.T) {
	parent, config := storageTestInstallation(t)
	before := storageTestTree(t, config)
	if ref, e := openStorageInstallation(t.Context(), config, "wrong-original-home"); e == nil || ref != nil {
		t.Fatal("foreign owner adopted")
	}
	if !reflect.DeepEqual(before, storageTestTree(t, config)) {
		t.Fatal("failed constructor mutated original")
	}
	use, e := parent.acquire()
	storageTestMust(t, e)
	storageTestMust(t, use.Close())
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if ref, e := openStorageInstallation(ctx, config, "synthetic-original-home"); e == nil || ref != nil {
		t.Fatal("cancelled constructor adopted")
	}
	use, e = parent.acquire()
	storageTestMust(t, e)
	storageTestMust(t, use.Close())
}

func TestStorageAnchorCustodyCurrentness(t *testing.T) {
	cases := []struct {
		name   string
		change func(*testing.T, string)
	}{
		{"config-name", func(t *testing.T, c string) {
			storageTestMust(t, os.Rename(c, c+"-old"))
			storageTestMust(t, os.Mkdir(c, 0700))
		}},
		{"ancestor-name", func(t *testing.T, c string) {
			p := filepath.Dir(c)
			storageTestMust(t, os.Rename(p, p+"-old"))
			storageTestMust(t, os.Mkdir(p, 0700))
		}},
		{"anchor-name", func(t *testing.T, c string) {
			p := filepath.Join(c, storageAnchorName)
			storageTestMust(t, os.Rename(p, p+"-old"))
			storageTestMust(t, os.Mkdir(p, 0700))
		}},
		{"anchor-symlink", func(t *testing.T, c string) {
			p := filepath.Join(c, storageAnchorName)
			storageTestMust(t, os.Rename(p, p+"-old"))
			storageTestMust(t, os.Symlink(storageAnchorName+"-old", p))
		}},
		{"config-symlink", func(t *testing.T, c string) {
			storageTestMust(t, os.Rename(c, c+"-old"))
			storageTestMust(t, os.Symlink(filepath.Base(c)+"-old", c))
		}},
		{"control-symlink", func(t *testing.T, c string) {
			p := filepath.Join(c, storageRecordName)
			storageTestMust(t, os.Rename(p, p+"-old"))
			storageTestMust(t, os.Symlink(storageRecordName+"-old", p))
		}},
		{"fixed-control-hardlink", func(t *testing.T, c string) {
			p := filepath.Join(c, storageAnchorName, "checkpoint.a")
			storageTestMust(t, os.Link(p, p+"-alias"))
		}},
		{"config-mode", func(t *testing.T, c string) { storageTestMust(t, os.Chmod(c, 0755)) }},
		{"parent-mode", func(t *testing.T, c string) { storageTestMust(t, os.Chmod(filepath.Dir(c), 0755)) }},
		{"lock-replacement", func(t *testing.T, c string) {
			p := filepath.Join(c, storageAnchorName, "lock")
			storageTestMust(t, os.Rename(p, p+"-old"))
			storageTestMust(t, os.WriteFile(p, nil, 0600))
		}},
		{"lock-hardlink", func(t *testing.T, c string) {
			p := filepath.Join(c, storageAnchorName, "lock")
			storageTestMust(t, os.Link(p, p+"-alias"))
		}},
		{"control-hardlink", func(t *testing.T, c string) {
			p := filepath.Join(c, storageRecordName)
			storageTestMust(t, os.Link(p, p+"-alias"))
		}},
		{"control-mode", func(t *testing.T, c string) { storageTestMust(t, os.Chmod(filepath.Join(c, storageIntentName), 0640)) }},
		{"control-replacement", func(t *testing.T, c string) {
			p := filepath.Join(c, storageAnchorName, "checkpoint.a")
			storageTestMust(t, os.Rename(p, p+"-old"))
			f, e := os.OpenFile(p, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
			storageTestMust(t, e)
			storageTestMust(t, f.Truncate(storagebudget.CheckpointBytes))
			storageTestMust(t, f.Close())
		}},
		{"bootstrap-lock-replacement", func(t *testing.T, c string) {
			p := filepath.Join(c, storageBootstrapName)
			storageTestMust(t, os.Rename(p, p+"-old"))
			storageTestMust(t, os.WriteFile(p, nil, 0600))
		}},
		{"use-lock-replacement", func(t *testing.T, c string) {
			p := filepath.Join(c, storageUseName)
			storageTestMust(t, os.Rename(p, p+"-old"))
			storageTestMust(t, os.WriteFile(p, nil, 0600))
		}},
		{"home-lease-replacement", func(t *testing.T, c string) {
			p := filepath.Join(c, ".home-use.lock")
			storageTestMust(t, os.Rename(p, p+"-old"))
			storageTestMust(t, os.WriteFile(p, nil, 0600))
		}},
		{"claim-same-inode", func(t *testing.T, c string) {
			f, e := os.OpenFile(filepath.Join(c, storageClaimName), os.O_WRONLY, 0)
			storageTestMust(t, e)
			_, e = f.WriteAt([]byte("!"), 0)
			storageTestMust(t, e)
			storageTestMust(t, f.Close())
		}},
		{"ready-same-inode", func(t *testing.T, c string) {
			f, e := os.OpenFile(filepath.Join(c, storageReadyName), os.O_WRONLY, 0)
			storageTestMust(t, e)
			_, e = f.WriteAt([]byte("wrong readiness binding........."), 0)
			storageTestMust(t, e)
			storageTestMust(t, f.Close())
		}},
		{"record-same-inode", func(t *testing.T, c string) {
			f, e := os.OpenFile(filepath.Join(c, storageRecordName), os.O_WRONLY, 0)
			storageTestMust(t, e)
			_, e = f.WriteAt([]byte("!"), 0)
			storageTestMust(t, e)
			storageTestMust(t, f.Close())
		}},
	}
	for _, row := range cases {
		t.Run(row.name, func(t *testing.T) {
			ref, config := storageTestInstallation(t)
			use, e := ref.acquire()
			storageTestMust(t, e)
			defer use.Close()
			row.change(t, config)
			// Walk the outer synthetic fixture, including any renamed-away original.
			outer := filepath.Dir(filepath.Dir(config))
			before := storageTestTree(t, outer)
			if e = use.Current(); !errors.Is(e, errStorageCustody) {
				t.Fatalf("changed original remained current: %v", e)
			}
			if _, e = ref.acquire(); !errors.Is(e, errStorageCustody) {
				t.Fatalf("growth accepted changed original: %v", e)
			}
			if !reflect.DeepEqual(before, storageTestTree(t, outer)) {
				t.Fatal("currentness refusal changed original or replacement")
			}
		})
	}
}

func TestStorageInstallationWrongOwner(t *testing.T) {
	ref, _ := storageTestInstallation(t)
	original := effectiveUser
	effectiveUser = func() uint32 { return original() + 1 }
	defer func() { effectiveUser = original }()
	if _, e := ref.acquire(); !errors.Is(e, errStorageCustody) {
		t.Fatalf("unknown owner accepted: %v", e)
	}
}

func TestStorageInstallationCrossProcessOriginalLock(t *testing.T) {
	ref, config := storageTestInstallation(t)
	exe, e := os.Executable()
	storageTestMust(t, e)
	cmd := exec.Command(exe, "-test.run=^TestStorageInstallationProcessHelper$")
	cmd.Env = append(os.Environ(), "JPACK_STORAGE_ANCHOR_HELPER=1", "JPACK_STORAGE_ANCHOR_CONFIG="+config)
	stdout, e := cmd.StdoutPipe()
	storageTestMust(t, e)
	stdin, e := cmd.StdinPipe()
	storageTestMust(t, e)
	var stderr strings.Builder
	cmd.Stderr = &stderr
	storageTestMust(t, cmd.Start())
	done := false
	defer func() {
		if !done {
			_ = cmd.Process.Kill()
			_ = cmd.Wait()
		}
	}()
	scan := bufio.NewScanner(stdout)
	if !scan.Scan() || scan.Text() != "ready" {
		t.Fatalf("child did not pin original: %s %s", scan.Text(), stderr.String())
	}
	lock := storageTestWitness(ref, "lock")
	storageTestMust(t, lockPrivateData(lock, true))
	_, e = io.WriteString(stdin, "blocked\n")
	storageTestMust(t, e)
	if !scan.Scan() || scan.Text() != "busy" {
		t.Fatalf("child bypassed original Q lock: %s %s", scan.Text(), stderr.String())
	}
	storageTestMust(t, storageUnlock(lock))
	_, e = io.WriteString(stdin, "released\n")
	storageTestMust(t, e)
	if !scan.Scan() || scan.Text() != "current" {
		t.Fatalf("child lost original after release: %s %s", scan.Text(), stderr.String())
	}
	storageTestMust(t, stdin.Close())
	storageTestMust(t, cmd.Wait())
	done = true
}
func TestStorageInstallationProcessHelper(t *testing.T) {
	if os.Getenv("JPACK_STORAGE_ANCHOR_HELPER") != "1" {
		return // Only its owning fixture enables helper behavior.
	}
	ref, e := openStorageInstallation(t.Context(), os.Getenv("JPACK_STORAGE_ANCHOR_CONFIG"), "synthetic-original-home")
	storageTestMust(t, e)
	defer ref.Close()
	fmt.Println("ready")
	scan := bufio.NewScanner(os.Stdin)
	if !scan.Scan() || scan.Text() != "blocked" {
		t.Fatal("missing barrier")
	}
	ctx, cancel := context.WithTimeout(t.Context(), 150*time.Millisecond)
	_, e = ref.cell.q.ConfigurationSnapshot(ctx)
	cancel()
	if !errors.Is(e, storagebudget.ErrBusy) {
		t.Fatalf("expected real cross-process flock refusal: %v", e)
	}
	fmt.Println("busy")
	if !scan.Scan() || scan.Text() != "released" {
		t.Fatal("missing release")
	}
	_, e = ref.cell.q.ConfigurationSnapshot(t.Context())
	storageTestMust(t, e)
	fmt.Println("current")
}
