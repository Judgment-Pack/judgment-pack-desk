package desk

import (
	"bufio"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/Judgment-Pack/judgment-pack-desk/internal/storagebudget"
)

func TestStorageCheckedWriterCrossProcessOriginalBudgetAndLifetime(t *testing.T) {
	o, r, config := storageWriterFixture(t, true)
	exe, e := os.Executable()
	storageTestMust(t, e)
	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, exe, "-test.run=^TestStorageCheckedWriterProcessHelper$", "-test.timeout=25s")
	cmd.Env = append(os.Environ(), "JPACK_STORAGE_WRITER_HELPER=1", "JPACK_STORAGE_WRITER_CONFIG="+config)
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
	expect := func(want string) {
		t.Helper()
		if !scan.Scan() || scan.Text() != want {
			t.Fatalf("child wanted %q got %q (%s): %s", want, scan.Text(), scan.Err(), stderr.String())
		}
	}
	send := func(line string) { t.Helper(); _, e := io.WriteString(stdin, line+"\n"); storageTestMust(t, e) }
	expect("ready")
	p := storageWriterPlan(r)
	a, e := o.admitSingleFile(t.Context(), p)
	storageTestMust(t, e)
	defer a.Close()
	send("blocked")
	expect("busy-original-domain-and-use")
	storageTestMust(t, a.Write(t.Context()))
	storageTestMust(t, a.Close())
	c, e := o.ref.cell.q.ConfigurationSnapshot(t.Context())
	storageTestMust(t, e)
	policy := c.Policy
	policy.Installation.Total.Bytes = storagebudget.MetadataBudget + storageHostBudget + uint64(len(p.Bytes)) + 64<<10
	policy.Installation.Category[storagebudget.Control].Bytes = policy.Installation.Total.Bytes
	storageTestMust(t, o.ref.cell.q.UpdatePolicy(t.Context(), c.Revision, policy))
	send("quota")
	expect("shared-quota-refused")
	c, e = o.ref.cell.q.ConfigurationSnapshot(t.Context())
	storageTestMust(t, e)
	storageTestMust(t, o.ref.cell.q.UpdatePolicy(t.Context(), c.Revision, storageTestOptions().Policy))
	send("publish")
	expect("published-on-original-budget")
	storageTestMust(t, stdin.Close())
	storageTestMust(t, cmd.Wait())
	done = true
	s, e := o.ref.cell.q.Snapshot(t.Context())
	storageTestMust(t, e)
	if len(s.Tickets) != 0 || s.Installation[storagebudget.Control].Bytes != storagebudget.MetadataBudget+storageHostBudget+uint64(len(p.Bytes))*2 {
		t.Fatal("separate process budget or terminal bytes diverged")
	}
	status, e := o.ref.cell.q.InspectHostState(t.Context())
	storageTestMust(t, e)
	storageTestMust(t, o.withControl(t.Context(), func(ctx context.Context, table *storageIntentTable) error {
		return o.validateIntentMappings(ctx, table, status)
	}))
}
func TestStorageCheckedWriterProcessHelper(t *testing.T) {
	if os.Getenv("JPACK_STORAGE_WRITER_HELPER") != "1" {
		return
	}
	ref, e := openStorageInstallation(t.Context(), os.Getenv("JPACK_STORAGE_WRITER_CONFIG"), "synthetic-original-home")
	storageTestMust(t, e)
	defer ref.Close()
	o, e := newStorageBudgetOwner(ref, storageSyntheticFilesystem{}, storageBudgetOptions{HeadroomBytes: 16 << 20, HeadroomInodes: 100})
	storageTestMust(t, e)
	defer o.Close()
	r, e := o.configRoot(t.Context(), ".data.lock")
	storageTestMust(t, e)
	_, e = o.anchorRoot(t.Context())
	storageTestMust(t, e)
	scan := bufio.NewScanner(os.Stdin)
	barrier := func(want string) {
		t.Helper()
		if !scan.Scan() || scan.Text() != want {
			t.Fatal("missing original process barrier")
		}
	}
	fmt.Println("ready")
	p := storageWriterPlan(r)
	p.Target = "child.json"
	p.Stage = ".child-stage"
	p.Operation = "child-operation"
	barrier("blocked")
	if a, e := o.admitSingleFile(t.Context(), p); !errors.Is(e, storagebudget.ErrBusy) {
		if a != nil {
			a.Close()
		}
		t.Fatalf("original held domain bypassed: %v", e)
	}
	if use, e := o.maintenanceUse(); e == nil {
		use.Close()
		t.Fatal("original storage-use SH bypassed")
	}
	fmt.Println("busy-original-domain-and-use")
	barrier("quota")
	if a, e := o.admitSingleFile(t.Context(), p); !errors.Is(e, storagebudget.ErrLimit) {
		if a != nil {
			a.Close()
		}
		t.Fatalf("separate process exceeded original shared quota: %v", e)
	}
	fmt.Println("shared-quota-refused")
	barrier("publish")
	a, e := o.admitSingleFile(t.Context(), p)
	storageTestMust(t, e)
	storageTestMust(t, a.Write(t.Context()))
	storageTestMust(t, a.Close())
	fmt.Println("published-on-original-budget")
}

