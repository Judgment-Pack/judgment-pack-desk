package desk

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
)

var policyRecoveryIncluded = []string{"policy-history", "policy-operation-receipts", "retained-policy-originals", "retained-policy-extractions"}
var policyRecoveryExcluded = []string{"chat-history", "pack-graph-drafts", "memory", "project-files", "jobs", "credentials", "source-consent", "browser-journals"}

type policyRecoveryLimits struct {
	ArchiveBytes  int64 `json:"archiveBytes"`
	ExpandedBytes int64 `json:"expandedBytes"`
	Entries       int   `json:"entries"`
	ManifestBytes int   `json:"manifestBytes"`
}
type policyRecoveryStatus struct {
	ProjectID   string               `json:"projectId"`
	ProjectPath string               `json:"projectPath"`
	Revision    string               `json:"revision,omitempty"`
	Available   bool                 `json:"available"`
	CanInspect  bool                 `json:"canInspect"`
	CanRestore  bool                 `json:"canRestore"`
	Reason      string               `json:"reason,omitempty"`
	Counts      *policyArchiveCounts `json:"counts,omitempty"`
	Limits      policyRecoveryLimits `json:"limits"`
	Scope       string               `json:"scope"`
	Included    []string             `json:"included"`
	Excluded    []string             `json:"excluded"`
}
type policyRecoveryAuthority struct {
	CurrentnessUnknown    bool `json:"currentnessUnknown"`
	FreshReviewRequired   bool `json:"freshReviewRequired"`
	SourceConsentRequired bool `json:"sourceConsentRequired"`
}
type policyRecoveryPreview struct {
	ProjectID           string                  `json:"projectId"`
	ProjectPath         string                  `json:"projectPath"`
	ArchiveDigest       string                  `json:"archiveDigest"`
	ArchiveBytes        int64                   `json:"archiveBytes"`
	LedgerDigest        string                  `json:"ledgerDigest"`
	SchemaVersion       int                     `json:"schemaVersion"`
	ArchiveVersion      int                     `json:"archiveVersion"`
	CapturedAt          string                  `json:"capturedAt"`
	Counts              policyArchiveCounts     `json:"counts"`
	Scope               string                  `json:"scope"`
	Included            []string                `json:"included"`
	Excluded            []string                `json:"excluded"`
	Destination         string                  `json:"destination"`
	DestinationRevision string                  `json:"destinationRevision"`
	Authority           policyRecoveryAuthority `json:"authority"`
	RestartRequired     bool                    `json:"restartRequired"`
}
type policyRecoveryRequest struct {
	OperationID         string `json:"operationId"`
	ProjectID           string `json:"projectId"`
	ArchiveDigest       string `json:"archiveDigest"`
	ArchiveBytes        int64  `json:"archiveBytes"`
	LedgerDigest        string `json:"ledgerDigest"`
	Destination         string `json:"destination"`
	DestinationRevision string `json:"destinationRevision"`
}
type policyRecoveryReceipt struct {
	Kind                string      `json:"kind"`
	OperationID         string      `json:"operationId"`
	RequestDigest       string      `json:"requestDigest"`
	ProjectID           string      `json:"projectId"`
	ArchiveDigest       string      `json:"archiveDigest"`
	ArchiveBytes        int64       `json:"archiveBytes"`
	LedgerDigest        string      `json:"ledgerDigest"`
	Destination         string      `json:"destination"`
	DestinationRevision string      `json:"destinationRevision"`
	SnapshotID          string      `json:"snapshotId"`
	At                  string      `json:"at"`
	Actor               policyActor `json:"actor"`
}
type policyRecoveryResult struct {
	Delivery        string                  `json:"delivery"`
	OutcomeKind     string                  `json:"outcomeKind"`
	ProjectID       string                  `json:"projectId"`
	Receipt         policyRecoveryReceipt   `json:"receipt"`
	RestartRequired bool                    `json:"restartRequired"`
	Authority       policyRecoveryAuthority `json:"authority"`
}

