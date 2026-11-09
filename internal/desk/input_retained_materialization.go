package desk

// Private D4-B direct preparation. A held saved selector is required; this does
// not enable public recipe, named Dialect, evaluation or subscription routes.
import (
	"bytes"
	"context"
	"encoding/json"
	"reflect"
	"slices"
	"time"
)

func retainedAssertedPreparationRoles() []string {
	return []string{"document-validation", "preview", "preparation"}
}

func retainedDirectPreparationAdmission(admission *retainedAdmission) error {
	if admission != nil && admission.generated {
		return retainedPreparationAdmission(admission)
	}
	if err := retainedValidateAdmission(admission); err != nil {
		return err
	}
	roles := retainedAssertedPreparationRoles()
	if !slices.Equal(admission.selection.Roles, roles) || len(admission.limits) != len(roles) {
		return retainedContractFailure("capability-unsupported")
	}
	for _, role := range roles {
		if admission.limits[role] == nil {
			return retainedContractFailure("capability-unsupported")
		}
	}
	return nil
}

// The snapshot's raw-cell seal binds ID, revision SHA, position, origin and
// source inventory. A generic JSON decode or a copied mutable record cannot
// supply a selected saved operation. Save/CAS authorization belongs to B4.
func retainedSavedPreparationRecord(saved inputPreparationReply, recordID string) (preparedInputRecord, error) {
	doc := saved.Document
	if doc.cellCustody == nil || saved.SHA256 == "" || doc.cellCustody.sha != saved.SHA256 || !reflect.DeepEqual(doc.Imports, doc.cellCustody.imports) {
		return preparedInputRecord{}, inputCellFailure()
	}
	snapshot, err := json.Marshal(doc)
	if err != nil || doc.cellCustody.snapshotDigest == "" || digestOf(snapshot) != doc.cellCustody.snapshotDigest {
		return preparedInputRecord{}, inputCellFailure()
	}
	if _, err := retainedInputMappingRules(doc.Recipe.Qualification); err != nil {
		return preparedInputRecord{}, err
	}
	q := doc.Recipe.Qualification
	if q.Kind != "pack" || q.Pack == "" || q.Digest != retainedDigest([]byte(q.Pack)) {
		return preparedInputRecord{}, retainedContractFailure("input-malformed")
	}
	found := -1
	for i, record := range doc.Records {
		if record.ID == recordID {
			if found != -1 {
				return preparedInputRecord{}, inputCellFailure()
			}
			found = i
		}
	}
	if found < 0 || found >= len(doc.cellCustody.records) {
		return preparedInputRecord{}, inputCellFailure()
	}
	record := doc.Records[found]
	c := doc.cellCustody.records[found]
	if c == nil || record.rawCells != c || c.collectionSHA != saved.SHA256 || c.preparationID != doc.ID || c.position != found || record.checkInputCellCustody() != nil {
		return preparedInputRecord{}, inputCellFailure()
	}
	if record.Class != "asserted" && record.Class != "generated" || record.Class == "asserted" && record.Research != nil || record.Class == "generated" && record.Research == nil {
		return preparedInputRecord{}, inputCellFailure()
	}
	return record, nil
}

