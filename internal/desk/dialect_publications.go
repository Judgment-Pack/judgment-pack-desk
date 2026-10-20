package desk

// Portable translations and Desk recipes have distinct identities. A publication
// retains one final executable translation and its reviewed local recipe snapshot.
// Generated inputs retain the signed operation; they never become asserted facts.
import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"
)

type dialectPublicationContext struct {
	PreparationID string              `json:"preparationId"`
	Revision      string              `json:"revision"`
	Variant       string              `json:"variant"`
	Recipe        retainedArtifactRef `json:"recipe"`
	Pack          retainedArtifactRef `json:"pack"`
	Request       string              `json:"request"`
}

func (c *dialectPublicationContext) valid(p artifactPublication) bool {
	return p.Config == nil && inputPreparationID.MatchString(c.PreparationID) && retainedArtifactDigest.MatchString(c.Revision) && (c.Variant == "asserted" || c.Variant == "generated") && c.Recipe.valid() && c.Pack.valid() && retainedArtifactDigest.MatchString(c.Request) && len(p.Documents) == 1 && p.Documents[0].Kind == "dialect"
}

type dialectPublicationRequest struct {
	Action          string               `json:"action"`
	Revision        string               `json:"revision"`
	Variant         string               `json:"variant"`
	Version         string               `json:"version"`
	CheckDigest     string               `json:"checkDigest,omitempty"`
	Reviewed        bool                 `json:"reviewed,omitempty"`
	BasePublication *string              `json:"basePublication,omitempty"`
	Impact          artifactImpact       `json:"impact"`
	Review          *retainedArtifactRef `json:"review,omitempty"`
}
type dialectPublicationAnswer struct {
	Publication     artifactPublication  `json:"publication"`
	Document        string               `json:"document"`
	Digest          string               `json:"digest"`
	Saved           bool                 `json:"saved"`
	BasePublication string               `json:"basePublication"`
	Review          *retainedArtifactRef `json:"review,omitempty"`
}

func dialectPublicationRequestID(id string, req dialectPublicationRequest) string {
	raw, _ := json.Marshal([]string{id, req.Revision, req.Variant, req.Version})
	return digestOf(raw)
}
func dialectRecipeBytes(doc inputPreparation) []byte { raw, _ := json.Marshal(doc.Recipe); return raw }
func dialectArtifactID(id, variant string) string {
	return "urn:judgmentpack:dialect:" + id + ":" + variant
}