// Process death is a real descriptor-lifetime cut, not machine power loss.
// Completed write stages use the production one-call write and checked-sync leaves;
// no qualification member or production enrollment state is constructed.
func TestStorageEnrollmentNativeProcessDeathPreservesOriginal(t *testing.T) {
	// Handwritten status facts avoid assuming OS-specific raw wait encodings.
	for _, row := range []struct {
		signaled bool
		signal   syscall.Signal
		want     bool
	}{
		{true, syscall.SIGKILL, true},
		{false, -1, false}, // ordinary nonzero exit, including test-timeout exit
		{true, syscall.SIGTERM, false},
		{false, syscall.SIGKILL, false}, // a stopped status is not termination
	} {
		if got := storageNativeProcessSIGKILL(row.signaled, row.signal); got != row.want {
			t.Fatalf("native death status predicate: signaled=%v signal=%v got=%v", row.signaled, row.signal, got)
		}
	}
	if !(runtime.GOOS == "linux" && runtime.GOARCH == "amd64" || runtime.GOOS == "darwin" && (runtime.GOARCH == "arm64" || runtime.GOARCH == "amd64")) {
		t.Skip("outside the published native matrix")
	}
	for _, stage := range []string{"before-write", "file-synced", "parent-synced"} {
		t.Run(stage, func(t *testing.T) {
			path := t.TempDir()
			exe, err := os.Executable()
			storageTestMust(t, err)
			ctx, cancel := context.WithTimeout(t.Context(), 20*time.Second)
			defer cancel()
			cmd := exec.CommandContext(ctx, exe, "-test.run=^TestStorageEnrollmentNativeProcessHelper$", "-test.timeout=15s")
			cmd.Env = append(os.Environ(), "JPACK_STORAGE_NATIVE_HELPER=1", "JPACK_STORAGE_NATIVE_ROOT="+path, "JPACK_STORAGE_NATIVE_CUT="+stage)
			out, err := cmd.StdoutPipe()
			storageTestMust(t, err)
			in, err := cmd.StdinPipe()
			storageTestMust(t, err)
			defer in.Close()
			cmd.Stderr = os.Stderr
			storageTestMust(t, cmd.Start())
			done := false
			defer func() {
				if !done {
					_ = cmd.Process.Kill()
					_ = cmd.Wait()
				}
			}()
			line, err := bufio.NewReader(out).ReadString('\n')
			storageTestMust(t, err)
			if line != stage+"\n" {
				t.Fatalf("missing native cut: %q", line)
			}
			root, err := os.OpenRoot(path)
			storageTestMust(t, err)
			defer root.Close()
			size := int64(4096)
			if stage == "before-write" {
				size = 0
			}
			before, err := storageOpenFile(root, "dispatch", size)
			storageTestMust(t, err)
			defer before.file.Close()
			identity, err := storageOwnerPersistentFile(t.Context(), before)
			storageTestMust(t, err)
			storageTestMust(t, cmd.Process.Kill())
			waitErr := cmd.Wait()
			done = true
			if waitErr == nil || ctx.Err() != nil {
				t.Fatalf("wanted explicit process-death cut, wait=%v context=%v", waitErr, ctx.Err())
			}
			if cmd.ProcessState == nil {
				t.Fatal("native death child has no joined process state")
			}
			status, ok := cmd.ProcessState.Sys().(syscall.WaitStatus)
			if !ok || !storageNativeProcessSIGKILL(status.Signaled(), status.Signal()) {
				t.Fatalf("native death child did not terminate by SIGKILL: %v", cmd.ProcessState)
			}
			after, err := storageOpenFile(root, "dispatch", size)
			storageTestMust(t, err)
			defer after.file.Close()
			gotIdentity, err := storageOwnerPersistentFile(t.Context(), after)
			storageTestMust(t, err)
			if gotIdentity != identity || !os.SameFile(before.identity, after.identity) {
				t.Fatal("original volume/object/incarnation changed across process death and reopen")
			}
			got, err := io.ReadAll(io.LimitReader(after.file, 4097))
			storageTestMust(t, err)
			want := bytes.Repeat([]byte{0x68, 0x05, 0x19, 0x31}, 1024)
			if size == 0 {
				want = nil
			}
			if !bytes.Equal(got, want) {
				t.Fatal("process death changed the exact original dispatch bytes")
			}
			storageTestMust(t, before.current())
			t.Log("actual child death, original native identity and readback; no power-loss/Fence/qualification claim")
		})
	}
}