// Materialize the complete saved leaf rules into the fixed direct operation.
// Sidecars are checked but never provide values. Generated case facts/evidence
// remain empty; only the acquired signed source's read.copy produces facts.
func materializeRetainedInputMapping(q *inputQualification, generated bool, profileDigest string) ([]byte, error) {
	rules, err := retainedInputMappingRules(q)
	if err != nil {
		return nil, err
	}
	if generated && !retainedSpecDigest.MatchString(profileDigest) || !generated && profileDigest != "" {
		return nil, retainedContractFailure("capability-unsupported")
	}
	facts, evidence := q.Mapping.Case.Facts, q.Mapping.Case.Evidence
	class := "asserted"
	if generated {
		class = "generated"
	}
	factAdmission, evidenceAdmission := map[string][]string{}, map[string][]string{}
	for _, rule := range rules.Facts {
		factAdmission[rule.Target] = []string{class}
	}
	for _, rule := range rules.Evidence {
		evidenceAdmission[rule.Requirement] = []string{class}
	}
	caseMapping := map[string]any{"facts": facts, "evidence": evidence}
	sources := []any{}
	if generated {
		caseMapping = map[string]any{"parameters": map[string]any{"grant": map[string]string{"pointer": "/grant", "type": "string"}, "digest": map[string]string{"pointer": "/digest", "type": "string"}}, "facts": []any{}, "evidence": []any{}}
		sources = append(sources, map[string]any{"name": "research", "kind": "operation", "profile": retainedReviewedInputProfile, "profileDigest": profileDigest, "maxAge": 86400,
			"arguments": map[string]any{"tool": reviewedInputTool, "arguments": map[string]any{"grant": map[string]string{"$param": "grant"}, "digest": map[string]string{"$param": "digest"}}},
			"read":      map[string]any{"copy": map[string]any{"facts": facts, "evidence": evidence}}})
	}
	mapping := map[string]any{"version": 2, "profile": q.ConversionContract.Profile, "case": caseMapping, "sources": sources, "admits": map[string]any{"facts": factAdmission, "evidence": evidenceAdmission}}
	if len(q.Mapping.Unmapped) > 0 {
		mapping["unmapped"] = slices.Clone(q.Mapping.Unmapped)
	}
	if len(q.Mapping.UnmappedEvidence) > 0 {
		mapping["unmappedEvidence"] = slices.Clone(q.Mapping.UnmappedEvidence)
	}
	raw, err := json.Marshal(mapping)
	if err != nil {
		return nil, err
	}
	if generated {
		err = retainedPreparationMapping(raw, profileDigest)
	} else {
		err = retainedAssertedPreparationMapping(raw)
	}
	return raw, err
}

// Host-only entry point. Record origin determines roles only after explicit
// saved tuple validation. Ordinary preparation is never a fallback.
func (s *Server) prepareRetainedSavedInput(ctx context.Context, saved inputPreparationReply, recordID string, downward map[string]map[string]int64) (retainedPreparedInput, error) {
	var result retainedPreparedInput
	if err := ctx.Err(); err != nil {
		return result, err
	}
	record, err := retainedSavedPreparationRecord(saved, recordID)
	if err != nil {
		return result, err
	}
	q := saved.Document.Recipe.Qualification
	if record.Class == "generated" {
		// This pointer is the qualification from the sealed held snapshot, never a
		// caller override. D4-A obtains the authenticated profile before materializing.
		row := inputPreparedRow{ID: record.ID, Origin: record.Class, Prepared: map[string]any{}, Issues: []inputConversionIssue{}}
		prepared, err := s.prepareRetainedReviewedInputSaved(ctx, retainedInstalledSelector(retainedPreparationRoles()...), nil, []byte(q.Pack), saved.Document, record, row, downward, &saved)
		if err != nil {
			return result, err
		}
		if err = ctx.Err(); err != nil {
			return result, err
		}
		return prepared, nil
	}
	if enabled, err := s.manualExecutionDemand(); err != nil {
		return result, err
	} else if enabled {
		return result, executionFailure("unsupported-profile")
	}
	lower, err := retainedPreparationDownward(downward)
	if err != nil {
		return result, err
	}
	for role := range lower {
		if !slices.Contains(retainedAssertedPreparationRoles(), role) {
			return result, retainedContractFailure("capability-unsupported")
		}
	}
	mapping, err := materializeRetainedInputMapping(q, false, "")
	if err != nil {
		return result, err
	}
	status, err := s.retainedPrivateRunnerGET(ctx, "status", retainedRunnerStatusBytes)
	if err != nil {
		return result, err
	}
	runner, err := readRetainedRunnerCapabilities(status)
	if err != nil {
		return result, err
	}
	// Refuse a missing Runner owner before starting Runtime commands.
	if _, err = negotiateRetainedRoles(retainedInstalledSelector("preview", "preparation"), []string{"preview", "preparation"}, false, retainedOwnerReports{Runner: runner}, nil); err != nil {
		return result, err
	}
	checked, err := s.checkRetainedDialectAuthoring(ctx, retainedInstalledSelector("document-validation"), mapping, []byte(q.Pack))
	if err != nil {
		return result, err
	}
	if !checked.Accepted {
		return result, retainedContractFailure("capability-unsupported")
	}
	discovery, err := readRetainedRuntimeDiscovery(checked.Discovery.Stdout)
	if err != nil {
		return result, err
	}
	selection := retainedInstalledSelector(retainedAssertedPreparationRoles()...)
	admission, err := negotiateRetainedRoles(selection, retainedAssertedPreparationRoles(), false, retainedOwnerReports{Runtime: discovery.capabilities, Runner: runner}, lower)
	if err != nil {
		return result, err
	}
	if err = checkRetainedArtifact(admission, "documentBytes", mapping); err != nil {
		return result, err
	}
	projected, err := projectRetainedAssertedInput(saved, recordID, admission)
	if err != nil {
		return result, err
	}
	request, err := retainedAssertedPreparationRequest(checked, projected, admission)
	if err != nil {
		return result, err
	}
	reply, err := s.retainedPreparationPOST(ctx, request, admission)
	if err != nil {
		return result, err
	}
	if err = ctx.Err(); err != nil {
		return result, err
	}
	input, err := retainedAssertedPreparationReply(reply, request, projected, admission)
	if err != nil {
		return result, err
	}
	completed, _ := parseRetainedJSON(input)
	result.RunnerMappingDigest, _ = retainedMemberString(completed.member("source"), "mappingDigest")
	result.ReplyJSON, result.InputJSON, result.SuppliedMappingJSON = bytes.Clone(reply), bytes.Clone(input), bytes.Clone(mapping)
	result.Runtime, result.Selection = checked, selection
	result.Limits, _ = retainedPreparationDownward(admission.limits)
	return result, nil
}

