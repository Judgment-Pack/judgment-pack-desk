package desk

// Reviewed research is acquired through an installation-owned generated profile.
// A receipt authenticates Desk's saved snapshot, not factual truth or provider
// authorship. The browser cannot register this profile or supply its result.
import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"
)

const reviewedInputProfile = "desk-reviewed-research-v1"
const reviewedInputSource = "desk-inputs"
const reviewedInputTool = "read_reviewed_input"
const reviewedInputEndpoint = "desk-local:reviewed-inputs"
const inputHandoffLimit = 1 << 20

var inputGrantID = regexp.MustCompile(`^grant-[a-f0-9]{32}$`)

type inputHandoff struct {
	Expires string          `json:"expires"`
	Digest  string          `json:"digest"`
	Result  json.RawMessage `json:"result"`
}

func deskInputAdapter() (string, map[string]string, error) {
	executable, err := os.Executable()
	if err != nil {
		return "", nil, err
	}
	f, err := os.Open(executable)
	if err != nil {
		return "", nil, err
	}
	defer f.Close()
	h := sha256.New()
	if _, err = io.Copy(h, f); err != nil {
		return "", nil, err
	}
	return executable, map[string]string{"name": "jpack-desk/reviewed-input", "version": "1", "digest": "sha256:" + hex.EncodeToString(h.Sum(nil))}, nil
}