func recoveryAuthority() policyRecoveryAuthority { return policyRecoveryAuthority{true, true, true} }
func (s *Server) policyRecoveryProjectID() (string, error) {
	if !s.assistant.usable() || !custodyChecked {
		return "", policyRefusal(503, "storage-unavailable", "Private Policy recovery storage is unavailable.")
	}
	held, err := s.root.Stat(".")
	if err != nil || !sameDirectory(s.projectDir, held) || s.folderKey() == "" {
		return "", policyRefusal(503, "storage-unavailable", "The current project identity is unavailable.")
	}
	return digestOf([]byte(s.installationOwner() + "\x00" + s.projectDir + "\x00" + s.folderKey())), nil
}
func policyRecoveryFileDigest(file *os.File) (string, int64, error) {
	stat, err := file.Stat()
	if err != nil {
		return "", 0, err
	}
	h := sha256.New()
	_, err = io.Copy(h, io.NewSectionReader(file, 0, stat.Size()))
	return hex.EncodeToString(h.Sum(nil)), stat.Size(), err
}
func (s *Server) policyRecoveryCut(ctx context.Context) (policyArchiveCut, error) {
	var snapshot policyLedger
	err := (filePolicyRepository{open: s.openPolicies}).View(ctx, func(l policyLedger) error { snapshot = l; return nil })
	if err != nil {
		return policyArchiveCut{}, err
	}
	return policyArchiveClosure(ctx, snapshot, s.artifactRepository())
}
func (s *Server) policyRecoveryDestination(destination string) (*ProjectRoot, string, error) {
	if !policyRecoveryCanPublish {
		return nil, "", policyRefusal(409, "archive-unsupported", "This platform cannot safely publish a restored project without replacing existing data.")
	}
	if !validProjectHistoryPath(destination) || len(destination) > 2048 || destination == string(filepath.Separator) || pathContains(s.projectDir, destination) || pathContains(destination, s.projectDir) || pathContains(s.configDir, destination) || pathContains(destination, s.configDir) {
		return nil, "", policyRefusal(400, "bad-request", "Choose a new absolute project folder outside the current project and protected settings.")
	}
	// Read the configured path without opening or initializing its contents. An
	// unavailable chat store must still be protected, and must not block recovery
	// into an unrelated destination.
	paths, err := s.policyRecoveryProtectedChatPaths()
	if err != nil {
		return nil, "", err
	}
	for _, path := range paths {
		if pathContains(path, destination) || pathContains(destination, path) {
			return nil, "", policyRefusal(400, "bad-request", "Choose a project folder separate from saved chat storage.")
		}
	}
	parent, err := OpenProjectRoot(filepath.Dir(destination))
	if err != nil {
		return nil, "", policyRefusal(400, "destination-conflict", "The destination parent is unavailable.")
	}
	if err = safeMoveParent(parent.info); err != nil {
		parent.Close()
		return nil, "", policyRefusal(400, "destination-conflict", "Use a parent folder owned by you that other users cannot modify.")
	}
	if parent.dir != filepath.Dir(destination) {
		parent.Close()
		return nil, "", policyRefusal(400, "destination-conflict", "Choose a destination without symbolic-link parents.")
	}
	if _, err = parent.own.root.Lstat(filepath.Base(destination)); !errors.Is(err, os.ErrNotExist) {
		parent.Close()
		return nil, "", policyRefusal(409, "destination-conflict", "The destination already exists or cannot be checked. Choose a new unused project folder.")
	}
	revision := digestOf([]byte(destination + "\x00" + identityKey(parent.info)))
	return parent, revision, nil
}

func (s *Server) policyRecoveryProtectedChatPaths() ([]string, error) {
	paths := []string{s.defaultChatDataPath()}
	lock, err := s.privateDataLockContext(context.Background(), false)
	if err != nil {
		return nil, err
	}
	defer lock.Close()
	settings, err := s.storageSettingsRoot()
	if errors.Is(err, os.ErrNotExist) && s.containedDeskData() {
		return paths, nil
	}
	if err != nil {
		return nil, err
	}
	defer settings.Close()
	raw, err := readPrivateData(settings, dataLocationName, 16<<10)
	if errors.Is(err, os.ErrNotExist) {
		return paths, nil
	}
	if err != nil {
		return nil, policyRefusal(503, "storage-unavailable", "The configured chat-store location cannot be checked. Its contents were not changed.")
	}
	var location dataLocation
	if rejectDuplicateMembers(raw) != nil || decodeDataJSON(raw, &location) != nil || location.Version != 1 {
		return nil, policyRefusal(503, "storage-unavailable", "The configured chat-store location is invalid. Its contents were not changed.")
	}
	for _, path := range []string{location.Path, location.Previous} {
		if path == "" {
			continue
		}
		if s.containedDeskData() {
			if !filepath.IsLocal(path) {
				return nil, policyRefusal(503, "storage-unavailable", "The configured chat-store location is invalid.")
			}
			path = filepath.Join(s.projectDir, ".desk-private", path)
		}
		if !validProjectHistoryPath(path) {
			return nil, policyRefusal(503, "storage-unavailable", "The configured chat-store location is invalid.")
		}
		paths = append(paths, path)
	}
	return paths, nil
}