func TestStorageEnrollmentNativeProcessHelper(t *testing.T) {
	if os.Getenv("JPACK_STORAGE_NATIVE_HELPER") != "1" {
		return
	}
	root, err := os.OpenRoot(os.Getenv("JPACK_STORAGE_NATIVE_ROOT"))
	storageTestMust(t, err)
	defer root.Close()
	stage := os.Getenv("JPACK_STORAGE_NATIVE_CUT")
	if strings.HasPrefix(stage, "fence-") {
		enrollmentNativeFenceProcessCut(t, root, stage)
		return
	}
	f, err := root.OpenFile("dispatch", os.O_CREATE|os.O_EXCL|os.O_RDWR, 0600)
	storageTestMust(t, err)
	defer f.Close()
	if stage != "before-write" {
		storageTestMust(t, storageEnrollmentNativeWrite(t.Context(), f, bytes.Repeat([]byte{0x68, 0x05, 0x19, 0x31}, 1024), 0))
		storageTestMust(t, storageEnrollmentNativeSync(t.Context(), f))
	}
	if stage == "parent-synced" {
		storageTestMust(t, f.Close())
		parent, err := root.Open(".")
		storageTestMust(t, err)
		syncErr := storageEnrollmentNativeSync(t.Context(), parent)
		closeErr := parent.Close()
		storageTestMust(t, errors.Join(syncErr, closeErr))
	} else if stage != "before-write" && stage != "file-synced" {
		t.Fatal("unknown native cut")
	}
	fmt.Println(stage)
	// The parent kills this exact process while the original root/description
	// remains retained. EOF is a failed protocol, never a graceful cut pass.
	_, err = bufio.NewReader(os.Stdin).ReadString('\n')
	t.Fatalf("native cut was not killed: %v", err)
}

