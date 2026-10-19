package desk

// These references are captured from the local release-check response before
// it reaches the browser, never from a run or its export. The profiles are the
// installation's boot inputs, not the profiles embedded in the release. Missing
// historical references stay missing: opening an old run never blesses it.
import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

const runReferencesDir = ".desk-private/run-references"
const runReferencesLimit = 128 << 10

var referenceReleaseID = regexp.MustCompile(`^release_[a-f0-9]{32}$`)

type runReferences struct {
	Version       int             `json:"version"`
	Workspace     string          `json:"workspace"`
	ReleaseID     string          `json:"releaseId"`
	ReleaseDigest string          `json:"releaseDigest"`
	Profiles      json.RawMessage `json:"profiles"`
	RecordedAt    string          `json:"recordedAt"`
}

// Runner v0.8 canonicalizes JSON with UseNumber and encoding/json's map key
// ordering. Match that contract without rounding numeric literals or embedding
// a copy of Runner's evolving Release type in Desk.
func runnerCanonical(raw []byte) ([]byte, error) {
	var value any
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	if err := decoder.Decode(&value); err != nil {
		return nil, err
	}
	if decoder.Decode(new(any)) != io.EOF {
		return nil, errors.New("one JSON value required")
	}
	return json.Marshal(value)
}

func (s *Server) openRunReferences(create bool) (*os.Root, error) {
	if s.root == nil || s.jobs == nil {
		return nil, errors.New("project unavailable")
	}
	root, made, err := openOwnFolder(s.root, strings.Split(runReferencesDir, "/"), create, "release verification references")
	if err != nil {
		return nil, err
	}
	if made {
		if err := writePrivateData(root, ".gitignore", []byte("*\n")); err != nil {
			root.Close()
			return nil, err
		}
	}
	return root, nil
}

func (s *Server) retainRunReferences(release, request []byte) error {
	var object, asked map[string]json.RawMessage
	_, duplicate, err := topLevelMembers(release)
	if err != nil || duplicate != "" || json.Unmarshal(release, &object) != nil || json.Unmarshal(request, &asked) != nil {
		return errors.New("release response unavailable")
	}
	var id, pack, requestedPack string
	if json.Unmarshal(object["id"], &id) != nil || !referenceReleaseID.MatchString(id) || json.Unmarshal(object["pack"], &pack) != nil ||
		json.Unmarshal(asked["pack"], &requestedPack) != nil || pack == "" || pack != requestedPack {
		return errors.New("release response differs from request")
	}
	canonical, err := runnerCanonical(release)
	if err != nil {
		return err
	}
	profiles := bytes.Clone(s.jobs.profiles)
	if len(profiles) == 0 {
		profiles = []byte("[]")
	}
	var entries []json.RawMessage
	if json.Unmarshal(profiles, &entries) != nil || entries == nil {
		return errors.New("installation profiles unavailable")
	}
	reference := runReferences{Version: 1, Workspace: s.jobs.workspace, ReleaseID: id, ReleaseDigest: "sha256:" + digestOf(canonical), Profiles: profiles, RecordedAt: time.Now().UTC().Format(time.RFC3339)}
	root, err := s.openRunReferences(true)
	if err != nil {
		return err
	}
	defer root.Close()
	lock, err := root.Open(".")
	if err != nil {
		return err
	}
	defer lock.Close()
	if err = lockPrivateData(lock, true); err != nil {
		return err
	}
	name := id + ".json"
	existing, err := readPrivateData(root, name, runReferencesLimit)
	if err == nil {
		var previous runReferences
		if decodeDataJSON(existing, &previous) != nil || previous.Version != 1 || previous.Workspace != reference.Workspace || previous.ReleaseID != id || previous.ReleaseDigest != reference.ReleaseDigest {
			return errors.New("saved release reference differs")
		}
		// First captured installation profiles remain the historical trust set.
		return nil
	}
	if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	raw, err := json.Marshal(reference)
	if err != nil || len(raw) > runReferencesLimit {
		return errors.New("release references exceed limit")
	}
	return writePrivateData(root, name, raw)
}

func (s *Server) readRunReferences(id string) (runReferences, error) {
	var references runReferences
	if !referenceReleaseID.MatchString(id) {
		return references, errors.New("invalid release identity")
	}
	root, err := s.openRunReferences(false)
	if err != nil {
		return references, err
	}
	defer root.Close()
	raw, err := readPrivateData(root, id+".json", runReferencesLimit)
	if err != nil {
		return references, err
	}
	if decodeDataJSON(raw, &references) != nil || references.Version != 1 || references.ReleaseID != id || references.Workspace != s.jobs.workspace || !recordForm.MatchString(references.ReleaseDigest) {
		return references, errors.New("saved release reference is invalid")
	}
	var profiles []json.RawMessage
	if json.Unmarshal(references.Profiles, &profiles) != nil || profiles == nil {
		return references, errors.New("saved profiles unavailable")
	}
	return references, nil
}

// The signature is reported separately. A verified signature supplies the
// independent key to Runner too, with --require-signed. Without one, Runner
// may still check the release/inputs, but that cannot promote the signature.
func (s *Server) checkRunReferences(ctx context.Context, raw []byte, result runSignatureAnswer) string {
	var bundle, run map[string]json.RawMessage
	var releaseID string
	if json.Unmarshal(raw, &bundle) != nil || json.Unmarshal(bundle["run"], &run) != nil || json.Unmarshal(run["releaseId"], &releaseID) != nil {
		return "unavailable"
	}
	references, err := s.readRunReferences(releaseID)
	if err != nil {
		return "unavailable"
	}
	return verifyRunReferences(ctx, s.jobs.bin, raw, references, result)
}

func verifyRunReferences(ctx context.Context, bin string, raw []byte, references runReferences, signature runSignatureAnswer) string {
	where, err := os.MkdirTemp("", "jpack-run-check-")
	if err != nil {
		return "check-unavailable"
	}
	defer os.RemoveAll(where)
	for name, data := range map[string][]byte{"run.json": raw, "profiles.json": references.Profiles} {
		if os.WriteFile(filepath.Join(where, name), data, 0600) != nil {
			return "check-unavailable"
		}
	}
	args := []string{"verify-run", "--file", "run.json", "--profiles", "profiles.json", "--release-digest", references.ReleaseDigest}
	if signature.Status == "verified" {
		args = append(args, "--public-key", signature.PublicKey, "--require-signed")
	}
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, bin, args...)
	cmd.Dir = where
	cmd.Env = []string{"LANG=C", "LC_ALL=C"}
	cmd.WaitDelay = time.Second
	stdout := &cappedBuffer{limit: 64 << 10}
	cmd.Stdout = stdout
	cmd.Stderr = io.Discard
	err = cmd.Run()
	if ctx.Err() != nil || stdout.exceeded() {
		return "check-unavailable"
	}
	if err != nil {
		var exited *exec.ExitError
		if errors.As(err, &exited) {
			return "invalid"
		}
		return "check-unavailable"
	}
	var report struct {
		Status              string `json:"status"`
		RetainedDisposition string `json:"retainedDisposition"`
		RecordDigest        string `json:"recordDigest"`
		Signature           *struct {
			Status string `json:"status"`
		} `json:"signature"`
	}
	if json.Unmarshal(stdout.Bytes(), &report) != nil || report.Status != "verified-inputs" || report.RetainedDisposition != "matches-audit" || report.RecordDigest != signature.RecordDigest {
		return "check-unavailable"
	}
	if signature.Status == "verified" && (report.Signature == nil || report.Signature.Status != "signed") {
		return "invalid"
	}
	return "verified"
}
