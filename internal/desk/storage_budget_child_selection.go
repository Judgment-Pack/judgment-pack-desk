package desk

import (
	"context"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"
)

// All selections come from the current Server and existing component owners.
// There are no request paths, temp-directory searches or component starts here.
func (s *Server) observeChildStorage(ctx context.Context) (*storageChildObservation, error) {
	out := &storageChildObservation{Coverage: "selected-child-roots-only", Enforcement: "warning-only", State: "measured", ObservedAt: storageStatusTime(time.Now()), LowSpaceThresholdBytes: storageStatusUInt(storageChildLowBytes), Roots: []storageChildRootStatus{}}
	w := &storageChildWalk{ctx: ctx, remaining: storageChildEntries, seen: map[string]bool{}}
	add := func(row storageChildRootStatus) {
		if len(out.Roots) < storageChildRows {
			out.Roots = append(out.Roots, row)
		} else {
			out.State = "partial"
		}
	}
	if !s.mu.TryLock() {
		return nil, os.ErrClosed
	}
	closed := s.closing
	var project *os.Root
	if !closed && s.root != nil {
		project, _ = s.root.OpenRoot(".")
	}
	s.mu.Unlock()
	if closed || project == nil {
		return nil, os.ErrClosed
	}
	defer project.Close()
	before, err := project.Stat(".")
	if err != nil || s.project == nil || !os.SameFile(before, s.project.info) || !sameDirectory(s.projectDir, before) || !s.storageChildProjectStationary() {
		return nil, os.ErrClosed
	}
	for _, row := range s.storageChildRuntime(ctx, w) {
		add(row)
	}
	if j := s.jobs; j != nil {
		row := storageChildUnknown("runner", "workspace")
		if j.mu.TryLock() {
			if !j.closed {
				parent, parentPath := project, s.projectDir
				if s.assistant != nil && s.assistant.root != nil {
					if relative, err := filepath.Rel(s.assistant.dir, j.dir); err == nil && (relative == "." || filepath.IsLocal(relative)) {
						parent, parentPath = s.assistant.root, s.assistant.dir
					}
				}
				row = w.selectedPath("runner", "workspace", j.dir, parent, parentPath)
			}
			j.mu.Unlock()
		}
		add(row)
	} else {
		row := storageChildUnknown("runner", "workspace")
		row.State, row.Reason = "inactive", "none"
		add(row)
	}
	if g := s.localGateway; g != nil {
		rows := []storageChildRootStatus{storageChildUnknown("gateway", "runtime"), storageChildUnknown("gateway", "connections"), storageChildUnknown("gateway", "handoffs")}
		if g.mu.TryLock() {
			if !g.closed && s.assistant != nil && s.assistant.root != nil {
				// Store name is only a selection. Match its actual held descriptor first.
				selected, se := s.assistant.root.Stat(".")
				if se == nil && sameDirectory(s.assistant.dir, selected) {
					handoffs := g.inputHandoffs
					if handoffs == "" {
						handoffs = filepath.Join(s.assistant.dir, "input-handoffs")
					}
					paths := []string{platformPath(s.assistant.dir, "local-gateway"), platformPath(s.assistant.dir, "gateway-connections"), handoffs}
					for i := range paths {
						rows[i] = w.selectedPath("gateway", rows[i].Role, paths[i], s.assistant.root, s.assistant.dir)
					}
					if !sameDirectory(s.assistant.dir, selected) {
						for i := range rows {
							rows[i] = storageChildUnknown("gateway", rows[i].Role)
							rows[i].Reason = "changed"
						}
					}
				}
			}
			g.mu.Unlock()
		}
		for _, row := range rows {
			add(row)
		}
	} else {
		row := storageChildUnknown("gateway", "runtime")
		row.State, row.Reason = "inactive", "none"
		add(row)
	}
	for _, row := range s.storageChildNative(ctx, w) {
		add(row)
	}
	// The original scratch Close callback and Server Close must wait until the
	// read finishes. No borrowed pointer escapes this mutex or starts Git.
	if s.mu.TryLock() {
		active := false
		if !s.closing {
			for _, root := range s.childScratch {
				if root != nil {
					active = true
					add(w.heldPath("git", "active-scratch", root, root.Name()))
				}
			}
			if s.childScratchOverflow != 0 {
				active = true
				add(storageChildUnknown("git", "active-scratch"))
			}
		}
		if !active {
			row := storageChildUnknown("git", "active-scratch")
			row.State, row.Reason = "inactive", "none"
			add(row)
		}
		closed = s.closing
		s.mu.Unlock()
	} else {
		add(storageChildUnknown("git", "active-scratch"))
	}
	if closed || !sameDirectory(s.projectDir, before) || !s.storageChildProjectStationary() {
		return nil, os.ErrClosed
	}
	for _, row := range out.Roots {
		if row.State == "partial" || row.State == "unavailable" {
			out.State = "partial"
		}
	}
	// A bounded/cancelled walk is an explicitly partial observation. The outer
	// request cancellation still prevents publication by storageChildStatus.
	if ctx.Err() != nil {
		out.State = "partial"
	}
	return out, nil
}

