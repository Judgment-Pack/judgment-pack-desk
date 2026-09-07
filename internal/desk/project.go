package desk

// The project this desk serves: validated and pinned through **one held
// descriptor**, never through a pathname resolved twice.

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sync"
)

// ProjectRoot is a project directory that has been validated and pinned.
//
// # Why this type exists rather than a string
//
// Round 2 found the launch and the server each resolving a pathname of their
// own: `usableProjectDir` validated the configured `jpack-desk.json`, returned
// its **parent as a pathname**, and `New` then called `EvalSymlinks` on that
// pathname again before `os.OpenRoot`. Between the two, a principal who can
// rename inside the parent could move the validated directory away and install
// a symlink to another tree — and the desk would pin and serve the
// replacement, having validated the original. There was a second, smaller
// window inside `New` between its own `EvalSymlinks` and its `OpenRoot`.
//
// The repair is the pattern `custody.go` already holds the credential
// directory to: **validate once, pin once, and let the descriptor be the
// authority afterwards**. Everything that decides is done here — the
// resolution, the `Lstat`, the `OpenRoot`, and the `SameFile` that says the
// descriptor is the directory that was inspected — and what leaves this
// function is the descriptor, not a name for one.
type ProjectRoot struct {
	// dir is the resolved pathname. It is still needed — the runtime
	// subprocess takes a working directory and the watcher takes a tree, and
	// neither can hold a descriptor — but nothing *decides* by it.
	dir string
	// own is the descriptors and who owns them, **shared by every copy of this
	// value**. See `ownership`.
	own *ownership
	// info is the identity that was validated, kept so a caller that validated
	// something about this directory earlier can prove it is the same one.
	info fs.FileInfo
}

// ownership is the two descriptors, and the one fact about who closes them.
//
// # Why it is a pointer and not two fields
//
// `ProjectRoot` is an exported struct, so a caller can copy one:
// `alias := *pinned` before handing `pinned` over. Round 4 found what that
// cost while ownership was a `bool` *inside* the struct — the copy kept the
// same `*os.Root` and the same `*os.File` with `adopted` still false, so
// `alias.Close()` closed the descriptors of a running server. A flag that
// copies is not ownership; it is a note about one copy.
//
// So the flag and the descriptors live in one heap cell that every copy points
// at. A copy made before the hand-over sees the detach, because there is only
// one thing to see.
//
// # Why the close is a `sync.Once`
//
// Closing a descriptor twice is closing whatever took its number the second
// time. The server closes on shutdown, a caller may close on the way out, and
// `New`'s own failure path closes what it did not adopt; each of those is
// correct on its own and the arithmetic between them is not something a reader
// should have to do. One cell, one close, whoever asks first.
type ownership struct {
	root    *os.Root
	dirFile *os.File

	mu sync.Mutex
	// adopted is set once a server has taken these descriptors over, and is
	// never unset: a wrapper that was handed over stays handed over, before
	// and after that server shuts down.
	adopted bool

	once sync.Once
	// closes counts how many times the descriptors were actually released, so
	// a test can hold "exactly once" rather than infer it.
	closes int
	err    error
}

// close releases the descriptors, at most once however many callers ask.
func (o *ownership) close() error {
	if o == nil {
		return nil
	}
	o.once.Do(func() {
		o.closes++
		if o.dirFile != nil {
			o.err = o.dirFile.Close()
		}
		if o.root != nil {
			if err := o.root.Close(); o.err == nil {
				o.err = err
			}
		}
	})
	return o.err
}

// Dir is the resolved pathname of the pinned directory.
func (p *ProjectRoot) Dir() string { return p.dir }

// Close releases the descriptors, unless a server has adopted them.
//
// **A handed-over root is no longer the caller's to close**, and round 3 found
// what that cost while it was only documented: the wrapper stayed live, its
// `Close` closed the very `os.Root` the running server holds, and the file API
// went dead while the runtime kept working. So adoption *detaches* — the
// caller's wrapper stops owning anything and says so — rather than asking
// every caller to remember.
func (p *ProjectRoot) Close() error {
	if p == nil || p.own == nil {
		return nil
	}
	p.own.mu.Lock()
	adopted := p.own.adopted
	p.own.mu.Unlock()
	if adopted {
		return errAdoptedByServer
	}
	return p.own.close()
}

// errAdoptedByServer is what a caller gets for closing a root it handed over.
var errAdoptedByServer = errors.New(
	"this project root was adopted by a server, which closes it; nothing was closed here")

// detach marks these descriptors as a server's, for every wrapper over them.
//
// Called by `New` at the instant it succeeds, so there is exactly one owner
// from then on — and it is set on the shared cell, so a copy a caller made
// **before** handing the original over sees it too.
func (p *ProjectRoot) detach() {
	if p == nil || p.own == nil {
		return
	}
	p.own.mu.Lock()
	p.own.adopted = true
	p.own.mu.Unlock()
}

