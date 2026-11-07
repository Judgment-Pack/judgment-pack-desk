package desk

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
)

// The immutable companion is the sole current native location. The immutable
// v1 row is historical provenance; none of its path bytes are opened here.
const dialectArtifactLocationRecordBytes = 5907 // absolute 5835, work 5907, home 603

type dialectArtifactLocation struct {
	Kind      string `json:"kind"`
	Anchor    string `json:"anchor,omitempty"`
	PathBytes []byte `json:"pathBytes"`
}

type dialectArtifactLocationRecord struct {
	Version        int                     `json:"version"`
	Owner          string                  `json:"owner"`
	Kind           string                  `json:"kind"`
	LegacySHA256   string                  `json:"legacySHA256"`
	FolderIdentity string                  `json:"folderIdentity"`
	Location       dialectArtifactLocation `json:"location"`
	WorkDomain     string                  `json:"workDomain"`
}

func dialectArtifactRelativePath(raw []byte) bool {
	path := string(raw)
	return len(raw) > 0 && len(raw) <= dialectArtifactOwnerPathBytes && bytes.IndexByte(raw, 0) < 0 &&
		filepath.IsLocal(path) && filepath.Clean(path) == path
}

func (v dialectArtifactLocationRecord) valid() bool {
	if v.Version != 2 || !assistantRunDigest(v.Owner) || v.Kind != "file" || !assistantRunDigest(v.LegacySHA256) ||
		!dialectArtifactOwnerFileIdentity(v.FolderIdentity) || !assistantRunDigest(v.WorkDomain) {
		return false
	}
	switch v.Location.Kind {
	case "absolute":
		return v.Location.Anchor == "" && dialectArtifactOwnerPath(v.Location.PathBytes)
	case "work":
		return v.Location.Anchor == v.WorkDomain && dialectArtifactRelativePath(v.Location.PathBytes)
	case "home":
		return assistantRunDigest(v.Location.Anchor) && validHomeFolder(string(v.Location.PathBytes))
	}
	return false
}

func readDialectArtifactLocationRecord(raw []byte) (dialectArtifactLocationRecord, error) {
	var v dialectArtifactLocationRecord
	err := readDialectArtifactOwnerJSON(raw, dialectArtifactLocationRecordBytes, &v)
	if err == nil && !v.valid() {
		err = errDialectArtifactOwner
	}
	return v, err
}

// Ordinary native project edges need no new permissions restriction. This is
// deliberately separate from the private metadata directory opener.
func (s *Server) openDialectArtifactRelativeProject(ctx context.Context, path string) (_ *os.Root, err error) {
	if !dialectArtifactRelativePath([]byte(path)) {
		return nil, errDialectArtifactOwner
	}
	current, err := s.assistant.root.OpenRoot(".")
	if err != nil {
		return nil, err
	}
	defer func() {
		if err != nil {
			err = errors.Join(err, closeDialectArtifactOwner(ctx, "location-path", current.Close))
		}
	}()
	if path != "." {
		for _, part := range strings.Split(path, string(filepath.Separator)) {
			entry, lookErr := current.Lstat(part)
			if lookErr != nil || !entry.IsDir() || entry.Mode()&os.ModeSymlink != 0 {
				return nil, errors.Join(errDialectArtifactOwner, lookErr)
			}
			next, openErr := current.OpenRoot(part)
			if openErr != nil {
				return nil, openErr
			}
			held, statErr := next.Stat(".")
			again, againErr := current.Lstat(part)
			if statErr != nil || againErr != nil || !os.SameFile(entry, held) || !os.SameFile(held, again) || again.Mode()&os.ModeSymlink != 0 {
				return nil, errors.Join(errDialectArtifactOwner, statErr, againErr, closeDialectArtifactOwner(ctx, "location-path", next.Close))
			}
			prior := current
			current = next
			if closeErr := closeDialectArtifactOwner(ctx, "location-path", prior.Close); closeErr != nil {
				return nil, closeErr
			}
		}
	}
	held, err := current.Stat(".")
	if err != nil || !os.SameFile(held, s.project.info) {
		return nil, errors.Join(errDialectArtifactOwner, err)
	}
	return current, nil
}

func (s *Server) dialectArtifactActualLocation(ctx context.Context, work string, home *dialectArtifactHomeHold) (dialectArtifactLocation, error) {
	if home != nil {
		return dialectArtifactLocation{Kind: "home", Anchor: home.record.ID, PathBytes: []byte(home.entry.Folder)}, home.check()
	}
	rel, err := filepath.Rel(s.configDir, s.projectDir)
	if err != nil {
		return dialectArtifactLocation{}, err
	}
	if !filepath.IsLocal(rel) {
		if !sameDirectory(s.projectDir, s.project.info) {
			return dialectArtifactLocation{}, errDialectArtifactOwner
		}
		return dialectArtifactLocation{Kind: "absolute", PathBytes: []byte(s.projectDir)}, nil
	}
	root, err := s.openDialectArtifactRelativeProject(ctx, rel)
	if err != nil {
		return dialectArtifactLocation{}, err // a contained failure is never an absolute fallback
	}
	if err = closeDialectArtifactOwner(ctx, "location-path", root.Close); err != nil {
		return dialectArtifactLocation{}, err
	}
	return dialectArtifactLocation{Kind: "work", Anchor: work, PathBytes: []byte(rel)}, nil
}

