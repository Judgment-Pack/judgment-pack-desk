package exchange

import (
	"bytes"
	"context"
	"crypto/sha1"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// OpenRelease resolves one explicit tag and reads only blobs at that commit.
// It never checks out or executes catalog files. All commands have bounded
// output and time; prompts, hooks, external protocols and user Git config are off.
func OpenRelease(ctx context.Context, location, tag string) (*GitTree, func(), error) {
	if _, e := CompareTags(tag, tag); e != nil {
		return nil, func() {}, e
	}
	tree := &GitTree{directory: location}
	close := func() {}
	if u, e := url.Parse(location); e == nil && u.Scheme != "" {
		if u.Scheme != "https" || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" {
			return nil, close, errors.New("choose an HTTPS Git catalog without credentials or a local Git directory")
		}
		temp, e := os.MkdirTemp("", "jpack-exchange-")
		if e != nil {
			return nil, close, e
		}
		tree.directory = temp
		tree.remote = location
		close = func() { os.RemoveAll(temp) }
		if _, e = tree.git(ctx, 4096, "init", "--bare", "--quiet"); e != nil {
			close()
			return nil, func() {}, e
		}
		if _, e = tree.git(ctx, 4096, "fetch", "--quiet", "--depth=1", "--no-tags", "--no-recurse-submodules", location, "refs/tags/"+tag+":refs/tags/"+tag); e != nil {
			close()
			return nil, func() {}, e
		}
	} else {
		if !filepath.IsAbs(location) {
			return nil, close, errors.New("choose an absolute local Git catalog directory")
		}
		info, e := os.Stat(location)
		if e != nil || !info.IsDir() {
			return nil, close, errors.New("catalog directory is unavailable")
		}
	}
	raw, e := tree.git(ctx, 128, "rev-parse", "--verify", "refs/tags/"+tag)
	if e != nil || !commitPattern.Match(bytes.TrimSpace(raw)) {
		close()
		return nil, func() {}, errors.New("release tag does not resolve to an object")
	}
	id := strings.TrimSpace(string(raw))
	// Verify every object in the tag -> commit -> tree -> blob chain. Git's
	// cat-file otherwise trusts object filenames even when stored bytes changed.
	for depth := 0; depth < 8; depth++ {
		kindBytes, e := tree.git(ctx, 128, "cat-file", "-t", id)
		if e != nil {
			close()
			return nil, func() {}, e
		}
		kind := strings.TrimSpace(string(kindBytes))
		if kind != "tag" && kind != "commit" {
			close()
			return nil, func() {}, errors.New("release tag must name a commit")
		}
		object, e := tree.object(ctx, id, kind, 1<<20)
		if e != nil {
			close()
			return nil, func() {}, e
		}
		first, _, _ := bytes.Cut(object, []byte{'\n'})
		if kind == "commit" {
			if !bytes.HasPrefix(first, []byte("tree ")) || !commitPattern.Match(first[5:]) {
				close()
				return nil, func() {}, errors.New("invalid catalog commit tree")
			}
			tree.Commit = id
			tree.root = string(first[5:])
			break
		}
		if !bytes.HasPrefix(first, []byte("object ")) || !commitPattern.Match(first[7:]) {
			close()
			return nil, func() {}, errors.New("invalid annotated release tag")
		}
		id = string(first[7:])
	}
	if tree.Commit == "" {
		close()
		return nil, func() {}, errors.New("release tag chain exceeds its limit")
	}
	tree.trees = map[string][]treeEntry{}

	return tree, close, nil
}

type GitTree struct {
	directory string
	remote    string
	Commit    string
	root      string
	trees     map[string][]treeEntry
	treeBytes int
}
type limitedOutput struct {
	bytes.Buffer
	limit int
}

func (w *limitedOutput) Write(p []byte) (int, error) {
	if len(p) > w.limit-w.Len() {
		return 0, errors.New("catalog output exceeds its limit")
	}
	return w.Buffer.Write(p)
}
func (t *GitTree) git(ctx context.Context, limit int, args ...string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(ctx, 45*time.Second)
	defer cancel()
	argv := []string{"--no-replace-objects", "-c", "credential.helper=", "-c", "core.fsmonitor=false", "-c", "core.hooksPath=/dev/null", "-c", "protocol.ext.allow=never", "-c", "protocol.file.allow=never", "-c", "http.followRedirects=false", "-C", t.directory}
	argv = append(argv, args...)
	cmd := exec.CommandContext(ctx, "git", argv...)
	env := []string{}
	for _, v := range os.Environ() {
		if !strings.HasPrefix(v, "GIT_") {
			env = append(env, v)
		}
	}
	cmd.Env = append(env, "GIT_CONFIG_NOSYSTEM=1", "GIT_CONFIG_GLOBAL=/dev/null", "GIT_TERMINAL_PROMPT=0", "GIT_LFS_SKIP_SMUDGE=1", "GIT_NO_LAZY_FETCH=1", "GIT_ALLOW_PROTOCOL=https")
	out := &limitedOutput{limit: limit}
	cmd.Stdout = out
	cmd.Stderr = &limitedOutput{limit: 4096}
	if e := cmd.Run(); e != nil {
		return nil, errors.New("catalog Git operation failed or exceeded its bounds")
	}
	return out.Bytes(), nil
}

