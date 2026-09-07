package desk

// Which project this desk opens, decided before there is a server.

import (
	"fmt"
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
//   - **No argument, and the desk-level file names a `project.file`**: the
//     directory that file is in. That member is the whole point of this
//     function — a desk launched from a menu entry, a shortcut or a service
//     unit has no useful working directory, and picking one out of the
//     environment is how a desk comes to be open on a project nobody chose.
//   - **Neither**: a usage error naming the file and the member, rather than
//     the current directory. Opening whatever directory the process happened
//     to start in is a project chosen by an accident of how it was launched,
//     and every consequence of that choice — which files the runtime reads,
//     which tree the watcher watches, where a pack is written — is silent.
//
// `configDir` is this machine's desk-level directory; empty means there is
// none to read, which is reported as itself rather than as an absent member.
// The read goes through the **same custody-validated store** every other read
// of that file goes through: it names a directory this desk will then serve
// out of, and a file anybody else could have written must not choose it.
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
//
// A struct rather than two strings so that the "neither" error can name the
// path a reader has to write to, in the state where there is no file there at
// all — which is the state most people meet this error in.
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
	if deskFile.file != "" {
		// The directory the configuration file is in. The member names the
		// file rather than the directory because that is the thing a person
		// can point at and check: a directory with no `jpack-desk.json` in it
		// is a project this desk has nothing configured for.
		return filepath.Dir(deskFile.file), nil
	}
	where := deskFile.path
	if where == "" {
		where = "this machine's desk configuration file"
	}
	return "", fmt.Errorf(
		"no project directory: pass one on the command line, or set project.file in %s to the "+
			"absolute path of the %s this desk should open",
		where, projectConfigName)
}

// configuredProjectFile reads the desk-level file and returns what it names.
//
// **A file that is refused names nothing**, and that is the same rule the probe
// is held to: any problem refuses the whole file, so a `project.file` sitting
// beside an unknown key does not choose a project. The refusal travels, because
// a desk that silently ignored the member and then reported "no project
// directory" would send a reader to look at the member that is already there.
func configuredProjectFile(configDir string) (deskLaunchFile, error) {
	store := openAssistantStore(configDir)
	defer store.Close()
	found := deskLaunchFile{}
	if !store.usable() {
		// Not fatal on its own: a desk given a directory on the command line
		// never asks this, and one that was not gets the usage error below
		// with the reason attached.
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
