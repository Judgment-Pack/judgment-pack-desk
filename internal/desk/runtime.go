package desk

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// ResolveRuntime turns the runtime named on the command line into an absolute
// path this process can execute, or says why it cannot — at launch, in one
// sentence, rather than on every relay connection.
//
// **The chassis used to accept any string here.** A path that did not exist
// was printed under `runtime:` as if it were a fact, the desk started, and
// the first WebSocket connection failed to spawn the runtime — which the page
// reported as the connection closing and retried forever. A name with no path
// separator is looked up on PATH, as `exec.Command` would; a name with one is
// taken as a path and must exist, be a file, and carry an execute bit.
func ResolveRuntime(name string) (string, error) {
	if name == "" {
		return "", errors.New("no runtime binary was named: pass --jpack")
	}
	if !strings.ContainsRune(name, os.PathSeparator) {
		found, err := exec.LookPath(name)
		if err != nil {
			return "", fmt.Errorf("the runtime binary %q is not on PATH: %w", name, err)
		}
		return filepath.Abs(found)
	}
	abs, err := filepath.Abs(name)
	if err != nil {
		return "", err
	}
	info, err := os.Stat(abs)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return "", fmt.Errorf("the runtime binary %s does not exist", abs)
		}
		return "", fmt.Errorf("the runtime binary %s cannot be read: %w", abs, err)
	}
	if info.IsDir() {
		return "", fmt.Errorf("the runtime binary %s is a directory", abs)
	}
	if info.Mode()&0o111 == 0 {
		return "", fmt.Errorf("the runtime binary %s is not executable", abs)
	}
	return abs, nil
}

// The runtime's own project files: the configuration it reads, and the
// reviewed-set lock it writes beside it (runtime ADR-0019).
const (
	runtimeConfigName = "jpack.json"
	runtimeLockName   = "jpack.lock.json"
)

// runtimeConfigEnv is the variable the runtime reads its configuration's
// path from when no `--config` is given, before `./jpack.json`.
const runtimeConfigEnv = "JPACK_CONFIG"

// runtimeEnv is the environment the relay's runtime gets: this process's own,
// except that a desk Desk made never reads another project's configuration.
//
// `jpack mcp` has no `--config`. It reads `$JPACK_CONFIG`, then
// `./jpack.json`. Inherited, a `JPACK_CONFIG` set where Desk was started
// pointed every named desk's runtime at that one configuration, so a desk
// made gated evaluated under another project's law. Removed, the runtime
// reads `./jpack.json` in the directory it was started in, which on Linux it
// entered through the descriptor this desk holds. Naming the file instead
// would put a pathname back where the descriptor stands.
//
// The startup desk keeps an inherited value. There it is the only way the
// owner can have chosen the configuration, and this desk does not change
// what an existing project reads. `New` logs it.
func (s *Server) runtimeEnv() []string {
	if s.cfg.deskID == "" {
		return nil
	}
	return withoutConfigOverride(os.Environ())
}

// withoutConfigOverride is env without `JPACK_CONFIG`.
func withoutConfigOverride(env []string) []string {
	kept := make([]string, 0, len(env))
	for _, entry := range env {
		if !strings.HasPrefix(entry, runtimeConfigEnv+"=") {
			kept = append(kept, entry)
		}
	}
	return kept
}

// heldDir is a directory a runtime command runs in, held open by this desk.
//
// The descriptor is what Linux starts the child through. The pathname and the
// identity it was opened with are for the hosts that cannot: the pathname is
// re-checked against the identity immediately before the spawn, exactly as
// the relay's is. See `runtimeCommandAt` and `aimRuntimeAt`.
type heldDir struct {
	file *os.File
	path string
	info os.FileInfo
}

// runtimeCommandTimeout bounds one runtime command this desk runs to
// completion.
//
// `jpack mcp` lives as long as its socket and has no bound of its own. The
// commands here answer and exit: on an empty project, in milliseconds. The
// bound is for a runtime that hangs, and it is generous because the first run
// of a newly installed binary can be slow while the host checks it. A variable
// only so a test can see a hang refused without waiting it out.
var runtimeCommandTimeout = 20 * time.Second

// runtimeAnswerLimit is the most of a command's standard output this desk
// reads. `packs schema` and an empty project's `packs lock` answer in well
// under a kilobyte.
const runtimeAnswerLimit = 64 << 10

// runRuntime runs one runtime command to completion in dir and returns what it
// printed on standard output.
//
// The command is built the way the relay's `jpack mcp` is (`runtimeCommandAt`):
// the same binary, resolved the same way, and the same inherited environment.
// What differs is that it ends. It is bounded by `runtimeCommandTimeout`, reads
// nothing, and is read up to `runtimeAnswerLimit`.
//
// A command that exits non-zero still returns what it printed, beside the
// error: with `--format json` the runtime says why in its diagnostics.
func runRuntime(ctx context.Context, bin string, dir heldDir, args ...string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(ctx, runtimeCommandTimeout)
	defer cancel()
	cmd, err := runtimeCommandAt(ctx, bin, dir, args...)
	if err != nil {
		return nil, err
	}
	// These commands run only in a desk this one is making, which never
	// reads another project's configuration (`runtimeEnv`).
	cmd.Env = withoutConfigOverride(os.Environ())
	stdout := &cappedBuffer{limit: runtimeAnswerLimit}
	cmd.Stdout = stdout
	cmd.Stderr = io.Discard
	// A descendant that kept the output open cannot hold this request past
	// the bound either.
	cmd.WaitDelay = time.Second
	// **Immediately before the spawn, and nothing between**, as in the relay.
	if err := aimRuntimeAt(cmd, dir); err != nil {
		return nil, err
	}
	err = cmd.Run()
	command := strings.Join(args, " ")
	if ctx.Err() != nil {
		return nil, fmt.Errorf("the runtime did not finish %s within %s", command, runtimeCommandTimeout)
	}
	if stdout.exceeded() {
		return nil, fmt.Errorf("the runtime's answer to %s was larger than %d bytes", command, runtimeAnswerLimit)
	}
	if err != nil {
		return stdout.Bytes(), fmt.Errorf("the runtime's %s failed: %w", command, err)
	}
	return stdout.Bytes(), nil
}