func executableDialect(doc inputPreparation, variant, version string) ([]byte, error) {
	q := doc.Recipe.Qualification
	if q == nil || q.Mapping.Case == nil {
		return nil, errors.New("Choose a pack and save its mapping first.")
	}
	facts := append([]inputDialectFact{}, q.Mapping.Case.Facts...)
	evidence := append([]inputDialectEvidence{}, q.Mapping.Case.Evidence...)
	for i := range facts {
		facts[i].Source = "/prepared/" + inputEscape(facts[i].Target)
	}
	m := map[string]any{"formatVersion": "2.0.0", "id": dialectArtifactID(doc.ID, variant), "version": version, "case": map[string]any{"facts": facts, "evidence": evidence}}
	if variant == "generated" {
		m["formatVersion"] = "3.0.0"
		m["case"] = map[string]any{"parameters": map[string]any{"grant": map[string]string{"pointer": "/grant", "type": "string"}, "digest": map[string]string{"pointer": "/digest", "type": "string"}}, "facts": []any{}, "evidence": []any{}}
		m["sources"] = []any{map[string]any{"name": "research", "kind": "operation", "platform": "jpack-desk", "operation": "reviewed-input.read", "shape": "mcp", "record": "jpack-desk/reviewed-input", "maxAge": 86400, "arguments": map[string]any{"tool": reviewedInputTool, "arguments": map[string]any{"grant": map[string]string{"$param": "grant"}, "digest": map[string]string{"$param": "digest"}}}, "read": map[string]any{"copy": map[string]any{"facts": facts, "evidence": evidence}}}}
		af, ae := map[string][]string{}, map[string][]string{}
		for _, f := range facts {
			af[f.Target] = []string{"generated"}
		}
		for _, e := range evidence {
			ae[e.Requirement] = []string{"generated"}
		}
		admits := map[string]any{}
		if len(af) > 0 {
			admits["facts"] = af
		}
		if len(ae) > 0 {
			admits["evidence"] = ae
		}
		m["admits"] = admits
	} else if variant != "asserted" {
		return nil, errors.New("Choose imported data or reviewed research.")
	}
	if len(q.Mapping.Unmapped) > 0 {
		m["unmapped"] = q.Mapping.Unmapped
	}
	if len(q.Mapping.UnmappedEvidence) > 0 {
		m["unmappedEvidence"] = q.Mapping.UnmappedEvidence
	}
	return json.Marshal(m)
}
func (s *Server) publicationAnswer(ctx context.Context, p artifactPublication) (dialectPublicationAnswer, error) {
	var answer dialectPublicationAnswer
	if p.Dialect == nil || !p.Dialect.valid(p) {
		return answer, errors.New("Dialect publication is unreadable.")
	}
	raw, err := s.artifactRepository().Get(ctx, p.Documents[0].Content)
	if err != nil {
		return answer, err
	}
	return dialectPublicationAnswer{Publication: p, Document: string(raw), Digest: "sha256:" + digestOf(raw), Saved: true, Review: p.Review}, nil
}
func (s *Server) readDialectPublicationLedger() (publicationLedger, error) {
	root, _, err := openOwnFolder(s.root, strings.Split(publicationDirectory, "/"), false, "artifact versions")
	if errors.Is(err, os.ErrNotExist) {
		return publicationLedger{Version: 1, Items: []artifactPublication{}}, nil
	}
	if err != nil {
		return publicationLedger{}, err
	}
	defer root.Close()
	return readPublicationLedger(root)
}
func (s *Server) handleDialectPublications(w http.ResponseWriter, r *http.Request) {
	if !s.guard(w, r) {
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	id := r.PathValue("preparation")
	fail := func(status int, err error) { writeJSONCoded(w, status, CodeBadRequest, err.Error()) }
	if !inputPreparationID.MatchString(id) {
		fail(400, errors.New("Choose a saved dialect."))
		return
	}
	ledger, err := s.readDialectPublicationLedger()
	if err != nil {
		fail(503, err)
		return
	}
	if r.Method == "GET" && r.URL.Query().Get("request") != "" {
		request := r.URL.Query().Get("request")
		if !retainedArtifactDigest.MatchString(request) {
			fail(400, errors.New("Choose a retained publication request."))
			return
		}
		for _, p := range ledger.Items {
			if p.Dialect != nil && p.Dialect.PreparationID == id && (p.Dialect.Request == request || ledger.Receipts[request] == p.ID) {
				answer, e := s.publicationAnswer(r.Context(), p)
				if e != nil {
					fail(503, e)
				} else {
					writeJSON(w, 200, map[string]any{"result": answer})
				}
				return
			}
		}
		writeJSON(w, 200, map[string]any{"result": nil})
		return
	}
	if r.Method == "GET" && r.URL.Query().Get("review") == "latest" {
		variant := r.URL.Query().Get("variant")
		if variant != "asserted" && variant != "generated" {
			fail(400, errors.New("Choose a record kind."))
			return
		}
		review, e := s.latestArtifactReview(r.Context(), dialectArtifactID(id, variant))
		if e != nil {
			fail(503, e)
			return
		}
		writeJSON(w, 200, map[string]any{"review": review})
		return
	}
	if r.Method == "GET" {
		variant := r.URL.Query().Get("variant")
		before := len(ledger.Items) + 1
		if raw := r.URL.Query().Get("before"); raw != "" {
			n, e := strconv.Atoi(raw)
			if e != nil || n < 1 {
				fail(400, errors.New("Choose a valid history page."))
				return
			}
			before = n
		}
		if variant != "" && variant != "asserted" && variant != "generated" {
			fail(400, errors.New("Choose a record kind."))
			return
		}
		answer := struct {
			Items []dialectPublicationAnswer `json:"items"`
			Next  int                        `json:"next,omitempty"`
		}{Items: []dialectPublicationAnswer{}}
		budget := 0
		for i := len(ledger.Items) - 1; i >= 0; i-- {
			p := ledger.Items[i]
			if i+1 >= before || p.Dialect == nil || p.Dialect.PreparationID != id || variant != "" && p.Dialect.Variant != variant {
				continue
			}
			if len(answer.Items) >= 25 || budget >= reviewReadingLimit {
				answer.Next = i + 2
				break
			}
			a, e := s.publicationAnswer(r.Context(), p)
			if e != nil {
				fail(503, e)
				return
			}
			budget += len(a.Document)
			answer.Items = append(answer.Items, a)
		}
		writeJSON(w, 200, answer)
		return
	}

	var req dialectPublicationRequest
	if inputRequest(r, &req) != nil || (req.Action != "check" && req.Action != "publish") || !retainedArtifactDigest.MatchString(req.Revision) || (req.Variant != "asserted" && req.Variant != "generated") || len(req.Version) > 256 {
		fail(400, errors.New("Review a saved recipe, input kind and artifact version."))
		return
	}
	status, answer := s.publishCheckedDialect(r.Context(), id, req, nil)
	writeJSON(w, status, answer)
}

// Manual and chat publication use this exact admission, immutable ledger and
// receipt reconciliation boundary. A supplied view remains held by its caller.
func (s *Server) publishCheckedDialect(ctx context.Context, id string, req dialectPublicationRequest, reader inputPreparationReader) (int, any) {
	fail := func(status int, err error) (int, any) { return status, codedBody(CodeBadRequest, err.Error()) }
	ledger, err := s.readDialectPublicationLedger()
	if err != nil {
		return fail(503, err)
	}
	request := dialectPublicationRequestID(id, req)
	// Completed publication wins over unavailable Runtime or a subsequently edited recipe.
	for _, p := range ledger.Items {
		if p.Dialect != nil && (p.Dialect.Request == request || ledger.Receipts[request] == p.ID) {
			answer, e := s.publicationAnswer(ctx, p)
			if e != nil {
				return fail(503, e)
			} else {
				return 200, answer
			}
		}
	}
	if req.Action == "publish" && (!req.Reviewed || req.CheckDigest == "") {
		return fail(400, errors.New("Check and review this exact version before publishing."))
	}
	base := latestDialectPublication(ledger, id, req.Variant)
	if !req.Impact.valid() {
		return fail(400, errors.New("Describe the compatibility impact before checking this version."))
	}
	if reader == nil {
		view, e := s.preparationRepository().TryRead(ctx)
		if e != nil {
			return fail(409, e)
		}
		defer view.Close()
		reader = view
	}
	saved, err := reader.Get(id)
	if err != nil {
		return fail(404, err)
	}
	if saved.SHA256 != req.Revision {
		return fail(409, errors.New("The dialect changed. Refresh and review the saved recipe again."))
	}
	doc := saved.Document
	raw, err := executableDialect(doc, req.Variant, req.Version)
	if err != nil {
		return fail(422, err)
	}
	checked, err := s.checkedDialectMapping(ctx, raw, doc.Recipe.Qualification.Pack)
	if err != nil {
		return fail(422, err)
	}
	if checked.ArtifactID != dialectArtifactID(id, req.Variant) || checked.ArtifactVersion != req.Version {
		return fail(502, errDialectChecksUnavailable)
	}
	recipe := dialectRecipeBytes(doc)
	pack := []byte(doc.Recipe.Qualification.Pack)
	candidate := artifactPublication{At: time.Now().UTC().Format(time.RFC3339Nano), Dialect: &dialectPublicationContext{id, saved.SHA256, req.Variant, artifactReference(recipe), artifactReference(pack), request}, Documents: []publishedDocument{{Kind: "dialect", Alias: req.Variant, ID: checked.ArtifactID, Version: req.Version, Content: artifactReference([]byte(checked.Document))}}}
	candidate.ID = publicationID(candidate)
	if err = admitPublication(ledger, candidate); err != nil {
		return fail(409, err)
	}
	// A record refresh does not create another artifact publication. The newest
	// matching recipe and document remain the same immutable release.
	for i := len(ledger.Items) - 1; i >= 0; i-- {
		prior := ledger.Items[i]
		if prior.Dialect == nil || prior.Dialect.PreparationID != id || prior.Dialect.Variant != req.Variant {
			continue
		}
		if prior.Dialect.Recipe == candidate.Dialect.Recipe && prior.Documents[0] == candidate.Documents[0] {
			if err = s.retainPublicationReceipt(ctx, request, prior.ID); err != nil {
				return fail(503, err)
			}
			answer, err := s.publicationAnswer(ctx, prior)
			if err != nil {
				return fail(503, err)
			} else {
				return 200, answer
			}
		}
		break
	}
	if req.Action == "publish" && (req.BasePublication == nil || *req.BasePublication != base || req.Review == nil) {
		return fail(409, errors.New("The publication base changed or its review is missing. Check and review this version again."))
	}
	held, err := holdDeskFolder(s.root, s.root.Name())
	if err != nil {
		return fail(503, err)
	}
	defer held.file.Close()
	if err = s.checkPublicationVersions(ctx, held, &publicationTransaction{ledger: ledger, candidate: candidate}); err != nil {
		return fail(409, err)
	}
	var baseVersion string
	for _, prior := range ledger.Items {
		if prior.ID == base {
			baseVersion = prior.Documents[0].Version
			break
		}
	}
	if err = s.checkArtifactImpact(ctx, held, baseVersion, req.Version, req.Impact); err != nil {
		return fail(409, err)
	}
	answer := dialectPublicationAnswer{Publication: candidate, Document: checked.Document, Digest: checked.Digest, BasePublication: base}
	preview, _ := json.Marshal(answer)
	review := artifactReview{Version: 1, Target: dialectArtifactID(id, req.Variant), BasePublication: base, StorageRevision: saved.SHA256, Candidate: candidate.Documents[0].Content, ProposedVersion: req.Version, Impact: req.Impact, Dependencies: []retainedArtifactRef{candidate.Dialect.Recipe, candidate.Dialect.Pack}, Preview: preview}
	if req.Action == "check" {
		for _, data := range [][]byte{[]byte(checked.Document), recipe, pack} {
			if _, err = s.artifactRepository().Put(ctx, data); err != nil {
				return fail(503, err)
			}
		}
		retained, e := s.retainArtifactReview(ctx, review)
		if e != nil {
			return fail(503, e)
		}
		answer.Review = &retained.Reference
		return 200, answer
	}
	if req.CheckDigest != checked.Digest {
		return fail(409, errors.New("The checked document changed. Review it again."))
	}
	retained, e := s.artifactReview(ctx, *req.Review)
	if e != nil {
		return fail(409, e)
	}
	// Preview timestamps are display metadata. All binding fields must still agree.
	review.Preview = retained.Review.Preview
	if !sameDraftValue(review, retained.Review) {
		return fail(409, errors.New("The candidate or compatibility review changed. Check this version again."))
	}
	answer.Review = req.Review
	transaction, err := s.openPublicationTransaction(ctx)
	if err != nil {
		return fail(409, err)
	}
	defer transaction.close()
	for _, p := range transaction.ledger.Items {
		if p.Dialect != nil && (p.Dialect.Request == request || transaction.ledger.Receipts[request] == p.ID) {
			a, e := s.publicationAnswer(ctx, p)
			if e != nil {
				return fail(503, e)
			} else {
				return 200, a
			}
		}
	}
	if latestDialectPublication(transaction.ledger, id, req.Variant) != base {
		return fail(409, errors.New("Another version was published. Review against the new publication."))
	}
	candidate.Review = req.Review
	candidate.ID = publicationID(candidate)
	answer.Publication = candidate
	transaction.candidate = candidate
	if err = admitPublication(transaction.ledger, candidate); err != nil {
		return fail(409, err)
	}
	if err = s.checkPublicationVersions(ctx, held, transaction); err != nil {
		return fail(409, err)
	}
	// Retain all inputs before a single durable ledger commit. No second recipe write.
	for _, data := range [][]byte{[]byte(checked.Document), recipe, pack} {
		if _, err = s.artifactRepository().Put(ctx, data); err != nil {
			return fail(503, err)
		}
	}
	if err = transaction.commit(); err != nil {
		return fail(503, err)
	}
	answer.Saved = true
	return 200, answer
}

// Select a retained publication for the exact reviewed recipe. This never uses
// a newer recipe or changes an existing job's release. Legacy recipes stay usable.
func (s *Server) publishedExecutable(ctx context.Context, doc inputPreparation, variant string) (*dialectPublicationAnswer, error) {
	ledger, err := s.readDialectPublicationLedger()
	if err != nil {
		return nil, err
	}
	recipe := artifactReference(dialectRecipeBytes(doc))
	found := false
	for i := len(ledger.Items) - 1; i >= 0; i-- {
		p := ledger.Items[i]
		if p.Dialect == nil || p.Dialect.PreparationID != doc.ID || p.Dialect.Variant != variant {
			continue
		}
		found = true
		if p.Dialect.Recipe != recipe {
			continue
		}
		answer, err := s.publicationAnswer(ctx, p)
		if err != nil {
			return nil, err
		}
		// Re-materialize with the retained identity, and compare Runtime canonical bytes.
		raw, err := executableDialect(doc, variant, p.Documents[0].Version)
		if err != nil {
			return nil, err
		}
		checked, err := s.checkedDialectMapping(ctx, raw, doc.Recipe.Qualification.Pack)
		if err != nil {
			return nil, err
		}
		if !bytes.Equal([]byte(checked.Document), []byte(answer.Document)) {
			return nil, errors.New("The published mapping no longer matches this recipe. Review a new version.")
		}
		return &answer, nil
	}
	if found {
		return nil, errors.New("The recipe changed since publication. Review its mapping version before running it.")
	}
	return nil, nil
}

type reviewedInputRelease struct {
	ID           string                                `json:"id"`
	Pack         string                                `json:"pack"`
	InputMapping json.RawMessage                       `json:"inputMapping"`
	Dialect      *struct{ ID, Version, Digest string } `json:"dialect"`
	Sample       struct {
		Source struct {
			Import *struct {
				Dialect json.RawMessage `json:"dialect"`
			} `json:"import"`
		} `json:"source"`
	} `json:"sample"`
}

func sameDialectJSON(a, b []byte) bool {
	if checkJSONMembers(a, opaqueJSONMembers) != nil || checkJSONMembers(b, opaqueJSONMembers) != nil {
		return false
	}
	var av, bv any
	da, db := json.NewDecoder(bytes.NewReader(a)), json.NewDecoder(bytes.NewReader(b))
	da.UseNumber()
	db.UseNumber()
	if da.Decode(&av) != nil || db.Decode(&bv) != nil {
		return false
	}
	ar, _ := json.Marshal(dialectComparable(av))
	br, _ := json.Marshal(dialectComparable(bv))
	return bytes.Equal(ar, br)
}
func (s *Server) frozenExecutable(ctx context.Context, doc inputPreparation, release reviewedInputRelease) (*dialectPublicationAnswer, error) {
	var head struct{ FormatVersion string }
	raw := release.InputMapping
	if json.Unmarshal(raw, &head) != nil {
		return nil, errors.New("The subscription's retained mapping is unavailable.")
	}
	if head.FormatVersion == "" {
		return nil, nil
	} // Existing integer releases never adopt a new publication.
	if release.Sample.Source.Import != nil {
		raw = release.Sample.Source.Import.Dialect
	}
	checked, err := s.checkedDialectMapping(ctx, raw, doc.Recipe.Qualification.Pack)
	if err != nil {
		return nil, err
	}
	if release.Dialect == nil || release.Dialect.ID != checked.ArtifactID || release.Dialect.Version != checked.ArtifactVersion || release.Dialect.Digest != checked.Digest {
		return nil, errors.New("The subscription's retained publication is inconsistent.")
	}
	return &dialectPublicationAnswer{Document: checked.Document, Digest: checked.Digest, Saved: true}, nil
}

func verifyPublishedRelease(input, actual json.RawMessage, pin *struct{ ID, Version, Digest string }) error {
	var row struct {
		Source struct {
			Mapping       json.RawMessage `json:"mapping"`
			MappingDigest string          `json:"mappingDigest"`
			Import        *struct {
				Dialect json.RawMessage `json:"dialect"`
				Binding struct {
					Dialect string `json:"dialect"`
				} `json:"binding"`
			} `json:"import"`
		} `json:"source"`
	}
	if json.Unmarshal(input, &row) != nil {
		return errors.New("Reviewed input is unreadable.")
	}
	var head struct{ FormatVersion, ID, Version string }
	published := row.Source.Mapping
	publishedDigest := row.Source.MappingDigest
	if row.Source.Import != nil {
		published = row.Source.Import.Dialect
		publishedDigest = row.Source.Import.Binding.Dialect
	}
	if json.Unmarshal(published, &head) != nil || head.FormatVersion == "" {
		return nil
	}

	if pin == nil || pin.ID != head.ID || pin.Version != head.Version || publishedDigest == "" || pin.Digest != publishedDigest || !sameDialectJSON(row.Source.Mapping, actual) {
		return errors.New("The release does not match the reviewed Dialect publication and execution mapping.")
	}
	return nil
}

func latestDialectPublication(ledger publicationLedger, id, variant string) string {
	for i := len(ledger.Items) - 1; i >= 0; i-- {
		p := ledger.Items[i]
		if p.Dialect != nil && p.Dialect.PreparationID == id && p.Dialect.Variant == variant {
			return p.ID
		}
	}
	return ""
}

// Record a no-op's completed request without publishing another version. This
// makes recovery independent of later recipe edits and Runtime availability.
func (s *Server) retainPublicationReceipt(ctx context.Context, request, id string) error {
	tx, err := s.openPublicationTransaction(ctx)
	if err != nil {
		return err
	}
	defer tx.close()
	if existing, ok := tx.ledger.Receipts[request]; ok {
		if existing != id {
			return errors.New("Publication request already has another receipt.")
		}
		return nil
	}
	found := false
	for _, p := range tx.ledger.Items {
		if p.ID == id {
			found = true
			break
		}
	}
	if !found {
		return errors.New("Publication is no longer available.")
	}
	if len(tx.ledger.Receipts) >= 4096 {
		return errors.New("Artifact publication receipts reached their storage limit.")
	}
	if tx.ledger.Receipts == nil {
		tx.ledger.Receipts = map[string]string{}
	}
	tx.ledger.Receipts[request] = id
	return writePublicationLedger(tx.root, tx.ledger)
}
