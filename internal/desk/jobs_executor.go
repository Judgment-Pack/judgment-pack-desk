package desk

import (
	"context"
	"crypto/sha256"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

const runnerExecutableLimit int64 = 256 << 20

func runnerOwnershipUnavailable() error {
	return errors.New("Runner executor ownership is unavailable; Jobs is held")
}

type runnerExecutableSource struct {
	path                          string
	file                          *os.File
	info                          os.FileInfo
	digest                        [sha256.Size]byte
	afterSnapshotDirectoryForTest func(string)
}

// Pin the configured executable before any background launch. Retaining the
// descriptor and original digest refuses both replacement and in-place changes.
func pinRunnerExecutable(path string) (*runnerExecutableSource, error) {
	if !custodyChecked || !filepath.IsAbs(path) || filepath.Clean(path) != path {
		return nil, runnerOwnershipUnavailable()
	}
	parent, _, err := openRunnerDirectory(filepath.Dir(path), false)
	if err != nil {
		return nil, runnerOwnershipUnavailable()
	}
	defer parent.Close()
	before, err := parent.Lstat(filepath.Base(path))
	if err != nil || !safeRunnerExecutable(before) {
		return nil, runnerOwnershipUnavailable()
	}
	file, err := parent.OpenFile(filepath.Base(path), os.O_RDONLY|openNoFollow|openNonBlocking, 0)
	if err != nil {
		return nil, runnerOwnershipUnavailable()
	}
	info, err := file.Stat()
	if err != nil || !safeRunnerExecutable(info) || !os.SameFile(before, info) {
		file.Close()
		return nil, runnerOwnershipUnavailable()
	}
	source := &runnerExecutableSource{path: path, file: file, info: info}
	hash := sha256.New()
	n, err := io.Copy(hash, io.NewSectionReader(file, 0, runnerExecutableLimit+1))
	copy(source.digest[:], hash.Sum(nil))
	if err != nil || n != info.Size() || source.check() != nil {
		source.close()
		return nil, runnerOwnershipUnavailable()
	}
	return source, nil
}

func safeRunnerExecutable(info os.FileInfo) bool {
	owner, known := ownerOf(info)
	return info.Mode().IsRegular() && info.Mode().Perm()&0022 == 0 && info.Mode().Perm()&0111 != 0 && known && (owner == effectiveUser() || owner == 0) && info.Size() > 0 && info.Size() <= runnerExecutableLimit
}

func (s *runnerExecutableSource) check() error {
	if s == nil || s.file == nil {
		return runnerOwnershipUnavailable()
	}
	entry, err := os.Lstat(s.path)
	if err != nil || !safeRunnerExecutable(entry) || !os.SameFile(s.info, entry) || entry.Size() != s.info.Size() || !entry.ModTime().Equal(s.info.ModTime()) {
		return runnerOwnershipUnavailable()
	}
	held, err := s.file.Stat()
	if err != nil || !safeRunnerExecutable(held) || !os.SameFile(s.info, held) || held.Size() != s.info.Size() || !held.ModTime().Equal(s.info.ModTime()) {
		return runnerOwnershipUnavailable()
	}
	return nil
}

func (s *runnerExecutableSource) close() {
	if s != nil && s.file != nil {
		s.file.Close()
		s.file = nil
	}
}

// Probe and launch execute exactly this private snapshot. The source is never
// executed again by pathname after probing. Every key-refusal fallback gets a
// fresh snapshot under the same pinned source and ownership demand.
func (s *runnerExecutableSource) snapshot() (string, func(), error) {
	if s.check() != nil {
		return "", nil, runnerOwnershipUnavailable()
	}
	root, dir, cleanup, err := newRunnerSnapshotDirectory(s.afterSnapshotDirectoryForTest)
	if err != nil {
		return "", nil, runnerOwnershipUnavailable()
	}
	file, err := root.OpenFile("runner", os.O_CREATE|os.O_EXCL|os.O_WRONLY|openNoFollow, 0500)
	if err != nil {
		cleanup()
		return "", nil, runnerOwnershipUnavailable()
	}
	hash := sha256.New()
	n, err := io.Copy(io.MultiWriter(file, hash), io.NewSectionReader(s.file, 0, runnerExecutableLimit+1))
	var digest [sha256.Size]byte
	copy(digest[:], hash.Sum(nil))
	if err == nil && (n != s.info.Size() || digest != s.digest || s.check() != nil) {
		err = runnerOwnershipUnavailable()
	}
	if err == nil {
		err = file.Sync()
	}
	if e := file.Close(); err == nil {
		err = e
	}
	if err != nil {
		cleanup()
		return "", nil, runnerOwnershipUnavailable()
	}
	return filepath.Join(dir, "runner"), cleanup, nil
}

// Resolve only trusted temporary-parent aliases, validating every directory
// actually traversed before following another component. In particular, an
// unsafe shared directory cannot disappear from validation through a symlink
// target or "..". Configured executable ancestry calls this with aliases off.
func openRunnerDirectory(path string, aliases bool) (*os.Root, string, error) {
	if !custodyChecked || !filepath.IsAbs(path) || len(path) > 4096 || strings.ContainsAny(path, "\x00\r\n") {
		return nil, "", runnerOwnershipUnavailable()
	}
	root, err := os.OpenRoot(string(filepath.Separator))
	if err != nil {
		return nil, "", runnerOwnershipUnavailable()
	}
	stack := []*os.Root{root}
	closeStack := func() {
		for _, r := range stack {
			r.Close()
		}
	}
	fail := func() (*os.Root, string, error) { closeStack(); return nil, "", runnerOwnershipUnavailable() }
	info, err := root.Stat(".")
	if err != nil || safeDirectory("Runner directory ancestry", info, false) != nil {
		return fail()
	}
	parts := strings.Split(path[1:], string(filepath.Separator))
	resolved := []string{}
	links := 0
	for len(parts) > 0 {
		if len(parts)+len(stack) > 256 {
			return fail()
		}
		part := parts[0]
		parts = parts[1:]
		if part == "" || part == "." {
			continue
		}
		if part == ".." {
			if len(stack) > 1 {
				stack[len(stack)-1].Close()
				stack = stack[:len(stack)-1]
				resolved = resolved[:len(resolved)-1]
			}
			continue
		}
		current := stack[len(stack)-1]
		entry, e := current.Lstat(part)
		if e != nil {
			return fail()
		}
		if entry.Mode()&os.ModeSymlink != 0 {
			links++
			owner, known := ownerOf(entry)
			if !aliases || links > 40 || !known || (owner != effectiveUser() && owner != 0) {
				return fail()
			}
			target, e := current.Readlink(part)
			after, statErr := current.Lstat(part)
			if e != nil || statErr != nil || !os.SameFile(entry, after) {
				return fail()
			}
			if filepath.IsAbs(target) {
				for len(stack) > 1 {
					stack[len(stack)-1].Close()
					stack = stack[:len(stack)-1]
				}
				resolved = nil
			}
			parts = append(strings.Split(target, string(filepath.Separator)), parts...)
			continue
		}
		if safeDirectory("Runner directory ancestry", entry, false) != nil || len(stack) >= 128 {
			return fail()
		}
		next, e := current.OpenRoot(part)
		if e != nil {
			return fail()
		}
		held, e := next.Stat(".")
		if e != nil || !os.SameFile(entry, held) || safeDirectory("Runner directory ancestry", held, false) != nil {
			next.Close()
			return fail()
		}
		stack = append(stack, next)
		resolved = append(resolved, part)
	}
	held := stack[len(stack)-1]
	for _, r := range stack[:len(stack)-1] {
		r.Close()
	}
	return held, filepath.Join(append([]string{string(filepath.Separator)}, resolved...)...), nil
}

// Create relative to a pinned, already validated parent. Retain the exact new
// leaf, never resolve it through aliases, and clean only our file and that same
// empty directory. A substituted entry is never followed or recursively removed.
func newRunnerSnapshotDirectory(afterPin func(string)) (*os.Root, string, func(), error) {
	parent, path, err := openRunnerDirectory(os.TempDir(), true)
	if err != nil {
		return nil, "", nil, err
	}
	nonce, err := NewToken()
	if err != nil {
		parent.Close()
		return nil, "", nil, runnerOwnershipUnavailable()
	}
	name := "jpack-desk-runner-" + nonce
	if parent.Mkdir(name, 0700) != nil {
		parent.Close()
		return nil, "", nil, runnerOwnershipUnavailable()
	}
	root, _, err := openOwnFolder(parent, []string{name}, false, "Runner executable snapshot")
	if err != nil {
		// No identity was retained. Preserve ambiguous storage rather than
		// removing an entry that may have replaced the one we created.
		parent.Close()
		return nil, "", nil, runnerOwnershipUnavailable()
	}
	info, err := root.Stat(".")
	if err != nil {
		root.Close()
		parent.Close()
		return nil, "", nil, runnerOwnershipUnavailable()
	}
	var once sync.Once
	cleanup := func() {
		once.Do(func() {
			_ = root.Remove("runner")
			root.Close()
			entry, e := parent.Lstat(name)
			if e == nil && entry.IsDir() && entry.Mode()&os.ModeSymlink == 0 && os.SameFile(info, entry) {
				_ = parent.Remove(name)
			}
			parent.Close()
		})
	}
	dir := filepath.Join(path, name)
	if afterPin != nil {
		afterPin(dir)
	}
	entry, err := parent.Lstat(name)
	if err != nil || !entry.IsDir() || entry.Mode()&os.ModeSymlink != 0 || !os.SameFile(info, entry) {
		cleanup()
		return nil, "", nil, runnerOwnershipUnavailable()
	}
	return root, dir, cleanup, nil
}

type runnerBootRequirements struct {
	Version      int      `json:"version"`
	Capabilities []string `json:"capabilities"`
}

func runnerRequiredOwnershipCapabilities() runnerBootRequirements {
	return runnerBootRequirements{Version: 1, Capabilities: []string{"jobs/1", "required-boot-capabilities/1", "executor-ownership/1"}}
}
func runnerRequiredAdmissionCapabilities() runnerBootRequirements {
	v := runnerRequiredOwnershipCapabilities()
	v.Capabilities = append(v.Capabilities, "policy-manual-pack-run/1")
	return v
}

// EOF-only input, bounded output and time, no token, boot line or environment
// secret. Waiting for process exit precedes validation of the closed object.
func probeRunnerOwnership(bin string) error {
	return probeRunnerCapabilities(bin, false)
}
func probeRunnerAdmission(bin string) error { return probeRunnerCapabilities(bin, true) }
func probeRunnerCapabilities(bin string, admission bool) error {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, bin, "capabilities", "--format", "json")
	cmd.Env = []string{"LANG=C", "LC_ALL=C"}
	cmd.WaitDelay = 100 * time.Millisecond
	stdout, stderr := &cappedBuffer{limit: 4096}, &cappedBuffer{limit: 4096}
	cmd.Stdout, cmd.Stderr = stdout, stderr
	if cmd.Run() != nil || ctx.Err() != nil || stdout.exceeded() || stderr.exceeded() || len(stderr.Bytes()) != 0 {
		return runnerOwnershipUnavailable()
	}
	raw := stdout.Bytes()
	var report struct {
		Version             int      `json:"version"`
		Capabilities        []string `json:"capabilities"`
		GovernanceAdmission bool     `json:"governanceAdmission"`
		GovernanceProfiles  []string `json:"governanceProfiles,omitempty"`
	}
	if rejectDuplicateMembers(raw) != nil || decodeDataJSON(raw, &report) != nil {
		return runnerOwnershipUnavailable()
	}
	fields := []string{"version", "capabilities", "governanceAdmission"}
	want := runnerRequiredOwnershipCapabilities().Capabilities
	if report.Version == 3 {
		fields = append(fields, "governanceProfiles")
		want = runnerRequiredAdmissionCapabilities().Capabilities
		if !report.GovernanceAdmission || len(report.GovernanceProfiles) != 1 || report.GovernanceProfiles[0] != policyExecutionProfile {
			return runnerOwnershipUnavailable()
		}
	} else if report.Version != 2 || admission || report.GovernanceAdmission {
		return runnerOwnershipUnavailable()
	}
	if policyAuthorityAnchorObject(raw, fields, "") != nil {
		return runnerOwnershipUnavailable()
	}
	if len(report.Capabilities) != len(want) {
		return runnerOwnershipUnavailable()
	}
	for i, capability := range want {
		if report.Capabilities[i] != capability {
			return runnerOwnershipUnavailable()
		}
	}
	return nil
}