func (s *Server) storageChildRuntime(ctx context.Context, w *storageChildWalk) []storageChildRootStatus {
	audit, signing := storageChildUnknown("runtime", "audit"), storageChildUnknown("runtime", "signing-metadata")
	unavailable := []storageChildRootStatus{audit, signing}
	// Only these two environment selectors affect root selection. Calling
	// runtimeEnv would also read signing-public-key state; observation needs none.
	values := map[string]string{}
	if s.usesStartupRuntimeEnvironment() {
		values[runtimeConfigEnv] = os.Getenv(runtimeConfigEnv)
		values[runtimeSigningKeyEnv] = os.Getenv(runtimeSigningKeyEnv)
	}
	config := strings.TrimSpace(values[runtimeConfigEnv])
	if config == "" {
		config = "jpack.json"
	}
	if !filepath.IsAbs(config) {
		config = filepath.Join(s.projectDir, config)
	}
	root, current, close, err := storageChildPinAbsolute(ctx, filepath.Dir(config))
	if err != nil {
		return unavailable
	}
	defer close()
	// Relative project configuration must join the actual retained project,
	// rather than a replacement reached through the project's pathname.
	if relative, e := filepath.Rel(s.projectDir, filepath.Dir(config)); e == nil && (relative == "." || filepath.IsLocal(relative)) {
		held, e := s.root.OpenRoot(relative)
		if e != nil {
			return unavailable
		}
		a, ae := held.Stat(".")
		b, be := root.Stat(".")
		held.Close()
		if ae != nil || be != nil || !os.SameFile(a, b) {
			return unavailable
		}
	}
	name := filepath.Base(config)
	info, err := root.Lstat(name)
	if os.IsNotExist(err) {
		audit.State, audit.Reason, signing.State, signing.Reason = "inactive", "none", "inactive", "none"
		return []storageChildRootStatus{audit, signing}
	}
	if err != nil || !info.Mode().IsRegular() || info.Size() < 0 || info.Size() > 1<<20 || ctx.Err() != nil {
		return unavailable
	}
	file, err := root.OpenFile(name, os.O_RDONLY|openNoFollow|openNonBlocking, 0)
	if err != nil {
		return unavailable
	}
	held, err := file.Stat()
	if err != nil || !os.SameFile(info, held) {
		file.Close()
		return unavailable
	}
	raw, err := io.ReadAll(io.LimitReader(file, (1<<20)+1))
	file.Close()
	if err != nil || len(raw) > 1<<20 || !uniqueJSON(raw) {
		return unavailable
	}
	var doc map[string]json.RawMessage
	if json.Unmarshal(raw, &doc) != nil || doc == nil {
		return unavailable
	}
	for key := range doc {
		if !slices.Contains([]string{"configVersion", "packs", "graphs", "audit", "requireReviewed", "requireComparableFacts"}, key) {
			return unavailable
		}
	}
	var version string
	if json.Unmarshal(doc["configVersion"], &version) != nil || !slices.Contains([]string{"1", "2", "3", "4", "5", "6", "7"}, version) {
		return unavailable
	}
	if len(doc["audit"]) == 0 || string(doc["audit"]) == "null" {
		audit.State, audit.Reason, signing.State, signing.Reason = "inactive", "none", "inactive", "none"
	} else {
		var selection map[string]json.RawMessage
		if json.Unmarshal(doc["audit"], &selection) != nil || selection == nil {
			return unavailable
		}
		for key := range selection {
			if !slices.Contains([]string{"dir", "chain", "signingKey", "signingPublicKey", "timestampAuthority"}, key) {
				return unavailable
			}
		}
		var dir string
		if json.Unmarshal(selection["dir"], &dir) != nil || dir == "" {
			return unavailable
		}
		parts, err := auditDirParts(dir)
		if err != nil {
			return unavailable
		}
		audit = w.selectedPath("runtime", "audit", filepath.Join(append([]string{filepath.Dir(config)}, parts...)...), root, filepath.Dir(config))
		chained := true
		if value := selection["chain"]; value != nil && json.Unmarshal(value, &chained) != nil {
			return unavailable
		}
		key := ""
		if chained {
			key = strings.TrimSpace(values[runtimeSigningKeyEnv])
			if key == "" && selection["signingKey"] != nil {
				if json.Unmarshal(selection["signingKey"], &key) != nil {
					return unavailable
				}
			}
		}
		if key == "" {
			signing.State, signing.Reason = "inactive", "none"
		} else if filepath.IsAbs(key) {
			// Inspect ONLY the selected file's metadata, never its bytes or siblings.
			parent, check, done, err := storageChildPinAbsolute(ctx, filepath.Dir(key))
			if err == nil {
				metadata, e := parent.Lstat(filepath.Base(key))
				if e == nil && metadata.Mode().IsRegular() && metadata.Size() >= 0 && metadata.Size() <= 1<<60 && ctx.Err() == nil {
					b, f, d := storageStatusUInt(uint64(metadata.Size())), "1", "0"
					identity := identityKey(metadata)
					if identity == "" || w.remaining == 0 {
						signing.Reason = "bounded"
					} else if w.seen[identity] {
						signing.State, signing.Reason = "partial", "shared"
					} else {
						w.remaining--
						w.seen[identity] = true
						signing.State, signing.Reason = "measured", "none"
						signing.LogicalBytes, signing.Files, signing.Directories = &b, &f, &d
					}
					after, e := parent.Lstat(filepath.Base(key))
					if e != nil || !os.SameFile(metadata, after) || metadata.Size() != after.Size() || !metadata.ModTime().Equal(after.ModTime()) || check() != nil {
						signing = storageChildUnknown("runtime", "signing-metadata")
						signing.Reason = "changed"
					}
				}
				done()
			}
		}
	}
	after, err := root.Lstat(name)
	if err != nil || !os.SameFile(info, after) || info.Size() != after.Size() || !info.ModTime().Equal(after.ModTime()) || current() != nil {
		return unavailable
	}
	return []storageChildRootStatus{audit, signing}
}

func (s *Server) storageChildProjectStationary() bool {
	if !s.location.mu.TryLock() {
		return false
	}
	defer s.location.mu.Unlock()
	return !s.location.moving.Load() && s.location.moved == ""
}