func retainedAssertedPreparationMapping(raw []byte) error {
	bad := retainedContractFailure("capability-unsupported")
	root, err := parseRetainedJSON(raw)
	if err != nil || !retainedExactObject(root, []string{"version", "profile", "case", "admits"}, "sources", "provider", "facts", "evidence", "unmapped", "unmappedEvidence") || string(root.member("version").raw) != "2" {
		return bad
	}
	profile, err := retainedMemberString(root, "profile")
	if err != nil || profile != "base" {
		return bad
	}
	if provider := root.member("provider"); provider != nil && string(provider.raw) != "null" && string(provider.raw) != `""` {
		return bad
	}
	for _, key := range []string{"facts", "evidence"} {
		n := root.member(key)
		if n != nil && string(n.raw) != "null" && !(n.kind == '[' && len(n.elements) == 0) {
			return bad
		}
	}
	if n := root.member("sources"); n != nil && !(n.kind == '[' && len(n.elements) == 0) {
		return bad
	}
	c := root.member("case")
	if !retainedExactObject(c, []string{"facts", "evidence"}) || c.member("facts").kind != '[' || c.member("evidence").kind != '[' {
		return bad
	}
	var leaves inputCaseDialect
	if json.Unmarshal(c.raw, &leaves) != nil {
		return bad
	}
	selection := inputRetainedContract()
	q := &inputQualification{ConversionContract: &selection, Conversions: map[string]inputConversion{}, Mapping: inputRecordDialect{Version: 2, Profile: &profile, Case: &leaves, Sources: []json.RawMessage{}}}
	for _, f := range leaves.Facts {
		if f.Conversion == nil {
			return bad
		}
		v, err := f.Conversion.sidecar()
		if err != nil {
			return bad
		}
		q.Conversions[f.Target] = v
	}
	for _, group := range []struct {
		key string
		out *[]string
	}{{"unmapped", &q.Mapping.Unmapped}, {"unmappedEvidence", &q.Mapping.UnmappedEvidence}} {
		if n := root.member(group.key); n != nil {
			if json.Unmarshal(n.raw, group.out) != nil {
				return bad
			}
		}
	}
	if _, err := retainedInputMappingRules(q); err != nil {
		return err
	}
	admits := root.member("admits")
	if !retainedExactObject(admits, nil, "facts", "evidence") {
		return bad
	}
	for _, group := range []struct {
		key     string
		targets []string
	}{{"facts", nil}, {"evidence", nil}} {
		if group.key == "facts" {
			for _, f := range leaves.Facts {
				group.targets = append(group.targets, f.Target)
			}
		} else {
			for _, e := range leaves.Evidence {
				group.targets = append(group.targets, e.Requirement)
			}
		}
		a := admits.member(group.key)
		if a == nil && len(group.targets) == 0 {
			continue
		}
		if a == nil || a.kind != '{' || len(a.members) != len(group.targets) {
			return bad
		}
		for _, target := range group.targets {
			n := a.member(target)
			if n == nil || n.kind != '[' || len(n.elements) != 1 {
				return bad
			}
			v, err := n.elements[0].scalarString()
			if err != nil || v != "asserted" {
				return bad
			}
		}
	}
	return nil
}

