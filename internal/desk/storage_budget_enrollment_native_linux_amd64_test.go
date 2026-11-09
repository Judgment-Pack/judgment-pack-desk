//go:build linux && amd64

package desk

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"os/signal"
	"syscall"
	"testing"
	"time"

	"golang.org/x/sys/unix"
)

// RLIMIT_FSIZE is confined to an exact child process, never the parent runner.
// This safely obtains a real kernel short write without filling any filesystem.
func TestStorageEnrollmentNativeLinuxActualShortWrite(t *testing.T) {
	var parentBefore, parentAfter unix.Rlimit
	storageTestMust(t, unix.Getrlimit(unix.RLIMIT_FSIZE, &parentBefore))
	exe, err := os.Executable()
	storageTestMust(t, err)
	ctx, cancel := context.WithTimeout(t.Context(), 20*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, exe, "-test.run=^TestStorageEnrollmentNativeLinuxShortWriteHelper$", "-test.timeout=15s")
	cmd.Env = append(os.Environ(), "JPACK_STORAGE_NATIVE_SHORT_HELPER=1", "JPACK_STORAGE_NATIVE_SHORT_ROOT="+t.TempDir())
	out, err := cmd.CombinedOutput()
	storageTestMust(t, unix.Getrlimit(unix.RLIMIT_FSIZE, &parentAfter))
	if parentBefore != parentAfter {
		t.Fatal("child fixture changed parent resource limit")
	}
	if err != nil {
		t.Fatalf("actual short-write child: %v: %s", err, out)
	}
	if !bytes.Contains(out, []byte("native-short-4096-efbig-original-tail\n")) {
		t.Fatalf("actual short-write child did not complete its assertions: %s", out)
	}
	t.Logf("%s", out)
}

func TestStorageEnrollmentNativeLinuxShortWriteHelper(t *testing.T) {
	if os.Getenv("JPACK_STORAGE_NATIVE_SHORT_HELPER") != "1" {
		return
	}
	root, err := os.OpenRoot(os.Getenv("JPACK_STORAGE_NATIVE_SHORT_ROOT"))
	storageTestMust(t, err)
	defer root.Close()
	f, err := root.OpenFile("original", os.O_CREATE|os.O_EXCL|os.O_RDWR, 0600)
	storageTestMust(t, err)
	defer f.Close()
	original := bytes.Repeat([]byte{0x19}, 8192)
	storageTestMust(t, storageEnrollmentNativeWrite(t.Context(), f, original, 0))
	storageTestMust(t, storageEnrollmentNativeSync(t.Context(), f))
	before, err := f.Stat()
	storageTestMust(t, err)
	var originalLimit unix.Rlimit
	storageTestMust(t, unix.Getrlimit(unix.RLIMIT_FSIZE, &originalLimit))
	if originalLimit.Cur < 8192 || originalLimit.Max < 8192 {
		t.Fatal("native short fixture requires the original 8192-byte file allowance")
	}
	signals := make(chan os.Signal, 1)
	signal.Notify(signals, syscall.SIGXFSZ)
	defer signal.Stop(signals)
	limited := originalLimit
	limited.Cur = 4096
	storageTestMust(t, unix.Setrlimit(unix.RLIMIT_FSIZE, &limited))
	defer func() { storageTestMust(t, unix.Setrlimit(unix.RLIMIT_FSIZE, &originalLimit)) }()
	data := bytes.Repeat([]byte{0x68}, 65536)
	if err := storageEnrollmentNativeWrite(t.Context(), f, data, 0); !errors.Is(err, io.ErrShortWrite) {
		t.Fatalf("actual one-call kernel short must refuse: %v", err)
	}
	// This separate negative invocation starts at the limit. It is not a
	// continuation/retry of the partial write and cannot modify the original.
	if err := storageEnrollmentNativeWrite(t.Context(), f, []byte{0xff}, 4096); !errors.Is(err, syscall.EFBIG) {
		t.Fatalf("actual file-limit errno must survive: %v", err)
	}
	storageTestMust(t, unix.Setrlimit(unix.RLIMIT_FSIZE, &originalLimit))
	storageTestMust(t, storageEnrollmentNativeSync(t.Context(), f))
	after, err := f.Stat()
	storageTestMust(t, err)
	got := make([]byte, len(original))
	_, err = f.ReadAt(got, 0)
	storageTestMust(t, err)
	want := append(append([]byte{}, data[:4096]...), original[4096:]...)
	if !os.SameFile(before, after) || after.Size() != 8192 || !bytes.Equal(got, want) {
		t.Fatal("short/error changed identity/extent, lost original tail, or retried remaining bytes")
	}
	fmt.Println("native-short-4096-efbig-original-tail")
}