// RunInputSourceAdapter has one read-only operation over a short-lived, random
// grant. No caller-supplied paths, file enumeration, research or evaluation.
func RunInputSourceAdapter(input io.Reader, output io.Writer) error {
	var request struct {
		Tool      string `json:"tool"`
		Arguments struct {
			Grant  string `json:"grant"`
			Digest string `json:"digest"`
		} `json:"arguments"`
	}
	raw, err := io.ReadAll(io.LimitReader(input, 4097))
	if err != nil || len(raw) > 4096 || decodeDataJSON(raw, &request) != nil || request.Tool != reviewedInputTool || !inputGrantID.MatchString(request.Arguments.Grant) {
		return errors.New("Invalid reviewed-input request.")
	}
	dir := os.Getenv("JPACK_INPUT_HANDOFFS")
	if !filepath.IsAbs(dir) {
		return errors.New("Reviewed-input storage is unavailable.")
	}
	root, err := openPrivateDataRoot(dir, false)
	if err != nil {
		return err
	}
	defer root.Close()
	raw, err = readPrivateData(root, request.Arguments.Grant+".json", inputHandoffLimit)
	var grant inputHandoff
	if err != nil || decodeDataJSON(raw, &grant) != nil {
		return errors.New("Reviewed-input grant is unavailable.")
	}
	until, err := time.Parse(time.RFC3339Nano, grant.Expires)
	if err != nil || !time.Now().Before(until) || grant.Digest != request.Arguments.Digest || grant.Digest != sha256Digest(grant.Result) {
		return errors.New("Reviewed-input grant expired or changed.")
	}
	_, adapter, err := deskInputAdapter()
	if err != nil {
		return err
	}
	return json.NewEncoder(output).Encode(map[string]any{"result": grant.Result, "acquisition": map[string]any{"adapter": adapter, "endpoint": reviewedInputEndpoint, "statement": nil, "snapshot": grant.Digest, "peerIdentity": nil, "schema": nil, "upstreamToken": nil, "observedAt": time.Now().UTC().Format(time.RFC3339)}})
}
func (s *Server) reviewedInputProfiles() (json.RawMessage, error) {
	var profiles []map[string]any
	if len(s.cfg.RunnerInputProfiles) > 0 {
		if err := json.Unmarshal(s.cfg.RunnerInputProfiles, &profiles); err != nil {
			return nil, err
		}
	}
	if s.localGateway == nil {
		return s.cfg.RunnerInputProfiles, nil
	}
	for _, p := range profiles {
		if p["id"] == reviewedInputProfile || p["source"] == reviewedInputSource {
			return nil, errors.New("The reviewed research profile is reserved for Desk.")
		}
	}
	public, err := localIdentity(s.assistant)
	if err != nil {
		return nil, err
	}
	_, adapter, err := deskInputAdapter()
	if err != nil {
		return nil, err
	}
	profiles = append(profiles, map[string]any{"id": reviewedInputProfile, "class": "generated", "source": reviewedInputSource, "authority": localAuthority, "publicKey": public, "shape": "mcp", "adapter": adapter, "endpoint": reviewedInputEndpoint, "tools": []string{reviewedInputTool}})
	return json.Marshal(profiles)
}
func readyInputReview(doc inputPreparation, record preparedInputRecord) error {
	r := record.Review
	if record.Research == nil || r == nil || r.Decision != "accepted" || r.RecipeVersion != doc.RecipeVersion || r.RecordID != record.ID || r.RecordDigest != inputRecordDigest(record) {
		return errors.New("Mark every selected AI record Ready for evaluation for the current recipe. No records were skipped.")
	}
	// A current pointer alone is insufficient: require its retained human event.
	a, _ := json.Marshal(r)
	for _, event := range doc.Reviews {
		b, _ := json.Marshal(event.Review)
		if string(a) == string(b) && inputRecordDigest(event.Record) == r.RecordDigest {
			return nil
		}
	}
	return errors.New("The saved input review is unavailable. Review the record again.")
}
func (s *Server) reviewedInputSnapshot(ctx context.Context, doc inputPreparation, record preparedInputRecord, row inputPreparedRow) (json.RawMessage, error) {
	if err := readyInputReview(doc, record); err != nil {
		return nil, err
	}
	root, err := s.inputResearchRoot(false)
	if err != nil {
		return nil, err
	}
	defer root.Close()
	dir, err := openInputResearch(root, record.Research.RunID, false)
	if err != nil {
		return nil, err
	}
	defer dir.Close()
	run, err := readInputResearch(dir)
	if err != nil {
		return nil, err
	}
	if run.PreparationID != doc.ID || run.RecipeVersion != record.Research.RecipeVersion || !contains([]string{"completed", "completed-with-gaps", "partial", "stopped", "interrupted", "blocked"}, run.State) {
		return nil, errors.New("Research changed or is still running. Review its saved records before evaluation.")
	}
	expected, _ := json.Marshal(record.Research.Entity)
	found := false
	for _, entity := range run.Records {
		raw, _ := json.Marshal(entity)
		if string(raw) == string(expected) {
			found = inputRecordDigest(record) == inputRecordDigest(researchPreparedRecord(run, entity))
		}
	}
	if !found || record.ID != researchPreparedRecord(run, record.Research.Entity).ID {
		return nil, errors.New("The record does not match its retained research.")
	}
	// Bind exact retained model responses and acquisition proofs by digest. These
	// are local references, not a claim that all proofs are in the run export.
	attempts := []map[string]any{}
	modelBound := false
	for _, a := range run.Attempts {
		if a.Kind == "model" && a.State == "completed" {
			action, e := decodeResearchAction(a.Result)
			if run.ContractVersion < 2 && e == nil && action.Tool == "records" {
				replay := run
				replay.Records = nil
				if applyResearchRecords(&replay, action.Arguments) == nil {
					for _, entity := range replay.Records {
						raw, _ := json.Marshal(entity)
						if string(raw) == string(expected) {
							modelBound = true
						}
					}
				}
			}
			attempts = append(attempts, map[string]any{"id": a.ID, "requestDigest": a.Digest, "resultDigest": sha256Digest(a.Result), "finishedAt": a.FinishedAt})
		}
	}
	if run.ContractVersion >= 2 {
		replay := run
		replay.Records = nil
		replay.Outcomes = nil
		replay.Attempts = nil
		// Corrections can refer to earlier failed results. Rebuild that context
		// in order instead of validating each response against final outcomes.

		ordered := append([]inputResearchAttempt(nil), run.Attempts...)
		sort.SliceStable(ordered, func(i, j int) bool { return ordered[i].AppliedSequence < ordered[j].AppliedSequence })
		for _, attempt := range ordered {
			replay.Attempts = append(replay.Attempts, attempt)
			if attempt.Kind != "model" || attempt.State != "completed" || attempt.TaskID != "" && attempt.AppliedSequence == 0 {
				continue
			}
			action, err := decodeResearchAction(attempt.Result)
			if err == nil && action.Tool == "records" {
				_ = applyResearchRecords(&replay, action.Arguments)
			}
		}
		for _, entity := range replay.Records {
			raw, _ := json.Marshal(entity)
			if string(raw) == string(expected) {
				modelBound = true
			}
		}
	}
	if !modelBound {
		return nil, errors.New("The retained AI response is unavailable.")
	}
	sources := []map[string]any{}
	program, err := s.researchVerifier()
	if err != nil {
		return nil, err
	}
	needed := map[string]bool{}
	for _, field := range record.Research.Entity.Fields {
		for _, citation := range field.Sources {
			needed[citation.SourceID] = true
		}
	}
	for _, source := range run.Sources {
		if needed[source.ID] {
			proof, err := readPrivateData(dir, source.ID+".proof.json", inputResearchSourceProofLimit)
			if err != nil {
				return nil, errors.New("A retained research source is unavailable.")
			}
			var retained inputResearchProof
			if decodeDataJSON(proof, &retained) != nil || retained.Pin != run.Gateway {
				return nil, errors.New("A retained research proof changed.")
			}
			verified, digest, err := verifyResearchProof(ctx, program, retained)
			if err != nil || digest != source.Digest {
				return nil, errors.New("A retained research proof could not be verified.")
			}
			decoded, err := decodeInputResearchSource(inputResearchSource{ID: source.ID, Kind: source.Kind, At: source.At, Digest: digest, Verification: "verified", Bytes: source.Bytes}, retained.Arguments, verified)
			if !source.TextTruncated {
				decoded.TextTruncated = false
			}
			a, _ := json.Marshal(decoded)
			b, _ := json.Marshal(source)
			if err != nil || string(a) != string(b) {
				return nil, errors.New("Saved research source text does not match its receipt.")
			}
			sources = append(sources, map[string]any{"id": source.ID, "kind": source.Kind, "url": source.URL, "urls": source.URLs, "query": source.Query, "at": source.At, "digest": source.Digest, "proofDigest": sha256Digest(proof), "verification": source.Verification})
			delete(needed, source.ID)
		}
	}
	if len(needed) > 0 {
		return nil, errors.New("A cited research source is unavailable.")
	}
	history := []inputRecordReviewEvent{}
	for _, event := range doc.Reviews {
		if event.Review.RecordID == record.ID {
			history = append(history, event)
		}
	}
	raw, err := json.Marshal(map[string]any{"version": 1, "class": "generated", "preparationId": doc.ID, "record": record, "recipeVersion": doc.RecipeVersion, "qualification": doc.Recipe.Qualification, "prepared": row.Prepared, "values": availableInputValues(record), "research": map[string]any{"id": run.ID, "recipeVersion": run.RecipeVersion, "recipe": run.Recipe, "intent": run.Intent, "createdAt": run.CreatedAt, "approvedLimits": researchApprovedLimits(run), "budgetApprovals": run.BudgetRequests, "workflowApproval": researchApprovalEvidence(run), "modelResponses": attempts, "sources": sources}, "reviews": history})
	if err == nil && len(raw) > inputHandoffLimit/2 {
		err = errors.New("This research record exceeds the evaluation handoff limit.")
	}
	return raw, err
}
func (s *Server) prepareReviewedInputs(ctx context.Context, doc inputPreparation, rows []inputPreparedRow) error {
	if len(rows) == 0 || rows[0].Origin != "generated" {
		return nil
	}
	if s.localGateway == nil {
		return errors.New("Install Desk's local processing components to evaluate AI research.")
	}
	snapshots := make([]json.RawMessage, len(rows))
	for i, row := range rows {
		for _, record := range doc.Records {
			if record.ID == row.ID {
				raw, err := s.reviewedInputSnapshot(ctx, doc, record, row)
				if err != nil {
					return err
				}
				snapshots[i] = raw
			}
		}
	}
	raw, err := s.inputRunner(ctx, "input-profiles", nil, "")
	if err != nil {
		return err
	}
	var profiles []struct {
		Profile struct {
			ID    string `json:"id"`
			Class string `json:"class"`
		} `json:"profile"`
		Digest string `json:"digest"`
	}
	if json.Unmarshal(raw, &profiles) != nil {
		return errors.New("The research trust profile is unavailable.")
	}
	digest := ""
	for _, p := range profiles {
		if p.Profile.ID == reviewedInputProfile && p.Profile.Class == "generated" {
			digest = p.Digest
		}
	}
	if digest == "" {
		return errors.New("Restart Desk to load its research trust profile.")
	}
	pin, err := s.localGateway.ensure(s.assistant)
	if err != nil {
		return err
	}
	root, err := openPrivateDataRoot(s.inputHandoffPath(), true)
	if err != nil {
		return err
	}
	defer root.Close()
	for i, snapshot := range snapshots {
		var nonce [16]byte
		_, err := rand.Read(nonce[:])
		id := "grant-" + hex.EncodeToString(nonce[:])
		if err != nil {
			return err
		}
		resultDigest := sha256Digest(snapshot)
		grant, _ := json.Marshal(inputHandoff{time.Now().Add(2 * time.Minute).UTC().Format(time.RFC3339Nano), resultDigest, snapshot})
		if err = writePrivateData(root, id+".json", grant); err != nil {
			return err
		}
		args := map[string]any{"tool": reviewedInputTool, "arguments": map[string]any{"grant": id, "digest": resultDigest}}
		response, err := acquireReviewedInput(ctx, pin, map[string]any{"session": "review-" + id, "source": reviewedInputSource, "arguments": args})
		root.Remove(id + ".json")
		if err != nil {
			return err
		}
		// Rebuild the mapping on the server: never copy generated values from case.
		q := doc.Recipe.Qualification
		facts := []inputDialectFact{}
		evidence := []inputDialectEvidence{}
		admitsFacts, admitsEvidence := map[string][]string{}, map[string][]string{}
		if q.Mapping.Case != nil {
			for _, f := range q.Mapping.Case.Facts {
				facts = append(facts, inputDialectFact{f.Target, "/prepared/" + inputEscape(f.Target)})
				admitsFacts[f.Target] = []string{"generated"}
			}
			for _, f := range q.Mapping.Case.Evidence {
				evidence = append(evidence, f)
				admitsEvidence[f.Requirement] = []string{"generated"}
			}
		}
		mapping := map[string]any{"version": 2, "case": map[string]any{"parameters": map[string]any{"grant": map[string]string{"pointer": "/grant", "type": "string"}, "digest": map[string]string{"pointer": "/digest", "type": "string"}}, "facts": []any{}, "evidence": []any{}}, "sources": []any{map[string]any{"name": "research", "kind": "operation", "profile": reviewedInputProfile, "profileDigest": digest, "maxAge": 86400, "arguments": map[string]any{"tool": reviewedInputTool, "arguments": map[string]any{"grant": map[string]string{"$param": "grant"}, "digest": map[string]string{"$param": "digest"}}}, "read": map[string]any{"copy": map[string]any{"facts": facts, "evidence": evidence}}}}, "admits": map[string]any{"facts": admitsFacts, "evidence": admitsEvidence}, "unmapped": q.Mapping.Unmapped, "unmappedEvidence": q.Mapping.UnmappedEvidence}
		rows[i].Input, err = json.Marshal(map[string]any{"source": map[string]any{"mapping": mapping, "case": map[string]string{"grant": id, "digest": resultDigest}, "sources": map[string]any{"research": map[string]any{"response": json.RawMessage(response)}}}})
		if err != nil {
			return err
		}
	}
	return nil
}
func availableInputValues(record preparedInputRecord) map[string]string {
	values := map[string]string{}
	for k, v := range record.Values {
		if strings.TrimSpace(v) != "" {
			values[k] = v
		}
	}
	if record.Class == "generated" && record.Research != nil {
		for k, field := range record.Research.Entity.Fields {
			if field.Status != "supported" && field.Status != "interpretation" {
				delete(values, k)
			}
		}
	}
	return values
}

func acquireReviewedInput(ctx context.Context, pin *localGatewayPin, body any) ([]byte, error) {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	if err := checkLocalGateway(ctx, pin.URL, pin.Signer.Public); err != nil {
		return nil, err
	}
	raw, _ := json.Marshal(body)
	req, err := http.NewRequestWithContext(ctx, "POST", pin.URL+"/acquire", bytes.NewReader(raw))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	transport := &http.Transport{Proxy: nil}
	defer transport.CloseIdleConnections()
	client := &http.Client{Transport: transport, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	resp, err := client.Do(req)
	if err != nil {
		return nil, errors.New("The reviewed research could not be acquired locally.")
	}
	defer resp.Body.Close()
	raw, err = readBounded(resp.Body, 2<<20)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != 200 {
		return nil, errors.New("The local gateway refused the reviewed research handoff.")
	}
	return raw, nil
}