// cappedBuffer keeps the first limit bytes written to it and notes whether
// there were more. It never refuses a write, so a child that prints too much
// is not stopped by a broken pipe before its exit is seen.
//
// **The buffer is a field, not embedded.** Embedded, `bytes.Buffer`'s
// `ReadFrom` would be promoted, and `io.Copy` (which is how `os/exec` fills a
// writer) prefers it to `Write`: the whole output would be kept, and the bound
// never consulted.
//
// It is locked because `os/exec` stops waiting for its copy after
// `WaitDelay`, and a descendant that kept the output open can still be
// writing when it is read.
type cappedBuffer struct {
	mu    sync.Mutex
	buf   bytes.Buffer
	limit int
	over  bool
}

func (b *cappedBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if room := b.limit - b.buf.Len(); len(p) > room {
		b.over = true
		if room > 0 {
			b.buf.Write(p[:room])
		}
		return len(p), nil
	}
	return b.buf.Write(p)
}

func (b *cappedBuffer) Bytes() []byte {
	b.mu.Lock()
	defer b.mu.Unlock()
	return bytes.Clone(b.buf.Bytes())
}

func (b *cappedBuffer) exceeded() bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.over
}

// runtimeSchema is what the runtime says about the configurations it reads.
type runtimeSchema struct {
	// version is the runtime's own version, as it reports it.
	version string
	// supported is its `supportedConfigVersions`.
	supported []string
}

// readRuntimeSchema asks the runtime which configuration versions it reads:
// `packs schema --format json`, member `supportedConfigVersions`.
//
// Measured on 0.25.0 it is `["1","2","3","4","5"]`, on 0.24.0
// `["1","2","3","4"]`, and on 0.23.1 `["1","2","3"]`. The command reads no
// project, so it can run before a new desk has a configuration.
func readRuntimeSchema(ctx context.Context, bin string, dir heldDir) (runtimeSchema, error) {
	out, err := runRuntime(ctx, bin, dir, "packs", "schema", "--format", "json")
	if err != nil {
		return runtimeSchema{}, fmt.Errorf("the runtime could not say which configuration versions it reads: %w", err)
	}
	var answer struct {
		Tool struct {
			Version string `json:"version"`
		} `json:"tool"`
		Command   string   `json:"command"`
		Status    string   `json:"status"`
		Supported []string `json:"supportedConfigVersions"`
	}
	if json.Unmarshal(out, &answer) != nil || answer.Command != "packs schema" || answer.Status != "valid" || len(answer.Supported) == 0 {
		return runtimeSchema{}, errors.New("the runtime's packs schema named no configuration versions it reads")
	}
	return runtimeSchema{version: answer.Tool.Version, supported: answer.Supported}, nil
}

// lockRuntimeProject runs `packs lock` over the configuration in dir, and
// checks that the lock it wrote pins config: the bytes this desk wrote there.
//
// **`--config` is named, never left to the runtime's search.** The runtime
// takes `--config`, then `$JPACK_CONFIG`, then `./jpack.json`, and the
// environment is this desk's own. A `JPACK_CONFIG` set where Desk was started
// would otherwise have the lock pin another project's configuration, beside
// that project. The digest the runtime reports is compared with the bytes
// written, so a lock of anything else is refused rather than trusted.
//
// root is the same directory as dir, through which the lock's presence is
// checked without resolving a name again.
func lockRuntimeProject(ctx context.Context, bin string, dir heldDir, root *os.Root, config []byte) error {
	out, err := runRuntime(ctx, bin, dir, "packs", "lock", "--config", runtimeConfigName, "--format", "json")
	var answer struct {
		Command      string `json:"command"`
		Status       string `json:"status"`
		ConfigDigest string `json:"configDigest"`
		Diagnostics  []struct {
			Message string `json:"message"`
		} `json:"diagnostics"`
	}
	decoded := out != nil && json.Unmarshal(out, &answer) == nil
	if err != nil || !decoded || answer.Command != "packs lock" || answer.Status != "valid" {
		var said []string
		for _, diagnostic := range answer.Diagnostics {
			if message := strings.TrimSpace(diagnostic.Message); message != "" {
				said = append(said, message)
			}
		}
		switch {
		case len(said) > 0:
			return fmt.Errorf("the runtime did not lock it: %s", strings.Join(said, " "))
		case err != nil:
			return fmt.Errorf("the runtime did not lock it: %w", err)
		}
		return errors.New("the runtime did not lock it: its packs lock did not answer as documented")
	}
	if answer.ConfigDigest != "sha256:"+digestOf(config) {
		return errors.New("the runtime's lock does not pin the configuration this desk wrote")
	}
	if info, err := root.Lstat(runtimeLockName); err != nil || !info.Mode().IsRegular() {
		return errors.New("the runtime reported a lock, but there is none in the new desk")
	}
	return nil
}
