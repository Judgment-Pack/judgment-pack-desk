package desk

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"time"
)

type homeRunnerRelocation struct {
	Version           int    `json:"version"`
	ID                string `json:"id"`
	Dir               string `json:"dir"`
	Workspace         string `json:"workspace"`
	Owner             string `json:"owner"`
	PreviousInputRoot string `json:"previousInputRoot"`
	InputRoot         string `json:"inputRoot"`
}

func runnerHomeCapability(binary string) error {
	if !filepath.IsAbs(binary) {
		return errors.New("Install a Runner with workspace relocation support before moving saved Jobs.")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	out := &cappedBuffer{limit: 4096}
	cmd := exec.CommandContext(ctx, binary, "relocate-workspace", "--capabilities")
	cmd.Stdout = out
	cmd.Stderr = &cappedBuffer{limit: 4096}
	cmd.WaitDelay = time.Second
	if err := cmd.Run(); err != nil {
		return errors.New("The installed Runner cannot relocate its saved workspace. Use the local Runner build with workspace relocation support before moving Desk home.")
	}
	var capability struct {
		Version int `json:"version"`
	}
	if decodeDataJSON(out.Bytes(), &capability) != nil || capability.Version != 1 {
		return errors.New("Runner workspace relocation support could not be confirmed.")
	}
	return nil
}
func (s *Server) homeRunnerRelocations(plan deskHomePlan) ([]homeRunnerRelocation, error) {
	rows := []homeRunnerRelocation{}
	for _, entry := range plan.Registry.Desks {
		oldProject := s.projectDir
		if s.home != nil {
			oldProject = filepath.Join(s.home.Path, entry.Folder)
		} else if entry.Kind == "desk" {
			oldProject = filepath.Join(s.configDir, "desks", entry.ID)
		}
		dir := filepath.Join(oldProject, ".desk-private", "jobs")
		if s.home == nil && entry.Kind == "project" {
			dir = filepath.Join(s.configDir, "jobs", entry.RunnerWorkspace)
		}
		if _, err := os.Lstat(filepath.Join(dir, "runs.sqlite")); errors.Is(err, os.ErrNotExist) {
			continue
		} else if err != nil {
			return nil, err
		}
		// IDs are assigned once when the confirmed journal is prepared. Preview
		// checks support, but does not spend IDs or modify the workspace.
		next := filepath.Join(plan.Destination, entry.Folder)
		rows = append(rows, homeRunnerRelocation{Version: 1, Dir: filepath.Join(next, ".desk-private", "jobs"), Workspace: entry.RunnerWorkspace, Owner: "local-owner:" + plan.Registry.Owner, PreviousInputRoot: oldProject, InputRoot: next})
	}
	if len(rows) > 0 {
		if err := runnerHomeCapability(s.cfg.RunnerBin); err != nil {
			return nil, err
		}
	}
	return rows, nil
}
func applyHomeRunnerRelocation(binary string, request homeRunnerRelocation) error {
	raw, err := json.Marshal(request)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, binary, "relocate-workspace")
	cmd.Stdin = bytes.NewReader(raw)
	out, said := &cappedBuffer{limit: 8192}, &cappedBuffer{limit: 8192}
	cmd.Stdout = out
	cmd.Stderr = said
	cmd.WaitDelay = time.Second
	if err = cmd.Run(); err != nil {
		return errors.New("Runner refused the reviewed workspace relocation. The migration journal was retained; restart with a compatible Runner to recover.")
	}
	var result struct {
		Version        int    `json:"version"`
		ID             string `json:"id"`
		InputRoot      string `json:"inputRoot"`
		AlreadyApplied bool   `json:"alreadyApplied"`
	}
	if decodeDataJSON(out.Bytes(), &result) != nil || result.Version != 1 || result.ID != request.ID || result.InputRoot != request.InputRoot {
		return errors.New("Runner's relocation result could not be confirmed. Recovery will check the same operation ID.")
	}
	return nil
}

func homeBinaryDigest(path string) (string, error) {
	info, err := os.Lstat(path)
	if err != nil {
		return "", err
	}
	if !info.Mode().IsRegular() || info.Mode().Perm()&0022 != 0 {
		return "", errors.New("Runner executable is not a trusted regular file.")
	}
	f, err := os.OpenFile(path, os.O_RDONLY|openNoFollow|openNonBlocking, 0)
	if err != nil {
		return "", err
	}
	defer f.Close()
	held, err := f.Stat()
	if err != nil || !os.SameFile(info, held) {
		return "", errors.New("Runner executable changed.")
	}
	hash := sha256.New()
	if _, err = io.Copy(hash, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(hash.Sum(nil)), nil
}
