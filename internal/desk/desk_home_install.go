package desk

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// The managed install is an owned installation with its own updater contract,
// not a directory to guess from an arbitrary browser-supplied path.
func homeManagedInstall(config string) (string, error) {
	path := os.Getenv("JPACK_DESK_INSTALL_ROOT")
	if path == "" {
		if filepath.Base(config) == deskHomePlatform {
			path = filepath.Join(config, "install")
		} else {
			path = filepath.Join(filepath.Dir(deskApplicationDataPath()), "jpack-desk-install")
		}
	}
	if _, err := os.Lstat(path); errors.Is(err, os.ErrNotExist) {
		return "", nil
	} else if err != nil {
		return "", err
	}
	if !validProjectHistoryPath(path) {
		return "", errors.New("The managed installation path is invalid.")
	}
	root, err := openPrivateDataRoot(path, false)
	if err != nil {
		return "", err
	}
	defer root.Close()
	raw, err := readPrivateData(root, "installation.json", 1<<20)
	if err != nil {
		return "", err
	}
	var marker struct {
		SchemaVersion int `json:"schemaVersion"`
	}
	if json.Unmarshal(raw, &marker) != nil || marker.SchemaVersion != 1 {
		return "", errors.New("The managed installation marker is invalid.")
	}
	return path, nil
}
func managedInstallHomeReady(path string) error {
	if _, err := os.Lstat(filepath.Join(path, "local-launch.json")); err == nil {
		return localInstallHomeReady(path)
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	root, err := openPrivateDataRoot(path, false)
	if err != nil {
		return err
	}
	defer root.Close()
	raw, err := readPrivateData(root, "installation.json", 1<<20)
	if err != nil {
		return err
	}
	var marker struct {
		Current *struct {
			Version string `json:"version"`
		} `json:"current"`
	}
	if json.Unmarshal(raw, &marker) != nil || marker.Current == nil || !stableReleaseTag.MatchString("v"+strings.TrimPrefix(marker.Current.Version, "v")) {
		return errors.New("The managed installation has no current release to check.")
	}
	binary := filepath.Join(path, "releases", marker.Current.Version, "jpack-desk")
	info, err := os.Lstat(binary)
	if err != nil || !info.Mode().IsRegular() || info.Mode().Perm()&0022 != 0 || ownedByUs("installed Desk", info) != nil {
		return errors.New("The installed Desk executable could not be checked.")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	out := &cappedBuffer{limit: 256}
	command := exec.CommandContext(ctx, binary, "--desk-home-layout-version")
	command.Stdout = out
	command.Stderr = &cappedBuffer{limit: 256}
	if err = command.Run(); err != nil || strings.TrimSpace(string(out.Bytes())) != "1" {
		return errors.New("Update the managed Desk installation to a release with Desk home support before moving its releases. The current local build can preview the move.")
	}
	return nil
}
func quoteHomeShell(path string) string { return "'" + strings.ReplaceAll(path, "'", "'\"'\"'") + "'" }
func migratedLauncher(raw []byte, previous, next string) ([]byte, error) {
	text := string(raw)
	// Preserve the owner's environment choices. Only recognize the existing
	// simple launcher contract; custom scripts need their own reviewed update.
	script := "desk-update.py"
	if _, err := os.Lstat(filepath.Join(previous, "local-launch.json")); err == nil {
		script = "desk-local.py"
	} else if !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}
	oldScript := filepath.Join(previous, script)
	prefix := "exec /usr/bin/python3 "
	candidates := []string{prefix + oldScript + ` run -- "$@"`, prefix + quoteHomeShell(oldScript) + ` --root ` + quoteHomeShell(previous) + ` run -- "$@"`}
	for _, line := range strings.Split(text, "\n") {
		for _, expected := range candidates {
			if line == expected {
				replacement := prefix + quoteHomeShell(filepath.Join(next, script)) + " --root " + quoteHomeShell(next) + ` run -- "$@"`
				return []byte(strings.Replace(text, line, replacement, 1)), nil
			}
		}
	}
	return nil, errors.New("The custom Desk launcher needs a reviewed location update before moving its installation.")
}

func readHomeLauncher(path string) ([]byte, error) {
	parent, err := OpenProjectRoot(filepath.Dir(path))
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	defer parent.Close()
	info, err := parent.own.root.Lstat(filepath.Base(path))
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() || info.Mode().Perm()&0022 != 0 || ownedByUs("Desk launcher", info) != nil {
		return nil, errors.New("Desk launcher is not an owned regular file.")
	}
	f, err := parent.own.root.OpenFile(filepath.Base(path), os.O_RDONLY|openNoFollow|openNonBlocking, 0)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	raw, err := io.ReadAll(io.LimitReader(f, 16385))
	if err != nil || len(raw) > 16384 {
		return nil, errors.New("Desk launcher could not be read safely.")
	}
	return raw, nil
}

// Install the bridge before any rename, then replace it with the final launcher
// after metadata publication. An interrupted move can still start recovery.
func homeLauncherBridge(raw []byte, previous, next string) ([]byte, error) {
	after, err := migratedLauncher(raw, previous, next)
	if err != nil {
		return nil, err
	}
	oldLines, newLines := strings.Split(string(raw), "\n"), strings.Split(string(after), "\n")
	for index, old := range oldLines {
		if old != newLines[index] {
			script := "desk-update.py"
			if strings.Contains(newLines[index], "/desk-local.py") {
				script = "desk-local.py"
			}
			oldLines[index] = "if [ -f " + quoteHomeShell(filepath.Join(next, script)) + " ]; then\n  " + newLines[index] + "\nelse\n  " + old + "\nfi"
			return []byte(strings.Join(oldLines, "\n")), nil
		}
	}
	return nil, errors.New("Desk launcher locations are unchanged.")
}

// A selected local build has its own launcher and digest-checked bundle. Never
// fall back to an older managed release if that selection is invalid.
func localInstallHomeReady(path string) error {
	raw, err := readHomeMetadata(filepath.Join(path, "local-launch.json"), 8192)
	if err != nil {
		return err
	}
	var marker struct {
		Version  int    `json:"schemaVersion"`
		Digest   string `json:"launcherDigest"`
		Manifest string `json:"manifestDigest"`
	}
	if decodeDataJSON(raw, &marker) != nil || marker.Version != 1 || len(marker.Manifest) != 64 || len(marker.Digest) != 64 {
		return errors.New("The selected local Desk build is invalid. Reinstall that local build before moving.")
	}
	script := filepath.Join(path, "desk-local.py")
	code, err := readHomeLauncher(script)
	if err != nil || code == nil || digestOf(code) != marker.Digest {
		return errors.New("The selected local Desk launcher changed. Reinstall that local build before moving.")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	command := exec.CommandContext(ctx, "/usr/bin/python3", script, "--root", path, "check")
	out := &cappedBuffer{limit: 256}
	command.Stdout, command.Stderr = out, &cappedBuffer{limit: 256}
	if command.Run() != nil || strings.TrimSpace(string(out.Bytes())) != "1" {
		return errors.New("The selected local Desk bundle could not be verified for this move. Reinstall that local build before moving.")
	}
	return nil
}