func (s *Server) handlePolicyRecovery(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	if !s.guard(w, r) {
		return
	}
	held, ok := s.sessionOf(r)
	if !ok || !validPolicyActor(policyActorOf(held)) {
		policyHTTPFailure(w, policyRefusal(401, "unauthorized", "Sign in before accessing Policy recovery."))
		return
	}
	actor := policyActorOf(held)
	service := s.localPolicyService(r, actor)
	if err := service.authorized(r.Context()); err != nil {
		policyHTTPFailure(w, err)
		return
	}
	projectID, err := s.policyRecoveryProjectID()
	if err != nil {
		policyHTTPFailure(w, err)
		return
	}
	if r.URL.RawQuery != "" {
		policyHTTPFailure(w, policyRefusal(400, "bad-request", "Invalid Policy recovery request."))
		return
	}
	if operation := r.PathValue("operation"); operation != "" {
		if !policyOperationIDForm.MatchString(operation) {
			policyHTTPFailure(w, policyRefusal(400, "bad-request", "Invalid recovery operation identity."))
			return
		}
		result, err := s.readPolicyRecoveryOperation(r.Context(), projectID, operation, nil)
		if err != nil {
			policyHTTPFailure(w, err)
			return
		}
		if err = service.authorized(r.Context()); err != nil {
			policyHTTPFailure(w, err)
			return
		}
		writeJSON(w, 200, result)
		return
	}
	if r.Method == http.MethodGet {
		cut, cutErr := s.policyRecoveryCut(r.Context())
		if strings.HasSuffix(r.URL.Path, "/archive") {
			if cutErr != nil {
				policyHTTPFailure(w, policyBeforeCommitFailure(cutErr))
				return
			}
			if r.Header.Get("If-Match") != digestOf(cut.Raw) {
				policyHTTPFailure(w, policyRefusal(409, "recovery-conflict", "Policy history changed. Refresh the recovery summary before downloading."))
				return
			}
			transfer, e := s.privateDataFileLock(".policy-recovery-transfer.lock", true)
			if e != nil {
				policyHTTPFailure(w, policyBeforeCommitFailure(e))
				return
			}
			defer transfer.Close()
			file, name, e := newDataStage(s.assistant.root)
			if e != nil {
				policyHTTPFailure(w, policyBeforeCommitFailure(e))
				return
			}
			defer file.Close()
			defer s.assistant.root.Remove(name)
			if e = writePolicyArchive(r.Context(), file, cut, s.artifactRepository()); e != nil {
				policyHTTPFailure(w, policyBeforeCommitFailure(e))
				return
			}
			if e = service.authorized(r.Context()); e != nil {
				policyHTTPFailure(w, e)
				return
			}
			digest, _, e := policyRecoveryFileDigest(file)
			if e != nil {
				policyHTTPFailure(w, policyBeforeCommitFailure(e))
				return
			}
			if e = service.authorized(r.Context()); e != nil {
				policyHTTPFailure(w, e)
				return
			}
			w.Header().Set("Content-Type", "application/zip")
			w.Header().Set("Content-Disposition", `attachment; filename="jpack-policy-recovery.zip"`)
			w.Header().Set("X-Policy-Archive-Digest", digest)
			// If-Match selects our Policy snapshot rather than an HTTP ETag. It was
			// checked above; ServeContent must not reinterpret it as a missing ETag.
			download := r.Clone(r.Context())
			download.Header = r.Header.Clone()
			download.Header.Del("If-Match")
			http.ServeContent(w, download, "jpack-policy-recovery.zip", time.Time{}, file)
			return
		}
		status := policyRecoveryStatus{ProjectID: projectID, ProjectPath: s.projectDir, CanInspect: policyRecoveryCanPublish, CanRestore: policyRecoveryCanPublish, Scope: "project-policies", Included: policyRecoveryIncluded, Excluded: policyRecoveryExcluded, Limits: policyRecoveryLimits{policyArchiveLimit, policyArchiveDataLimit, policyArchiveEntryLimit, maxBackupManifest}}
		if cutErr == nil {
			status.Available = true
			status.Revision = digestOf(cut.Raw)
			status.Counts = &cut.Counts
		} else {
			status.Reason = "archive-integrity"
		}
		if err = service.authorized(r.Context()); err != nil {
			policyHTTPFailure(w, err)
			return
		}
		writeJSON(w, 200, status)
		return
	}
	transfer, err := s.privateDataFileLock(".policy-recovery-transfer.lock", true)
	if err != nil {
		policyHTTPFailure(w, policyBeforeCommitFailure(err))
		return
	}
	defer transfer.Close()
	file, name, settings, err := s.readPolicyRecoveryUpload(w, r)
	if err != nil {
		policyHTTPFailure(w, policyBeforeCommitFailure(err))
		return
	}
	defer file.Close()
	defer s.assistant.root.Remove(name)
	digest, size, err := policyRecoveryFileDigest(file)
	if err != nil {
		policyHTTPFailure(w, policyBeforeCommitFailure(err))
		return
	}
	if strings.HasSuffix(r.URL.Path, "/restore") {
		var q policyRecoveryRequest
		if rejectDuplicateMembers(settings) != nil || decodeDataJSON(settings, &q) != nil || !validPolicyRecoveryRequest(q) || q.ProjectID != projectID || q.ArchiveDigest != digest || q.ArchiveBytes != size {
			policyHTTPFailure(w, policyRefusal(400, "bad-request", "Restore must match the exact reviewed archive and destination."))
			return
		}
		result, err := s.restorePolicyArchive(r.Context(), projectID, actor, q, file, service.authorized)
		if err != nil {
			policyHTTPFailure(w, err)
			return
		}
		writeJSON(w, 200, result)
		return
	}
	var q struct {
		Destination string `json:"destination"`
	}
	if rejectDuplicateMembers(settings) != nil || decodeDataJSON(settings, &q) != nil {
		policyHTTPFailure(w, policyRefusal(400, "bad-request", "Choose a new destination project folder."))
		return
	}
	cut, manifest, _, err := readPolicyArchive(r.Context(), file)
	if err != nil {
		policyHTTPFailure(w, policyBeforeCommitFailure(err))
		return
	}
	parent, revision, err := s.policyRecoveryDestination(q.Destination)
	if err != nil {
		policyHTTPFailure(w, policyBeforeCommitFailure(err))
		return
	}
	parent.Close()
	if err = service.authorized(r.Context()); err != nil {
		policyHTTPFailure(w, err)
		return
	}
	writeJSON(w, 200, policyRecoveryPreview{ProjectID: projectID, ProjectPath: s.projectDir, ArchiveDigest: digest, ArchiveBytes: size, LedgerDigest: manifest.Ledger.SHA256, SchemaVersion: 2, ArchiveVersion: 1, CapturedAt: manifest.CapturedAt, Counts: cut.Counts, Scope: "project-policies", Included: policyRecoveryIncluded, Excluded: policyRecoveryExcluded, Destination: q.Destination, DestinationRevision: revision, Authority: recoveryAuthority(), RestartRequired: true})
}