func retainedAssertedPreparationRequest(checked retainedDialectAuthoringCheck, projected retainedAssertedCase, admission *retainedAdmission) ([]byte, error) {
	if err := retainedDirectPreparationAdmission(admission); err != nil {
		return nil, err
	}
	if admission.generated {
		return nil, retainedContractFailure("capability-unsupported")
	}
	if !checked.Accepted || checked.Digest != retainedDigest([]byte(checked.Document)) || retainedAssertedPreparationMapping([]byte(checked.Document)) != nil {
		return nil, retainedContractFailure("input-malformed")
	}
	c, err := parseRetainedJSON([]byte(projected.CaseJSON))
	if err != nil || c.kind != '{' || c.member("retained") == nil || !bytes.Equal(c.member("retained").raw, projected.FrameJSON) {
		return nil, retainedContractFailure("input-malformed")
	}
	frame := c.member("retained")
	if !retainedExactObject(frame, []string{"contractVersion", "origin", "values", "research"}) || string(frame.member("contractVersion").raw) != `"1"` || string(frame.member("origin").raw) != `"asserted"` || string(frame.member("research").raw) != "null" {
		return nil, retainedContractFailure("input-malformed")
	}
	if _, err = retainedAssertedCells(frame.member("values").raw); err != nil {
		return nil, err
	}
	for _, artifact := range []struct {
		key string
		raw []byte
	}{{"documentBytes", []byte(checked.Document)}, {"frameBytes", projected.FrameJSON}, {"combinedBytes", []byte(projected.CaseJSON)}} {
		if err = checkRetainedArtifact(admission, artifact.key, artifact.raw); err != nil {
			return nil, err
		}
	}
	source, err := json.Marshal(map[string]any{"conversionContract": admission.selection, "mapping": json.RawMessage(checked.Document), "retainedCaseJson": projected.CaseJSON})
	if err != nil {
		return nil, err
	}
	n, err := parseRetainedJSON(source)
	if err != nil {
		return nil, err
	}
	if err = checkRetainedArtifact(admission, "documentBytes", n.member("mapping").raw); err != nil {
		return nil, err
	}
	if err = checkRetainedArtifact(admission, "combinedBytes", source); err != nil {
		return nil, err
	}
	request := append(append([]byte(`{"source":`), source...), '}')
	if err = checkRetainedArtifact(admission, "requestBodyBytes", request); err != nil {
		return nil, err
	}
	return request, nil
}

