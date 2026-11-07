package desk

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/Judgment-Pack/judgment-pack-desk/internal/storagebudget"
)

// These are observation bounds, never limits on a child's work or storage.
const (
	storageChildEntries   = 4096
	storageChildDepth     = 16
	storageChildNameBytes = 1 << 20
	storageChildLowBytes  = 1 << 30
	storageChildRows      = 64 // <=7 ordinary selections +34 native +17 scratch rows
)

type storageChildRootStatus struct {
	Component      string  `json:"component"`
	Role           string  `json:"role"`
	State          string  `json:"state"`  // measured, partial, unavailable, inactive
	Reason         string  `json:"reason"` // none, unknown, changed, bounded, shared
	LogicalBytes   *string `json:"logicalBytes"`
	Files          *string `json:"files"`
	Directories    *string `json:"directories"`
	AvailableBytes *string `json:"availableBytes"`
	LowSpace       *bool   `json:"lowSpace"`
}
type storageChildObservation struct {
	Coverage               string                   `json:"coverage"`
	Enforcement            string                   `json:"enforcement"`
	State                  string                   `json:"state"`
	ObservedAt             string                   `json:"observedAt"`
	LowSpaceThresholdBytes string                   `json:"lowSpaceThresholdBytes"`
	Roots                  []storageChildRootStatus `json:"roots"`
}

func storageChildUnknown(component, role string) storageChildRootStatus {
	return storageChildRootStatus{Component: component, Role: role, State: "unavailable", Reason: "unknown"}
}

// Uses the existing coalescing/throttling cache, separately per selected Desk.
// Q failure cannot suppress warnings, and a refresh never starts a component.
func (s *Server) storageChildStatus(ctx context.Context, now time.Time) *storageChildObservation {
	cache := &s.childStorageStatus
	view, refresh := cache.begin(now)
	if refresh {
		childCtx, cancel := context.WithTimeout(ctx, time.Second)
		value, err := s.observeChildStorage(childCtx)
		cancel()
		var encoded []byte
		if err == nil {
			encoded, err = storageStatusEncode(value, 32768)
		}
		if ctx.Err() != nil {
			err = ctx.Err()
		}
		view = cache.finish(encoded, time.Now(), err)
	}
	if len(view.encoded) == 0 {
		return nil
	}
	var value storageChildObservation
	if json.Unmarshal(view.encoded, &value) != nil {
		return nil
	}
	if view.stale(time.Now(), false) {
		value.State = "stale"
	}
	return &value
}

type storageChildWalk struct {
	ctx              context.Context
	remaining, names int
	seen             map[string]bool // bounded by the global entry budget; shared objects once
	roots            []os.FileInfo
	statfs           func(context.Context, *os.File) (storageNativeStatfsRaw, error) // isolated metadata-leaf fixtures only
	cut              func(string, *os.Root, string)                                  // deterministic in-walk fixture cuts; nil in production
}

