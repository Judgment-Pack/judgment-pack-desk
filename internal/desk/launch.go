package desk

// Which project this desk opens, decided before there is a server.

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

// ResolveProjectDir answers which project directory `jpack-desk` opens.
//
// # Three answers, and the order between them
//
//   - **An argument wins, always.** `jpack-desk /some/project` opens that
//     project whatever any file says, because the command line is the more
//     specific statement and a configured default that could override one
//     would be a desk nobody can point somewhere else.
//   - **No argument, and the desk-level file names a usable `project.file`**:
//     the directory that file is in. That member is the whole point of this
//     function — a desk launched from a menu entry, a shortcut or a service
//     unit has no useful working directory.
//   - **Neither**: the current directory, exactly as it always was.
//
// # A configured default is validated on **this host**, or refused
//
// The shared decoder's rule is lexical, because it is shared: it has to give
// the same answer in a browser, where there is no filesystem to ask. That
// makes it a rule about *spelling* — and a spelling that is absolute on one
// platform is a relative path on another. `C:\p\jpack-desk.json` is one path
// component to `path/filepath` on Unix, so `filepath.Dir` answers `"."` and
// the desk would silently open the directory it happened to be launched from,
// which is the accident this member exists to remove.
//
// So a configured default is checked against the host that is about to act on
// it: absolute here, resolving through its symlinks to a regular file of the
// name this desk reads, in a directory that is not the filesystem root. A
// default that fails any of those **refuses the launch by name** — it is never
// a silent fall back to the current directory, because a person who configured
// a default and got some other project would have no way to see that the
// member they wrote was ignored.
func ResolveProjectDir(argument, configDir string) (string, error) {
	if argument != "" {
		return argument, nil
	}
	file, err := configuredProjectFile(configDir)
	if err != nil {
		return "", err
	}
	return resolveProjectDir(argument, file)
}

// deskLaunchFile is the desk-level file as the launch path sees it: where it
// is, and the project file it names.
type deskLaunchFile struct {
	// path is the desk-level file, absolute, or empty where this machine has
	// no configuration directory to hold one.
	path string
	// file is `project.file`, or empty where the file names none, is absent,
	// or was refused.
	file string
}

// resolveProjectDir is the decision itself, with the file already read.
//
// Split from the read so that the rule can be tested without a filesystem, and
// so that there is exactly one place the three answers are chosen between.
func resolveProjectDir(argument string, deskFile deskLaunchFile) (string, error) {
	if argument != "" {
		return argument, nil
	}
	if deskFile.file == "" {
		// The default this desk has always had, and the state most desks are
		// in: nobody configured one, and the current directory is what
		// `jpack-desk` in a project means.
		return ".", nil
	}
	dir, err := usableProjectDir(deskFile.file)
	if err != nil {
		where := deskFile.path
		if where == "" {
			where = "this machine's desk configuration file"
		}
		return "", fmt.Errorf("project.file in %s names %q, which this desk cannot open: %w",
			where, deskFile.file, err)
	}
	return dir, nil
}

// usableProjectDir is the host's own verdict on a configured `project.file`.
//
// Five checks, and each one is a way the lexical rule can be true while the
// path is not one this host can act on:
//
//   - **Absolute here.** `filepath.IsAbs` and not the shared spelling rule:
//     a Windows-shaped path is a *relative* path on Unix, and resolving it
//     would land on the launch directory by accident.
//   - **It resolves.** `EvalSymlinks` refuses a path that is not there, which
//     is the state a stale default is usually in — and it is what makes the
//     next two checks about the file that would actually be opened rather than
//     about the name that was written.
//   - **A regular file.** A directory, a socket or a device named here is not
//     a configuration file, and opening its parent would be this desk taking a
//     project from a coincidence.
//   - **Named `jpack-desk.json`** *after* resolution, so a link cannot point
//     the name this desk reads at something else.
//   - **Not in the filesystem root.** `/jpack-desk.json` would pin `/` as the
//     project, and the file API would then serve the host. Nothing legitimate
//     puts a project there, and a rule that admitted it would make every other
//     containment argument on this desk conditional on nobody writing it.
func usableProjectDir(file string) (string, error) {
	if !filepath.IsAbs(file) {
		return "", fmt.Errorf(
			"it is not an absolute path on this system (a path written for another platform " +
				"is a relative one here, and would open whatever directory this desk was " +
				"launched from)")
	}
	// **The root is refused before anything is asked of the filesystem**, so
	// that `/jpack-desk.json` is refused for being where it is rather than for
	// happening not to exist — the refusal a reader has to act on is the one
	// about the location, and a file that appeared there later would otherwise
	// be admitted.
	if isFilesystemRoot(filepath.Dir(file)) {
		return "", errProjectInRoot
	}
	resolved, err := filepath.EvalSymlinks(file)
	if err != nil {
		return "", fmt.Errorf("it could not be resolved: %w", err)
	}
	info, err := os.Lstat(resolved)
	if err != nil {
		return "", fmt.Errorf("it could not be read: %w", err)
	}
	if !info.Mode().IsRegular() {
		return "", fmt.Errorf("it is not a regular file")
	}
	if filepath.Base(resolved) != projectConfigName {
		return "", fmt.Errorf("it resolves to %s rather than a %s",
			filepath.Base(resolved), projectConfigName)
	}
	// And again on what it resolved to, because a link can point out of a
	// directory that passed the check above and into the root.
	dir := filepath.Dir(resolved)
	if isFilesystemRoot(dir) {
		return "", errProjectInRoot
	}
	return dir, nil
}

// errProjectInRoot is the one refusal stated in two places, so that breaking
// either leaves the other saying the same sentence and a row can tell.
var errProjectInRoot = errors.New(
	"it is in the filesystem root, and this desk will not serve a project rooted there")

// isFilesystemRoot reports whether a directory is its own parent.
func isFilesystemRoot(dir string) bool { return dir == filepath.Dir(dir) }

// configuredProjectFile reads the desk-level file and returns what it names.
//
// **A file that is refused names nothing**, and that is the same rule the probe
// is held to: any problem refuses the whole file, so a `project.file` sitting
// beside an unknown key does not choose a project. The refusal travels, because
// a desk that silently ignored the member and then opened the current directory
// would send a reader to look at a member that is already there.
func configuredProjectFile(configDir string) (deskLaunchFile, error) {
	store := openAssistantStore(configDir)
	defer store.Close()
	found := deskLaunchFile{}
	if !store.usable() {
		// Not fatal on its own: a desk given a directory on the command line
		// never asks this, and one that was not falls through to the current
		// directory, which is where it would have been anyway.
		return found, nil
	}
	found.path = filepath.Join(configDir, deskConfigName)
	present, data, err := store.readConfigFile()
	if err != nil {
		return found, fmt.Errorf("%s could not be read: %w", found.path, err)
	}
	if !present {
		return found, nil
	}
	decoded := decodeDeskFile(data)
	if decoded.refused() {
		return found, fmt.Errorf(
			"%s is not a configuration this desk reads, so it names no project: %s",
			found.path, describeProblems(decoded.Problems))
	}
	found.file = decoded.ProjectFile
	return found, nil
}

// DeskConfigDirFor is `configDirFor` for a caller outside this package.
//
// `main` needs the directory before it builds a server, because the file in it
// is what decides which project that server is built for.
func DeskConfigDirFor(explicit string) string { return configDirFor(explicit) }