// Handwritten codec operands are transported by real native calls. These
// detached bytes are not an original qualification or the live two-Fence flag.
func enrollmentNativeFenceImages(t *testing.T) (epComplete, []byte, []byte) {
	t.Helper()
	f := epEvidence(t, 1)
	old := ownerAdvisoryWire(f.before)
	snapshot := storageEnrollmentPeakSnapshot{Kind: 0, Bank: 255, Full: sha256.Sum256(old), Inner: epHash("desk-enrollment-original-v3-inner-peak-v1", old[64:64+int(binary.LittleEndian.Uint32(old[24:]))]), Before: old}
	scope := f.scope
	scope[321] = 0
	clear(scope[1080:1220])
	fence, err := storageEnrollmentPeakEncodeEnvelope(storageEnrollmentPeakEnvelope{Kind: 0, BeforeCodec: 0, Sequence: 2, Scope: scope, Snapshot: &snapshot}, f.record, f.claim)
	storageTestMust(t, err)
	return f, old, fence
}

func enrollmentNativeFenceProcessCut(t *testing.T, root *os.Root, stage string) {
	t.Helper()
	_, old, fence := enrollmentNativeFenceImages(t)
	publish := func(name string, body []byte, create bool) {
		t.Helper()
		flags := os.O_RDWR
		if create {
			flags |= os.O_CREATE | os.O_EXCL
		}
		file, err := root.OpenFile(name, flags, 0600)
		storageTestMust(t, err)
		defer file.Close()
		for offset := 0; offset < len(body); offset += 65536 {
			end := offset + 65536
			if end > len(body) {
				end = len(body)
			}
			storageTestMust(t, storageEnrollmentNativeWrite(t.Context(), file, body[offset:end], int64(offset)))
		}
		storageTestMust(t, storageEnrollmentNativeSync(t.Context(), file))
		storageTestMust(t, file.Close())
		parent, err := root.Open(".")
		storageTestMust(t, err)
		syncErr := storageEnrollmentNativeSync(t.Context(), parent)
		closeErr := parent.Close()
		storageTestMust(t, errors.Join(syncErr, closeErr))
	}
	publish("storage-owner.a", old, true)
	publish("storage-owner.b", old, true)
	fmt.Println("fence-ready")
	reader := bufio.NewReader(os.Stdin)
	line, err := reader.ReadString('\n')
	storageTestMust(t, err)
	if line != "go\n" {
		t.Fatal("missing Fence cut command")
	}
	switch stage {
	case "fence-pre":
	case "fence-one":
		publish("storage-owner.a", fence, false)
	case "fence-post":
		publish("storage-owner.a", fence, false)
		publish("storage-owner.b", fence, false)
	default:
		t.Fatal("unknown Fence cut")
	}
	fmt.Println(stage)
	_, err = reader.ReadString('\n')
	t.Fatalf("Fence cut was not killed: %v", err)
}