// pinAbsolute retains every named ancestor. No symlink is followed or repaired;
// even an absolute configured destination remains only an observation selection.
func storageChildPinAbsolute(ctx context.Context, path string) (root *os.Root, check func() error, close func(), err error) {
	if err := ctx.Err(); err != nil {
		return nil, nil, nil, err
	}
	if !filepath.IsAbs(path) || len(path) > 4096 {
		return nil, nil, nil, os.ErrInvalid
	}
	base, err := os.OpenRoot(string(filepath.Separator))
	if err != nil {
		return nil, nil, nil, err
	}
	roots := []*os.Root{base}
	pins := []storageNamedRoot{}
	close = func() {
		for i := len(roots) - 1; i >= 0; i-- {
			roots[i].Close()
		}
	}
	parts := strings.Split(strings.TrimPrefix(filepath.Clean(path), string(filepath.Separator)), string(filepath.Separator))
	if len(parts) > 64 {
		close()
		return nil, nil, nil, storagebudget.ErrBounds
	}
	for _, name := range parts {
		if err := ctx.Err(); err != nil {
			close()
			return nil, nil, nil, err
		}
		if name == "" {
			continue
		}
		pin, e := storagePinChild(roots[len(roots)-1], name, false)
		if e != nil {
			close()
			return nil, nil, nil, e
		}
		pins = append(pins, pin)
		roots = append(roots, pin.root)
	}
	check = func() error {
		for _, p := range pins {
			if e := p.current(); e != nil {
				return e
			}
		}
		return nil
	}
	return roots[len(roots)-1], check, close, nil
}
func storageChildByteCounts(raw storageNativeStatfsRaw) (storageNativeByteCounts, error) {
	switch runtime.GOOS {
	case "linux":
		return storageNativeLinuxBlockBytes(raw)
	case "darwin":
		return storageNativeDarwinBlockBytes(raw)
	default:
		return storageNativeByteCounts{}, storagebudget.ErrUnsupported
	}
}
func (w *storageChildWalk) root(component, role string, root *os.Root) storageChildRootStatus {
	row := storageChildUnknown(component, role)
	info, err := root.Stat(".")
	if err != nil || w.ctx.Err() != nil {
		return row
	}
	// Exact aliases are reported without another count or sum. Nested roots and
	// hardlinks are caught by the global bounded object identity set below.
	for _, prior := range w.roots {
		if os.SameFile(info, prior) {
			row.State, row.Reason = "partial", "shared"
			return row
		}
	}
	w.roots = append(w.roots, info)
	f, err := root.Open(".")
	if err == nil {
		statfs := w.statfs
		if statfs == nil {
			statfs = storageNativeStatfs
		}
		raw, re := statfs(w.ctx, f)
		f.Close()
		if re == nil {
			if counts, ce := storageChildByteCounts(raw); ce == nil {
				available := storageStatusUInt(counts.Available)
				low := counts.Available < storageChildLowBytes
				row.AvailableBytes, row.LowSpace = &available, &low
			}
		}
	}
	var bytes, files, dirs uint64
	partial, reason := false, "none"
	var visit func(*os.Root, int) error
	visit = func(dir *os.Root, depth int) error {
		if err := w.ctx.Err(); err != nil {
			return err
		}
		if depth > storageChildDepth || w.remaining == 0 {
			return storagebudget.ErrBounds
		}
		before, err := dir.Stat(".")
		if err != nil {
			return err
		}
		key := identityKey(before)
		if key == "" {
			return storagebudget.ErrUnknown
		}
		if w.seen[key] {
			partial, reason = true, "shared"
			return nil
		}
		w.seen[key] = true
		w.remaining--
		dirs++
		file, err := dir.Open(".")
		if err != nil {
			return err
		}
		defer file.Close()
		for {
			if err := w.ctx.Err(); err != nil {
				return err
			}
			entries, readErr := file.ReadDir(32)
			for _, entry := range entries {
				if w.ctx.Err() != nil {
					return w.ctx.Err()
				}
				if w.remaining == 0 {
					return storagebudget.ErrBounds
				}
				w.remaining-- // Every attempted entry costs budget, including errors/aliases.
				w.names += len(entry.Name())
				if w.names > storageChildNameBytes {
					return storagebudget.ErrBounds
				}
				info, err := dir.Lstat(entry.Name())
				if err != nil {
					partial = true
					continue
				}
				if info.Mode()&os.ModeSymlink != 0 {
					partial = true
					continue
				}
				if info.IsDir() {
					pin, err := storagePinChild(dir, entry.Name(), false)
					if err != nil {
						partial = true
						continue
					}
					err = visit(pin.root, depth+1)
					current := pin.current()
					pin.root.Close()
					if err != nil {
						return err
					}
					if current != nil {
						return errStorageCustody
					}
				} else if info.Mode().IsRegular() {
					key := identityKey(info)
					if key == "" || info.Size() < 0 {
						partial = true
						continue
					}
					if w.seen[key] {
						partial, reason = true, "shared"
						continue
					}
					w.seen[key] = true
					if uint64(info.Size()) > 1<<60 || bytes > (1<<60)-uint64(info.Size()) {
						return storagebudget.ErrBounds
					}
					bytes += uint64(info.Size())
					files++
					if w.cut != nil {
						w.cut("file-counted", dir, entry.Name())
					}
					after, err := dir.Lstat(entry.Name())
					if err != nil || !os.SameFile(info, after) || info.Size() != after.Size() || !info.ModTime().Equal(after.ModTime()) {
						return errStorageCustody
					}
				} else {
					partial = true
				}
			}
			if readErr == io.EOF {
				break
			}
			if readErr != nil {
				return readErr
			}
		}
		if w.cut != nil {
			w.cut("directory-post", dir, "")
		}
		after, err := dir.Stat(".")
		if err != nil || !os.SameFile(before, after) || !before.ModTime().Equal(after.ModTime()) {
			return errStorageCustody
		}
		return nil
	}
	err = visit(root, 0)
	if err != nil {
		partial = true
		reason = "unknown"
		if errors.Is(err, storagebudget.ErrBounds) || errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled) {
			reason = "bounded"
		}
		if errors.Is(err, errStorageCustody) {
			reason = "changed"
		}
	}
	b, fcount, d := storageStatusUInt(bytes), storageStatusUInt(files), storageStatusUInt(dirs)
	// A detected change invalidates the aggregate, not merely the entry which
	// happened to reveal it. Prior positive counts need not remain lower bounds
	// after truncation/removal. Independently witnessed filesystem facts remain.
	if !errors.Is(err, errStorageCustody) && (dirs != 0 || files != 0 || err == nil) {
		row.LogicalBytes, row.Files, row.Directories = &b, &fcount, &d
	}
	row.State, row.Reason = "measured", reason
	if partial {
		row.State = "partial"
		if row.Reason == "none" {
			row.Reason = "unknown"
		}
	}
	return row
}
func (w *storageChildWalk) path(component, role, path string) storageChildRootStatus {
	row := storageChildUnknown(component, role)
	root, current, close, err := storageChildPinAbsolute(w.ctx, path)
	if err != nil {
		return row
	}
	defer close()
	row = w.root(component, role, root)
	if current() != nil {
		row = storageChildUnknown(component, role)
		row.Reason = "changed"
	}
	return row
}

