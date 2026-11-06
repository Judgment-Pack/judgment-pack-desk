package desk

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
)

// Local provenance only. This row is never a path-opening capability, and no
// factory or live execution path calls this inactive enrollment foundation.
const dialectArtifactWorkDomainRecordBytes = 5702 // base64(4096)=5464 + fixed shape 238

type dialectArtifactWorkDomainRecord struct {
	Version        int    `json:"version"`
	ID             string `json:"id"`
	Kind           string `json:"kind"`
	PathBytes      []byte `json:"pathBytes"`
	FolderIdentity string `json:"folderIdentity"`
	LockIdentity   string `json:"lockIdentity"`
}

func dialectArtifactWorkDomainID(folder, lock string) string {
	raw, _ := json.Marshal([]string{"dialect-artifact-work-v1", folder, lock})
	return digestOf(raw)
}

func (v dialectArtifactWorkDomainRecord) valid() bool {
	return v.Version == 1 && v.Kind == "settings" && assistantRunDigest(v.ID) &&
		dialectArtifactOwnerPath(v.PathBytes) && dialectArtifactOwnerFileIdentity(v.FolderIdentity) &&
		dialectArtifactOwnerFileIdentity(v.LockIdentity) && v.ID == dialectArtifactWorkDomainID(v.FolderIdentity, v.LockIdentity)
}

func readDialectArtifactWorkDomainRecord(raw []byte) (dialectArtifactWorkDomainRecord, error) {
	var v dialectArtifactWorkDomainRecord
	err := readDialectArtifactOwnerJSON(raw, dialectArtifactWorkDomainRecordBytes, &v)
	if err == nil && !v.valid() {
		err = errDialectArtifactOwner
	}
	return v, err
}

// Only the real enrollment caller supplies its original held work descriptor,
// after work/root durability, existing-marker validation and the owner NB lock.
// No caller path, identity, namespace or preconstructed row selects this record.
func (s *Server) publishDialectArtifactWorkDomain(ctx context.Context, metadata *os.Root, work *os.File, check func() error) (raw []byte, err error) {
	defer func() {
		if err != nil {
			raw = nil
		}
	}()
	folder, err := s.assistant.root.Stat(".")
	if err != nil {
		return nil, err
	}
	lock, err := work.Stat()
	if err != nil {
		return nil, err
	}
	v := dialectArtifactWorkDomainRecord{Version: 1, Kind: "settings", PathBytes: []byte(s.configDir),
		FolderIdentity: identityKey(folder), LockIdentity: identityKey(lock)}
	v.ID = dialectArtifactWorkDomainID(v.FolderIdentity, v.LockIdentity)
	if !v.valid() {
		return nil, errDialectArtifactOwner
	}
	checkPins := func() error {
		if err := check(); err != nil {
			return err
		}
		entry, err := s.assistant.root.Lstat(".data-work.lock")
		if err != nil || !os.SameFile(lock, entry) || !sameDirectory(s.configDir, folder) {
			return errors.Join(errDialectArtifactOwner, err)
		}
		return nil
	}
	if err = checkPins(); err != nil {
		return nil, err
	}
	raw, err = json.Marshal(v)
	if err != nil || len(raw) > dialectArtifactWorkDomainRecordBytes {
		return nil, errors.Join(errDialectArtifactOwner, err)
	}
	rows, err := openDialectArtifactOwnerFolder(ctx, metadata, true, "work-domain-folder", "work-domains", v.ID[:2])
	if err != nil {
		return nil, err
	}
	defer func() { err = errors.Join(err, closeDialectArtifactOwner(ctx, "work-domain-folder", rows.Close)) }()
	if err = claimDialectArtifactOwner(ctx, rows, v.ID[2:]+".json", raw, "work-domain", checkPins); err != nil {
		return nil, err
	}
	return raw, nil
}

// Exact selected reread from the original metadata root. It never opens a
// locator from row bytes; the only filesystem components are generated IDs.
func readbackDialectArtifactWorkDomain(ctx context.Context, metadata *os.Root, expected []byte) (err error) {
	v, err := readDialectArtifactWorkDomainRecord(expected)
	if err != nil {
		return err
	}
	rows, err := openDialectArtifactOwnerFolder(ctx, metadata, false, "work-domain-folder", "work-domains", v.ID[:2])
	if err != nil {
		return err
	}
	defer func() { err = errors.Join(err, closeDialectArtifactOwner(ctx, "work-domain-readback", rows.Close)) }()
	raw, err := readDialectArtifactOwnerFile(rows, v.ID[2:]+".json", dialectArtifactWorkDomainRecordBytes)
	if err != nil || !bytes.Equal(raw, expected) {
		return errors.Join(errDialectArtifactOwner, err)
	}
	return dialectArtifactOwnerStep(ctx, "work-domain.selected-readback")
}