// Genuine absence alone permits an initial v1 transition. A present companion
// always selects v2, including on every decoding, custody or binding failure.
// The successful absence decision is retained through publication; present v2
// can never become permission to create after a later disappearance.
func preflightDialectArtifactLocation(ctx context.Context, metadata *os.Root, owner string, expected, legacy, currentLegacy []byte) (create bool, err error) {
	absent := func() (bool, error) {
		if bytes.Equal(legacy, currentLegacy) {
			return true, nil
		}
		return false, errDialectArtifactOwner
	}
	// Inspect each selected edge before opening it. ENOENT after an observed
	// edge is a stale read failure, never permission to transition from v1.
	tableInfo, err := metadata.Lstat("owner-locations")
	if errors.Is(err, os.ErrNotExist) {
		return absent()
	}
	if err != nil {
		return false, err
	}
	table, err := openDialectArtifactOwnerFolder(ctx, metadata, false, "location-folder", "owner-locations")
	if err != nil {
		return false, err
	}
	defer func() { err = errors.Join(err, closeDialectArtifactOwner(ctx, "location-preflight", table.Close)) }()
	held, err := table.Stat(".")
	if err != nil || !os.SameFile(tableInfo, held) {
		return false, errors.Join(errDialectArtifactOwner, err)
	}
	shardInfo, err := table.Lstat(owner[:2])
	if errors.Is(err, os.ErrNotExist) {
		return absent()
	}
	if err != nil {
		return false, err
	}
	rows, err := openDialectArtifactOwnerFolder(ctx, table, false, "location-folder", owner[:2])
	if err != nil {
		return false, err
	}
	defer func() { err = errors.Join(err, closeDialectArtifactOwner(ctx, "location-preflight", rows.Close)) }()
	held, err = rows.Stat(".")
	if err != nil || !os.SameFile(shardInfo, held) {
		return false, errors.Join(errDialectArtifactOwner, err)
	}
	name := owner[2:] + ".json"
	entry, err := rows.Lstat(name)
	if errors.Is(err, os.ErrNotExist) {
		return absent()
	}
	if err != nil {
		return false, err
	}
	if err = dialectArtifactOwnerStep(ctx, "location.preflight-open"); err != nil {
		return false, err
	}
	file, err := openDialectArtifactOwnerFile(rows, name)
	if err != nil {
		return false, err
	}
	defer func() { err = errors.Join(err, closeDialectArtifactOwner(ctx, "location-preflight-file", file.Close)) }()
	held, err = file.Stat()
	if err != nil || !os.SameFile(entry, held) {
		return false, errors.Join(errDialectArtifactOwner, err)
	}
	raw, err := readBounded(file, dialectArtifactLocationRecordBytes)
	if err != nil {
		return false, err
	}
	if _, err = readDialectArtifactLocationRecord(raw); err != nil || !bytes.Equal(raw, expected) {
		return false, errors.Join(errDialectArtifactOwner, err)
	}
	return false, nil
}

func publishDialectArtifactLocation(ctx context.Context, metadata *os.Root, raw []byte, create bool, check func() error) (err error) {
	v, err := readDialectArtifactLocationRecord(raw)
	if err != nil {
		return err
	}
	rows, err := openDialectArtifactOwnerFolder(ctx, metadata, create, "location-folder", "owner-locations", v.Owner[:2])
	if err != nil {
		return err
	}
	defer func() { err = errors.Join(err, closeDialectArtifactOwner(ctx, "location-folder", rows.Close)) }()
	name := v.Owner[2:] + ".json"
	if !create {
		// A selected existing v2 may only be re-acknowledged. Neither a
		// missing branch nor a missing file can enter the claim/create path.
		if err = check(); err != nil {
			return err
		}
		if err = syncExistingDialectArtifactOwner(ctx, rows, name, raw, "location"); err != nil {
			return err
		}
		return acknowledgeDialectArtifactOwner(ctx, rows, name, raw, "location", check)
	}
	return claimDialectArtifactOwner(ctx, rows, name, raw, "location", check)
}

func readbackDialectArtifactLocation(ctx context.Context, metadata *os.Root, expected []byte) (err error) {
	v, err := readDialectArtifactLocationRecord(expected)
	if err != nil {
		return err
	}
	rows, err := openDialectArtifactOwnerFolder(ctx, metadata, false, "location-folder", "owner-locations", v.Owner[:2])
	if err != nil {
		return err
	}
	defer func() { err = errors.Join(err, closeDialectArtifactOwner(ctx, "location-readback", rows.Close)) }()
	raw, err := readDialectArtifactOwnerFile(rows, v.Owner[2:]+".json", dialectArtifactLocationRecordBytes)
	if err != nil || !bytes.Equal(raw, expected) {
		return errors.Join(errDialectArtifactOwner, err)
	}
	if _, err = readDialectArtifactLocationRecord(raw); err != nil {
		return err
	}
	return dialectArtifactOwnerStep(ctx, "location.selected-readback")
}

func marshalDialectArtifactLocation(record dialectArtifactLocationRecord) ([]byte, error) {
	if !record.valid() {
		return nil, errDialectArtifactOwner
	}
	raw, err := json.Marshal(record)
	if err != nil || len(raw) > dialectArtifactLocationRecordBytes {
		return nil, errors.Join(errDialectArtifactOwner, err)
	}
	return raw, nil
}