func retainedAssertedPreparationReply(reply, request []byte, projected retainedAssertedCase, admission *retainedAdmission) ([]byte, error) {
	if err := retainedDirectPreparationAdmission(admission); err != nil {
		return nil, err
	}
	if admission.generated {
		return nil, retainedContractFailure("capability-unsupported")
	}
	if err := checkRetainedArtifact(admission, "inputReplyBytes", reply); err != nil {
		return nil, err
	}
	bad := retainedContractFailure("input-malformed")
	root, err := parseRetainedJSON(reply)
	if err != nil || !retainedExactObject(root, []string{"input"}, "factsText", "evidenceText") {
		return nil, bad
	}
	input := root.member("input")
	if !retainedExactObject(input, []string{"facts", "source", "preparation"}, "evidence") || input.member("facts").kind != '{' {
		return nil, bad
	}
	prep := input.member("preparation")
	if !retainedExactObject(prep, []string{"conversionContract", "version", "verifiedAt", "mappingDigest", "verification", "cites", "lineage", "outcomes"}) || string(prep.member("version").raw) != "2" {
		return nil, bad
	}
	stamp, e := retainedMemberString(prep, "verifiedAt")
	parsed, pErr := time.Parse("2006-01-02T15:04:05Z", stamp)
	verification, vErr := retainedMemberString(prep, "verification")
	if e != nil || pErr != nil || parsed.Format("2006-01-02T15:04:05Z") != stamp || vErr != nil || verification == "" {
		return nil, bad
	}
	for _, key := range []string{"cites", "lineage", "outcomes"} {
		if prep.member(key).kind != '[' {
			return nil, bad
		}
	}
	if err = checkRetainedArtifact(admission, "preparationBytes", prep.raw); err != nil {
		return nil, err
	}
	original, err := parseRetainedJSON(request)
	if err != nil || !retainedExactObject(original, []string{"source"}) {
		return nil, bad
	}
	written := original.member("source")
	source := input.member("source")
	if !retainedExactObject(written, []string{"conversionContract", "mapping", "retainedCaseJson"}) || !retainedExactObject(source, []string{"conversionContract", "mapping", "mappingDigest", "retainedCaseJson"}) {
		return nil, bad
	}
	for _, key := range []string{"conversionContract", "retainedCaseJson"} {
		if !retainedJSONEqual(written.member(key), source.member(key)) {
			return nil, bad
		}
	}
	text, e := retainedMemberString(source, "retainedCaseJson")
	if e != nil || text != projected.CaseJSON {
		return nil, bad
	}
	if !retainedAssertedPreparationSameMapping(written.member("mapping"), source.member("mapping")) {
		return nil, bad
	}
	for _, n := range []*retainedJSONNode{source.member("conversionContract"), prep.member("conversionContract")} {
		selection, e := parseRetainedSelector(n.raw, retainedAssertedPreparationRoles()...)
		if e != nil || selection.Contract != admission.selection.Contract || selection.Spec != admission.selection.Spec || selection.Profile != admission.selection.Profile || !slices.Equal(selection.Roles, admission.selection.Roles) {
			return nil, bad
		}
	}
	digest, e := retainedMemberString(source, "mappingDigest")
	prepared, e2 := retainedMemberString(prep, "mappingDigest")
	if e != nil || e2 != nil || !retainedSpecDigest.MatchString(digest) || digest != prepared {
		return nil, bad
	}
	for _, artifact := range []struct {
		key string
		raw []byte
	}{{"documentBytes", source.member("mapping").raw}, {"combinedBytes", source.raw}, {"frameBytes", projected.FrameJSON}} {
		if err = checkRetainedArtifact(admission, artifact.key, artifact.raw); err != nil {
			return nil, err
		}
	}
	// Actual Runner owns recomputation, claim admissibility and lineage. A local
	// projection or a transport stand-in cannot authenticate these semantic facts.
	return bytes.Clone(input.raw), nil
}

func retainedAssertedPreparationSameMapping(written, returned *retainedJSONNode) bool {
	if written == nil || returned == nil || retainedAssertedPreparationMapping(written.raw) != nil || retainedAssertedPreparationMapping(returned.raw) != nil {
		return false
	}
	left, e := parseRetainedJSON(written.raw)
	if e != nil {
		return false
	}
	right, e := parseRetainedJSON(returned.raw)
	if e != nil {
		return false
	}
	// Runner writes v2 legacy defaults, omits empty sources/unmapped lists and
	// empty admission maps. No leaf, source or unknown sibling is erased.
	for _, key := range []string{"provider", "facts", "evidence"} {
		if right.member(key) == nil {
			return false
		}
		if left.member(key) == nil || key == "provider" && string(left.member(key).raw) == "null" {
			raw := []byte("null")
			if key == "provider" {
				raw = []byte(`""`)
			}
			name, _ := json.Marshal(key)
			left.members[retainedJSONKey(name)], _ = parseRetainedJSON(raw)
		}
	}
	for _, root := range []*retainedJSONNode{left, right} {
		for _, key := range []string{"sources", "unmapped", "unmappedEvidence"} {
			if n := root.member(key); n != nil && (string(n.raw) == "null" || n.kind == '[' && len(n.elements) == 0) {
				name, _ := json.Marshal(key)
				delete(root.members, retainedJSONKey(name))
			}
		}
		for _, key := range []string{"facts", "evidence"} {
			if n := root.member("admits").member(key); n != nil && n.kind == '{' && len(n.members) == 0 {
				name, _ := json.Marshal(key)
				delete(root.member("admits").members, retainedJSONKey(name))
			}
		}
	}
	return retainedJSONEqual(left, right)
}