// object verifies both the requested bound and Git's exact object identity.
// SHA-1 is used only for existing Git object IDs, never new artifact digests.
func (t *GitTree) object(ctx context.Context, id, kind string, limit int) ([]byte, error) {
	if !commitPattern.MatchString(id) {
		return nil, errors.New("invalid catalog object identity")
	}
	info, e := t.git(ctx, 128, "cat-file", "-s", id)
	if e != nil {
		return nil, e
	}
	size, e := strconv.Atoi(strings.TrimSpace(string(info)))
	if e != nil || size < 0 || size > limit {
		return nil, errors.New("catalog object exceeds its byte limit")
	}
	raw, e := t.git(ctx, limit, "cat-file", kind, id)
	if e != nil {
		return nil, e
	}
	if len(raw) != size {
		return nil, errors.New("catalog object size changed")
	}
	header := []byte(fmt.Sprintf("%s %d\x00", kind, len(raw)))
	var actual string
	if len(id) == 40 {
		h := sha1.New()
		h.Write(header)
		h.Write(raw)
		actual = hex.EncodeToString(h.Sum(nil))
	} else {
		h := sha256.New()
		h.Write(header)
		h.Write(raw)
		actual = hex.EncodeToString(h.Sum(nil))
	}
	if actual != id {
		return nil, errors.New("catalog object does not match its Git identity")
	}
	return raw, nil
}

type treeEntry struct{ mode, name, id string }

func (t *GitTree) entries(ctx context.Context, id string) ([]treeEntry, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if entries, ok := t.trees[id]; ok {
		return entries, nil
	}
	raw, e := t.object(ctx, id, "tree", 4<<20)
	if e != nil {
		return nil, e
	}
	t.treeBytes += len(raw)
	if t.treeBytes > 4<<20 {
		return nil, errors.New("catalog tree exceeds its byte limit")
	}
	size := len(id) / 2
	entries := []treeEntry{}
	seen := map[string]bool{}
	for len(raw) > 0 {
		mode, tail, ok := bytes.Cut(raw, []byte{' '})
		if !ok {
			return nil, errors.New("invalid catalog tree mode")
		}
		name, tail, ok := bytes.Cut(tail, []byte{0})
		if !ok || len(name) == 0 || bytes.ContainsAny(name, "/\\\r\n") || string(name) == "." || string(name) == ".." || len(tail) < size || seen[string(name)] {
			return nil, errors.New("invalid catalog tree entry")
		}
		seen[string(name)] = true
		entries = append(entries, treeEntry{string(mode), string(name), hex.EncodeToString(tail[:size])})
		raw = tail[size:]
	}
	t.trees[id] = entries
	return entries, nil
}
func (t *GitTree) resolve(ctx context.Context, path string) (treeEntry, error) {
	id := t.root
	parts := strings.Split(path, "/")
	for i, part := range parts {
		entries, e := t.entries(ctx, id)
		if e != nil {
			return treeEntry{}, e
		}
		found := false
		for _, entry := range entries {
			if entry.name != part {
				continue
			}
			if i == len(parts)-1 {
				return entry, nil
			}
			if entry.mode != "40000" {
				return treeEntry{}, errors.New("catalog path is not a directory")
			}
			id = entry.id
			found = true
			break
		}
		if !found {
			return treeEntry{}, os.ErrNotExist
		}
	}
	return treeEntry{}, os.ErrNotExist
}
func (t *GitTree) Files(ctx context.Context) ([]string, error) {
	root, e := t.resolve(ctx, "entries")
	if errors.Is(e, os.ErrNotExist) {
		return []string{}, nil
	}
	if e != nil {
		return nil, e
	}
	if root.mode != "40000" {
		return nil, errors.New("catalog entries must be a directory")
	}
	files := []string{}
	visits := 0
	var walk func(string, string, int) error
	walk = func(id, path string, depth int) error {
		if err := ctx.Err(); err != nil {
			return err
		}
		visits++
		if visits > 4096 {
			return errors.New("catalog directory visits exceed their limit")
		}
		if depth > 8 {
			return errors.New("catalog tree exceeds its nesting limit")
		}
		entries, e := t.entries(ctx, id)
		if e != nil {
			return e
		}
		if len(entries) == 0 && path != "entries" {
			return errors.New("catalog entry directories cannot be empty")
		}
		for _, entry := range entries {
			name := path + "/" + entry.name
			switch entry.mode {
			case "40000":
				if e := walk(entry.id, name, depth+1); e != nil {
					return e
				}
			case "100644", "100755":
				files = append(files, name)
				if len(files) > 10000 {
					return errors.New("catalog file count exceeds its limit")
				}
			default:
				return errors.New("catalog entries cannot contain symlinks or submodules")
			}
		}
		return nil
	}
	if e := walk(root.id, "entries", 0); e != nil {
		return nil, e
	}
	return files, nil
}
func (t *GitTree) Read(ctx context.Context, path string, limit int) ([]byte, error) {
	if path != "manifest.json" && !strings.HasPrefix(path, "entries/") {
		return nil, errors.New("invalid catalog blob path")
	}
	for _, part := range strings.Split(path, "/") {
		if part == "" || part == "." || part == ".." || strings.ContainsAny(part, "\x00\r\n\\") {
			return nil, errors.New("invalid catalog blob path")
		}
	}
	entry, e := t.resolve(ctx, path)
	if e != nil {
		return nil, e
	}
	if entry.mode != "100644" && entry.mode != "100755" {
		return nil, errors.New("catalog source must be a regular blob")
	}
	return t.object(ctx, entry.id, "blob", limit)
}