// When a selected destination is inside an existing owner, walk from that
// held parent first. The absolute name must then join that exact description.
func (w *storageChildWalk) selectedPath(component, role, path string, parent *os.Root, parentPath string) storageChildRootStatus {
	relative, err := filepath.Rel(parentPath, path)
	if err != nil || parent == nil || relative != "." && !filepath.IsLocal(relative) {
		return w.path(component, role, path)
	}
	row := storageChildUnknown(component, role)
	parts := strings.Split(relative, string(filepath.Separator))
	if len(path) > 4096 || len(parts) > 64 {
		row.Reason = "bounded"
		return row
	}
	root, err := parent.OpenRoot(".")
	if err != nil {
		return row
	}
	roots := []*os.Root{root}
	pins := []storageNamedRoot{}
	defer func() {
		for i := len(roots) - 1; i >= 0; i-- {
			roots[i].Close()
		}
	}()
	for _, part := range parts {
		if w.ctx.Err() != nil {
			row.Reason = "bounded"
			return row
		}
		if part == "." {
			continue
		}
		pin, err := storagePinChild(roots[len(roots)-1], part, false)
		if err != nil {
			return row
		}
		roots = append(roots, pin.root)
		pins = append(pins, pin)
	}
	row = w.heldPath(component, role, roots[len(roots)-1], path)
	for _, pin := range pins {
		if pin.current() != nil {
			row = storageChildUnknown(component, role)
			row.Reason = "changed"
			break
		}
	}
	return row
}

// A retained owner descriptor is the authority. The name is only a witness:
// verify every ancestor and join the original description before observing it.
func (w *storageChildWalk) heldPath(component, role string, held *os.Root, path string) storageChildRootStatus {
	row := storageChildUnknown(component, role)
	named, current, close, err := storageChildPinAbsolute(w.ctx, path)
	if err != nil {
		return row
	}
	defer close()
	original, err := held.Stat(".")
	selected, se := named.Stat(".")
	if err != nil || se != nil || !os.SameFile(original, selected) {
		row.Reason = "changed"
		return row
	}
	row = w.root(component, role, held)
	if current() != nil {
		row = storageChildUnknown(component, role)
		row.Reason = "changed"
	}
	return row
}

// Scratch slots are bounded volatile borrows, not a catalog of temporary files.
// Overflow affects observation completeness only and never refuses a Git route.
func (s *Server) observeExchangeScratch(root *os.Root) func() {
	s.mu.Lock()
	slot := -1
	if !s.closing {
		for i := range s.childScratch {
			if s.childScratch[i] == nil {
				slot = i
				break
			}
		}
	}
	var held *os.Root
	if slot >= 0 && root != nil {
		held, _ = root.OpenRoot(".")
	}
	if held != nil {
		s.childScratch[slot] = held
	} else {
		s.childScratchOverflow++
	}
	s.mu.Unlock()
	var once sync.Once
	return func() {
		once.Do(func() {
			s.mu.Lock()
			if held != nil {
				s.childScratch[slot] = nil
				held.Close()
			} else if s.childScratchOverflow > 0 {
				s.childScratchOverflow--
			}
			s.mu.Unlock()
		})
	}
}
