package desk

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"strings"
	"time"

	exchangeCatalog "github.com/Judgment-Pack/judgment-pack-desk/internal/exchange"
)

const exchangeDirectory = ".desk-private/exchange"
const exchangeHistoryLimit = 4 << 20

type exchangeImport struct {
	ID             string              `json:"id"`
	EntryID        string              `json:"entryId"`
	Location       string              `json:"location"`
	Tag            string              `json:"tag"`
	Commit         string              `json:"commit"`
	PreviousCommit string              `json:"previousCommit,omitempty"`
	At             string              `json:"at"`
	Reviewer       inputReviewActor    `json:"reviewer"`
	Review         retainedArtifactRef `json:"review"`
}
type exchangeHistory struct {
	Version int              `json:"version"`
	Imports []exchangeImport `json:"imports"`
}
type exchangeReviewed struct {
	ID      string                  `json:"id"`
	Release exchangeCatalog.Release `json:"release"`
	Base    string                  `json:"base"`
}

func encodeExchangeReview(reviewed exchangeReviewed) (exchangeReviewed, []byte, error) {
	reviewed.ID = ""
	raw, err := json.Marshal(reviewed)
	if err != nil {
		return reviewed, nil, err
	}
	reviewed.ID = digestOf(raw)
	raw, err = json.Marshal(reviewed)
	if err != nil {
		return reviewed, nil, err
	}
	if len(raw) > retainedArtifactByteLimit {
		return reviewed, nil, errors.New("Catalog review exceeds the retained snapshot limit")
	}
	return reviewed, raw, nil
}
func (s *Server) readExchangeImport(ctx context.Context, record exchangeImport) (exchangeReviewed, exchangeCatalog.Candidate, error) {
	var reviewed exchangeReviewed
	raw, err := s.artifactRepository().Get(ctx, record.Review)
	if err != nil {
		return reviewed, exchangeCatalog.Candidate{}, err
	}
	if decodeDataJSON(raw, &reviewed) != nil {
		return reviewed, exchangeCatalog.Candidate{}, errors.New("Retained Exchange review is unreadable")
	}
	identity, _, err := encodeExchangeReview(reviewed)
	if err != nil || reviewed.ID != identity.ID || digestOf([]byte(reviewed.ID+"\n"+record.EntryID)) != record.ID || reviewed.Release.Commit != record.Commit || reviewed.Release.Location != record.Location || reviewed.Release.Tag != record.Tag {
		return reviewed, exchangeCatalog.Candidate{}, errors.New("Retained Exchange review does not match its import")
	}
	for _, candidate := range reviewed.Release.Candidates {
		if candidate.Row.ID == record.EntryID {
			return reviewed, candidate, nil
		}
	}
	return reviewed, exchangeCatalog.Candidate{}, errors.New("Retained Exchange template is unavailable")
}
func exchangeHistoryID(h exchangeHistory) string { raw, _ := json.Marshal(h); return digestOf(raw) }
func readExchangeHistory(root *os.Root) (exchangeHistory, error) {
	h := exchangeHistory{Version: 1, Imports: []exchangeImport{}}
	raw, err := readPrivateData(root, "history.json", exchangeHistoryLimit)
	if errors.Is(err, os.ErrNotExist) {
		return h, nil
	}
	if err != nil {
		return h, err
	}
	if decodeDataJSON(raw, &h) != nil || h.Version != 1 || h.Imports == nil || len(h.Imports) > 4096 {
		return h, errors.New("Exchange history is unreadable")
	}
	for _, record := range h.Imports {
		if !record.Review.valid() || record.ID == "" || record.EntryID == "" || record.Commit == "" || record.At == "" {
			return h, errors.New("Exchange import history is inconsistent")
		}
	}
	return h, nil
}
func (s *Server) exchangeRoot() (*os.Root, error) {
	root, _, e := openOwnFolder(s.root, strings.Split(exchangeDirectory, "/"), true, "Exchange imports")
	return root, e
}
func (s *Server) exchangeCheck(ctx context.Context, document []byte) (string, string, error) {
	_, report, err := s.checkDialectAuthoring(ctx, document, "")
	if err != nil {
		return "", "", err
	}
	if report.Status != "document-valid" || report.Document == "" || report.Digest == "" {
		return "", "", errors.New("Runtime cannot validate this template's document form")
	}
	return report.Document, report.Digest, nil
}
func (s *Server) handleExchange(w http.ResponseWriter, r *http.Request) {
	if !s.guard(w, r) || s.refuseUnusableStore(w) {
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	fail := func(status int, e error) { writeJSONCoded(w, status, CodeBadRequest, e.Error()) }
	if r.URL.RawQuery != "" {
		fail(400, errors.New("Exchange requests do not accept query parameters"))
		return
	}
	root, err := s.exchangeRoot()
	if err != nil {
		fail(503, err)
		return
	}
	defer root.Close()
	if r.Method == "GET" {
		h, e := readExchangeHistory(root)
		if e != nil {
			fail(503, e)
			return
		}
		if id := r.PathValue("importId"); id != "" {
			for _, record := range h.Imports {
				if record.ID == id {
					_, candidate, e := s.readExchangeImport(r.Context(), record)
					if e != nil {
						fail(503, e)
						return
					}
					writeJSON(w, 200, map[string]any{"import": record, "candidate": candidate})
					return
				}
			}
			fail(404, errors.New("Imported template is unavailable"))
			return
		}
		writeJSON(w, 200, map[string]any{"items": h.Imports, "revision": exchangeHistoryID(h)})
		return
	}
	var request struct {
		Location string `json:"location"`
		Tag      string `json:"tag"`
		ReviewID string `json:"reviewId"`
		EntryID  string `json:"entryId"`
		Base     string `json:"base"`
		Reviewed bool   `json:"reviewed"`
	}
	if inputRequest(r, &request) != nil {
		fail(400, errors.New("Choose a catalog release or a checked import"))
		return
	}
	if strings.HasSuffix(r.URL.Path, "/review") {
		if request.ReviewID != "" || request.EntryID != "" || request.Reviewed || request.Base != "" || len(request.Location) > 2048 || request.Location == "" {
			fail(400, errors.New("Choose a Git catalog location and release tag"))
			return
		}
		tree, close, e := exchangeCatalog.OpenRelease(r.Context(), request.Location, request.Tag)
		if e != nil {
			fail(422, e)
			return
		}
		defer close()
		release, e := exchangeCatalog.Review(r.Context(), tree, request.Location, request.Tag, tree.Commit, s.exchangeCheck, s.exchangeReplay)
		if e != nil {
			fail(422, e)
			return
		}
		h, e := readExchangeHistory(root)
		if e != nil {
			fail(503, e)
			return
		}
		pins := []exchangeCatalog.Pin{}
		identities := append([]exchangeCatalog.Candidate(nil), release.Candidates...)
		for _, record := range h.Imports {
			pins = append(pins, exchangeCatalog.Pin{Location: record.Location, Tag: record.Tag, Commit: record.Commit})
			previous, candidate, err := s.readExchangeImport(r.Context(), record)
			if err != nil {
				fail(503, err)
				return
			}
			pins = append(pins, previous.Release.History...)
			identities = append(identities, candidate)
		}
		if err := exchangeCatalog.CheckArtifactPairs(identities); err != nil {
			fail(409, err)
			return
		}
		// Historical tags already seen must keep their pinned commits too.
		for _, historical := range release.History {
			for _, pin := range pins {
				if historical.Location == pin.Location && historical.Tag == pin.Tag && historical.Commit != pin.Commit {
					fail(409, errors.New("A previously reviewed catalog tag moved"))
					return
				}
			}
		}
		if e = exchangeCatalog.CheckReleasePins(release, pins); e != nil {
			fail(409, e)
			return
		}
		reviewed, raw, e := encodeExchangeReview(exchangeReviewed{Release: release, Base: exchangeHistoryID(h)})
		if e != nil {
			fail(413, e)
			return
		}
		// The review is retained, but importing still requires a separate explicit
		// action. A later reply or chat transcript cannot replace its checked bytes.
		if e = writePrivateData(root, "review-"+reviewed.ID+".json", raw); e != nil {
			fail(503, e)
			return
		}
		changes := map[string]exchangeCatalog.Change{}
		previous := map[string]string{}
		for i := len(h.Imports) - 1; i >= 0; i-- {
			old := h.Imports[i]
			if old.Location != release.Location {
				continue
			}
			if _, seen := changes[old.EntryID]; seen {
				continue
			}
			_, entry, e := s.readExchangeImport(r.Context(), old)
			if e != nil {
				fail(503, e)
				return
			}
			changes[old.EntryID] = exchangeCatalog.Changes(entry, release)
			previous[old.EntryID] = old.ID
		}
		writeJSON(w, 200, map[string]any{"review": reviewed, "changes": changes, "previous": previous})
		return
	}
	if request.Location != "" || request.Tag != "" || !retainedArtifactDigest.MatchString(request.ReviewID) || request.EntryID == "" || !request.Reviewed || !retainedArtifactDigest.MatchString(request.Base) {
		fail(400, errors.New("Review the exact template and import changes before saving"))
		return
	}
	lock, e := root.Open(".")
	if e != nil {
		fail(503, e)
		return
	}
	defer lock.Close()
	if e = waitPrivateDataLock(r.Context(), lock, true); e != nil {
		fail(409, e)
		return
	}
	h, e := readExchangeHistory(root)
	if e != nil {
		fail(503, e)
		return
	}
	// Completed imports are read before current state/capability checks. A repeat
	// response cannot perform a second replacement or grant execution permission.
	id := digestOf([]byte(request.ReviewID + "\n" + request.EntryID))
	for _, prior := range h.Imports {
		if prior.ID == id {
			writeJSON(w, 200, prior)
			return
		}
	}
	if request.Base != exchangeHistoryID(h) {
		fail(409, errors.New("Installed templates changed; review the release again"))
		return
	}
	raw, e := readPrivateData(root, "review-"+request.ReviewID+".json", retainedArtifactByteLimit)
	if e != nil {
		fail(404, errors.New("The checked catalog review is unavailable"))
		return
	}
	var reviewed exchangeReviewed
	if decodeDataJSON(raw, &reviewed) != nil || reviewed.ID != request.ReviewID || reviewed.Base != request.Base {
		fail(409, errors.New("The checked catalog review does not match this import"))
		return
	}
	identity := reviewed
	identity.ID = ""
	identityRaw, _ := json.Marshal(identity)
	if digestOf(identityRaw) != request.ReviewID {
		fail(409, errors.New("The checked catalog review changed"))
		return
	}
	var selected *exchangeCatalog.Candidate
	for i := range reviewed.Release.Candidates {
		if reviewed.Release.Candidates[i].Row.ID == request.EntryID {
			selected = &reviewed.Release.Candidates[i]
			break
		}
	}
	if selected == nil {
		fail(422, errors.New("Only a successfully checked template can be imported"))
		return
	}
	actor := inputReviewActor{Kind: "local-access"}
	if held, ok := s.sessionOf(r); ok {
		actor = inputReviewActor{Kind: "session", Subject: held.subject, Issuer: held.issuer, Name: held.name}
	}
	record := exchangeImport{ID: id, EntryID: selected.Row.ID, Location: reviewed.Release.Location, Tag: reviewed.Release.Tag, Commit: reviewed.Release.Commit, At: time.Now().UTC().Format(time.RFC3339Nano), Reviewer: actor, Review: artifactReference(raw)}
	for i := len(h.Imports) - 1; i >= 0; i-- {
		prior := h.Imports[i]
		if prior.Location == record.Location && prior.EntryID == record.EntryID {
			record.PreviousCommit = prior.Commit
			break
		}
	}
	if _, e = s.artifactRepository().Put(r.Context(), raw); e != nil {
		fail(503, e)
		return
	}
	h.Imports = append(h.Imports, record)
	next, _ := json.Marshal(h)
	if len(next) > exchangeHistoryLimit || len(h.Imports) > 4096 {
		fail(413, errors.New("Exchange import history reached its limit"))
		return
	}
	if e = writePrivateData(root, "history.json", next); e != nil {
		fail(503, e)
		return
	}
	writeJSON(w, 201, record)
}