func (s *Server) readPolicyRecoveryUpload(w http.ResponseWriter, r *http.Request) (*os.File, string, []byte, error) {
	r.Body = http.MaxBytesReader(w, r.Body, policyArchiveLimit+32<<10)
	reader, err := r.MultipartReader()
	if err != nil {
		return nil, "", nil, policyRefusal(400, "bad-request", "Choose a Policy archive and a new destination project folder.")
	}
	part, err := reader.NextPart()
	if err != nil || part.FormName() != "settings" {
		return nil, "", nil, policyRefusal(400, "bad-request", "Reviewed settings must precede the archive.")
	}
	settings, err := readBounded(part, 16<<10)
	part.Close()
	if err != nil {
		return nil, "", nil, policyRefusal(400, "bad-request", "Recovery settings exceed their limit.")
	}
	part, err = reader.NextPart()
	if err != nil || part.FormName() != "archive" {
		return nil, "", nil, policyRefusal(400, "bad-request", "A Policy archive is required.")
	}
	file, name, err := newDataStage(s.assistant.root)
	if err != nil {
		return nil, "", nil, err
	}
	fail := func(err error) (*os.File, string, []byte, error) {
		file.Close()
		s.assistant.root.Remove(name)
		return nil, "", nil, err
	}
	size, err := io.Copy(file, io.LimitReader(part, policyArchiveLimit+1))
	part.Close()
	if err != nil || size > policyArchiveLimit {
		return fail(policyArchiveCapacity())
	}
	if _, err = reader.NextPart(); err != io.EOF {
		return fail(policyRefusal(400, "bad-request", "Unexpected data follows the archive."))
	}
	return file, name, settings, nil
}