// OpenProjectRoot validates a directory and pins it in one operation.
//
// **The `SameFile` is the point.** `os.OpenRoot` takes a name, and a name can
// be pointed somewhere else between the `Lstat` that inspected it and the open
// that acts on it. Comparing the identity of what was inspected against the
// identity of what the descriptor actually holds is what closes that window —
// the same comparison `readConfigFile` makes about the desk-level file, and
// for the same reason.
func OpenProjectRoot(dir string) (*ProjectRoot, error) {
	absolute, err := filepath.Abs(dir)
	if err != nil {
		return nil, fmt.Errorf("resolving %q: %w", dir, err)
	}
	// Resolved once, here, and never again anywhere else: this is the pathname
	// the runtime and the watcher are given.
	resolved, err := filepath.EvalSymlinks(absolute)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", absolute, err)
	}
	inspected, err := os.Lstat(resolved)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", resolved, err)
	}
	if !inspected.IsDir() {
		return nil, fmt.Errorf("%s is not a directory", resolved)
	}
	// The residual the kind check cannot close: a swap performed *after* it.
	// It is what proves the comparison below is doing work rather than
	// restating an `Lstat` that already refused the ordinary case — the same
	// instrument, and the same argument, as `afterConfigStat`.
	afterInspectingProject(resolved)
	root, err := os.OpenRoot(resolved)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", resolved, err)
	}
	held, err := root.Stat(".")
	if err != nil {
		root.Close()
		return nil, fmt.Errorf("%s: %w", resolved, err)
	}
	if !os.SameFile(inspected, held) {
		root.Close()
		return nil, fmt.Errorf(
			"%s changed between being inspected and being opened, and was not served", resolved)
	}
	// **Through the root, not by name.** This is the same directory as an
	// ordinary descriptor, for the two consumers that cannot go through
	// `os.Root` at all — a subprocess's working directory and an inotify
	// watch. Opening it by pathname here would reintroduce the resolution this
	// whole type exists to remove.
	dirFile, err := root.Open(".")
	if err != nil {
		root.Close()
		return nil, fmt.Errorf("%s: %w", resolved, err)
	}
	return &ProjectRoot{
		dir:  resolved,
		own:  &ownership{root: root, dirFile: dirFile},
		info: held,
	}, nil
}

// testHookAfterInspectingProject runs between establishing what a directory is
// and opening it, and is nil outside tests.
//
// It exists so a test can perform exactly the swap a time-of-check /
// time-of-use attack would, at the instant where it would matter. Without it
// the `SameFile` below would rest on reading the code and believing it.
var testHookAfterInspectingProject func(path string)

func afterInspectingProject(path string) {
	if testHookAfterInspectingProject != nil {
		testHookAfterInspectingProject(path)
	}
}

// testHookBeforePinningProject runs between the launch deciding on a directory
// and pinning it, and is nil outside tests.
//
// It exists so a test can perform exactly the swap round 2 described, at the
// instant where it would matter. Without it the argument above would rest on
// reading the code and believing it — which is the same reason
// `testHookAfterCustodyCheck` exists.
var testHookBeforePinningProject func(dir string)

func beforePinningProject(dir string) {
	if testHookBeforePinningProject != nil {
		testHookBeforePinningProject(dir)
	}
}

// OpenProject is the whole launch: which project, and the descriptor for it.
//
// It is what `main` calls, and it is the only shape that is safe to call:
// `ResolveProjectDir` answers *which directory* and hands back a name, and a
// name is precisely what must not be re-resolved between validation and
// pinning. So the identity of everything the decision inspected travels with
// the decision, and is compared against the descriptor that is actually held.
func OpenProject(argument, configDir string) (*ProjectRoot, error) {
	chosen, err := chooseProject(argument, configDir)
	if err != nil {
		return nil, err
	}
	beforePinningProject(chosen.dir)
	pinned, err := OpenProjectRoot(chosen.dir)
	if err != nil {
		return nil, fmt.Errorf("project directory: %w", err)
	}
	if err := chosen.stillTheOneValidated(pinned); err != nil {
		pinned.Close()
		return nil, err
	}
	return pinned, nil
}

// projectChoice is the launch's decision, with the identities it decided by.
//
// `dirInfo` and `fileInfo` are nil where nothing was validated — an argument,
// or the current directory, neither of which this desk inspects before opening
// — and set where a configured default chose the directory, because that is
// the case in which something *was* checked and the check has to still be
// about the thing that ends up being served.
type projectChoice struct {
	dir      string
	dirInfo  fs.FileInfo
	fileInfo fs.FileInfo
}

// stillTheOneValidated compares what was validated against what is pinned.
//
// Both halves, and each is a different swap: the **directory** may have been
// renamed away and replaced with a link to another tree, and the **file** that
// chose it may have been replaced inside a directory that is still the same
// one. Where nothing was validated there is nothing to compare, and that is
// not a gap: an argument names a directory this desk was told to serve, and
// `OpenProjectRoot` has already established that the descriptor is the
// directory it inspected.
func (c projectChoice) stillTheOneValidated(pinned *ProjectRoot) error {
	if c.dirInfo == nil {
		return nil
	}
	if !os.SameFile(c.dirInfo, pinned.info) {
		return errProjectMoved
	}
	// Through the pinned descriptor, so the name cannot be redirected out from
	// under the check: this is the file whose existence and kind chose this
	// directory, and it has to still be that file.
	held, err := pinned.own.root.Lstat(projectConfigName)
	if err != nil {
		return fmt.Errorf("%s in the project that was validated: %w", projectConfigName, err)
	}
	if !held.Mode().IsRegular() || !os.SameFile(c.fileInfo, held) {
		return errProjectMoved
	}
	return nil
}

// errProjectMoved is the one refusal both identity checks make.
var errProjectMoved = errors.New(
	"the project directory changed between being validated and being opened, and was not served")