func TestStorageEnrollmentNativeFenceProcessDeathImages(t *testing.T) {
	if !(runtime.GOOS == "linux" && runtime.GOARCH == "amd64" || runtime.GOOS == "darwin" && (runtime.GOARCH == "arm64" || runtime.GOARCH == "amd64")) {
		t.Skip("outside the published native matrix")
	}
	for _, stage := range []string{"fence-pre", "fence-one", "fence-post"} {
		t.Run(stage, func(t *testing.T) {
			f, old, fence := enrollmentNativeFenceImages(t)
			path := t.TempDir()
			exe, err := os.Executable()
			storageTestMust(t, err)
			ctx, cancel := context.WithTimeout(t.Context(), 20*time.Second)
			defer cancel()
			cmd := exec.CommandContext(ctx, exe, "-test.run=^TestStorageEnrollmentNativeProcessHelper$", "-test.timeout=15s")
			cmd.Env = append(os.Environ(), "JPACK_STORAGE_NATIVE_HELPER=1", "JPACK_STORAGE_NATIVE_ROOT="+path, "JPACK_STORAGE_NATIVE_CUT="+stage)
			out, err := cmd.StdoutPipe()
			storageTestMust(t, err)
			in, err := cmd.StdinPipe()
			storageTestMust(t, err)
			defer in.Close()
			cmd.Stderr = os.Stderr
			storageTestMust(t, cmd.Start())
			done := false
			defer func() {
				if !done {
					_ = cmd.Process.Kill()
					_ = cmd.Wait()
				}
			}()
			reader := bufio.NewReader(out)
			line, err := reader.ReadString('\n')
			storageTestMust(t, err)
			if line != "fence-ready\n" {
				t.Fatalf("missing original pair: %q", line)
			}
			root, err := os.OpenRoot(path)
			storageTestMust(t, err)
			defer root.Close()
			var originals [2]storageNamedFile
			var identities [2]storageOwnerPersistentObject
			for i, name := range []string{"storage-owner.a", "storage-owner.b"} {
				originals[i], err = storageOpenFile(root, name, int64(len(old)))
				storageTestMust(t, err)
				defer originals[i].file.Close()
				identities[i], err = storageOwnerPersistentFile(t.Context(), originals[i])
				storageTestMust(t, err)
			}
			_, err = io.WriteString(in, "go\n")
			storageTestMust(t, err)
			line, err = reader.ReadString('\n')
			storageTestMust(t, err)
			if line != stage+"\n" {
				t.Fatalf("missing Fence cut: %q", line)
			}
			storageTestMust(t, cmd.Process.Kill())
			waitErr := cmd.Wait()
			done = true
			if waitErr == nil || ctx.Err() != nil {
				t.Fatalf("wanted PID death: wait=%v context=%v", waitErr, ctx.Err())
			}
			if cmd.ProcessState == nil {
				t.Fatal("Fence death child has no joined process state")
			}
			status, ok := cmd.ProcessState.Sys().(syscall.WaitStatus)
			if !ok || !storageNativeProcessSIGKILL(status.Signaled(), status.Signal()) {
				t.Fatalf("Fence death child did not terminate by SIGKILL: %v", cmd.ProcessState)
			}
			for i, name := range []string{"storage-owner.a", "storage-owner.b"} {
				current, err := storageOpenFile(root, name, int64(len(old)))
				storageTestMust(t, err)
				defer current.file.Close()
				identity, err := storageOwnerPersistentFile(t.Context(), current)
				storageTestMust(t, err)
				if identity != identities[i] || !os.SameFile(originals[i].identity, current.identity) {
					t.Fatal("Fence publication replaced an original")
				}
				got, err := io.ReadAll(io.LimitReader(current.file, int64(len(old))+1))
				storageTestMust(t, err)
				isFence := stage == "fence-post" || stage == "fence-one" && i == 0
				want := old
				if isFence {
					want = fence
				}
				if !bytes.Equal(got, want) {
					t.Fatal("wrong surviving full original/Fence image")
				}
				_, oldErr := storageOwnerDecodeCatalogV3(got, f.record, f.claim)
				if isFence {
					if oldErr == nil {
						t.Fatal("old reader accepted Fence bytes")
					}
					envelope, err := storageEnrollmentPeakDecodeEnvelope(got, f.record, f.claim, true)
					storageTestMust(t, err)
					if envelope.Kind != 0 || envelope.Sequence != 2 {
						t.Fatal("Fence envelope lost")
					}
				} else {
					storageTestMust(t, oldErr)
				}
				storageTestMust(t, originals[i].current())
			}
			names, err := root.Open(".")
			storageTestMust(t, err)
			entries, readErr := names.ReadDir(-1)
			closeErr := names.Close()
			storageTestMust(t, errors.Join(readErr, closeErr))
			if len(entries) != 2 {
				t.Fatal("Fence primitive cut created other namespace/Q state")
			}
			t.Log("actual native pre-/one-/post-Fence image transport and PID death; detached fixture, not live qualified entry or power loss")
		})
	}
}

// Read only joined wait facts: successful Kill delivery alone is insufficient.
func storageNativeProcessSIGKILL(signaled bool, signal syscall.Signal) bool {
	return signaled && signal == syscall.SIGKILL
}
