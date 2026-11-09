package desk

// D4-A is a private host operation. Public recipes, publications, evaluations
// and subscriptions do not dispatch here until they retain explicit selection.
import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"reflect"
	"slices"
	"time"
)

type retainedPreparedInput struct {
	ReplyJSON, InputJSON []byte
	SuppliedMappingJSON  []byte
	RunnerMappingDigest  string
	Selection            retainedConversionSelector
	Limits               map[string]map[string]int64
	Runtime              retainedDialectAuthoringCheck
}

func retainedPreparationRoles() []string {
	return []string{"document-validation", "preview", "preparation", "producer-carrier"}
}

func retainedPreparationAdmission(admission *retainedAdmission) error {
	if err := retainedValidateAdmission(admission); err != nil {
		return err
	}
	roles := retainedPreparationRoles()
	if !admission.generated || !slices.Equal(admission.selection.Roles, roles) || len(admission.limits) != len(roles) {
		return retainedContractFailure("capability-unsupported")
	}
	for _, role := range roles {
		if admission.limits[role] == nil {
			return retainedContractFailure("capability-unsupported")
		}
	}
	return nil
}

func retainedPreparationApproval(admission *retainedAdmission, approved retainedPrivateProducerApproval) error {
	if err := retainedPreparationAdmission(admission); err != nil {
		return err
	}
	seal, err := retainedPrivateApprovalDigest(approved)
	if err != nil || seal != approved.limitsDigest || approved.profileDigest == "" || approved.adapterDigest == "" {
		return retainedContractFailure("capability-unsupported")
	}
	if !approved.admission.generated || !slices.Equal(approved.admission.selection.Roles, []string{"producer-carrier"}) || len(approved.admission.limits) != 1 {
		return retainedContractFailure("capability-unsupported")
	}
	limits, err := retainedProducerLimits(approved)
	if err != nil {
		return err
	}
	if !retainedSameLimits(limits, admission.limits["producer-carrier"]) {
		return retainedContractFailure("capability-unsupported")
	}
	return nil
}

func retainedPreparationProduction(production retainedReviewedInputProduction, admission *retainedAdmission) error {
	if err := retainedPreparationAdmission(admission); err != nil {
		return err
	}
	original, frameSHA, frame, err := retainedCheckedCarrier(production.CarrierJSON, admission)
	if err != nil {
		return err
	}
	if original != production.OriginalSnapshotSHA256 || frameSHA != production.ProjectedFrameSHA256 {
		return retainedContractFailure("input-malformed")
	}
	return checkRetainedArtifact(admission, "frameBytes", frame)
}

func retainedPreparationDownward(downward map[string]map[string]int64) (map[string]map[string]int64, error) {
	copy := map[string]map[string]int64{}
	for role, values := range downward {
		caps := retainedRoleCaps(role)
		if !slices.Contains(retainedPreparationRoles(), role) || len(values) != len(caps) {
			return nil, retainedContractFailure("input-malformed")
		}
		copy[role] = map[string]int64{}
		for key := range caps {
			value := values[key]
			if value < 1 || value > 9007199254740991 {
				return nil, retainedContractFailure("input-malformed")
			}
			copy[role][key] = value
		}
	}
	return copy, nil
}

// This API accepts only a helper-derived exact selector from a trusted host.
// It never infers opt-in or capabilities from a conversion leaf or a report.
func (s *Server) prepareRetainedReviewedInput(ctx context.Context, selection retainedConversionSelector, mapping, pack []byte, doc inputPreparation, record preparedInputRecord, row inputPreparedRow, downward map[string]map[string]int64) (retainedPreparedInput, error) {
	return s.prepareRetainedReviewedInputSaved(ctx, selection, mapping, pack, doc, record, row, downward, nil)
}

// A nonnil held snapshot is accepted only by the saved selected host owner.
func (s *Server) prepareRetainedReviewedInputSaved(ctx context.Context, selection retainedConversionSelector, mapping, pack []byte, doc inputPreparation, record preparedInputRecord, row inputPreparedRow, downward map[string]map[string]int64, saved *inputPreparationReply) (retainedPreparedInput, error) {
	var result retainedPreparedInput
	if saved != nil {
		held, err := retainedSavedPreparationRecord(*saved, record.ID)
		if err != nil {
			return result, err
		}
		if held.Class != "generated" || !reflect.DeepEqual(saved.Document, doc) || !reflect.DeepEqual(held, record) || !bytes.Equal(pack, []byte(saved.Document.Recipe.Qualification.Pack)) || len(mapping) != 0 {
			return result, inputCellFailure()
		}
	}
	if err := retainedValidateSelector(selection, retainedPreparationRoles()...); err != nil {
		return result, err
	}
	if !slices.Equal(selection.Roles, retainedPreparationRoles()) {
		return result, retainedContractFailure("capability-unsupported")
	}
	selection.Roles = slices.Clone(selection.Roles)
	lower, err := retainedPreparationDownward(downward)
	if err != nil {
		return result, err
	}
	if err := ctx.Err(); err != nil {
		return result, err
	}
	if enabled, err := s.manualExecutionDemand(); err != nil {
		return result, err
	} else if enabled {
		return result, executionFailure("unsupported-profile")
	}
	if record.Class != "generated" || row.Origin != "generated" || row.ID != record.ID || len(row.Issues) != 0 {
		return result, retainedContractFailure("capability-unsupported")
	}
	if err := readyInputReview(doc, record); err != nil {
		return result, err
	}
	if len(mapping) > 1048576 || len(pack) > 1048576 {
		return result, retainedContractFailure("input-over-bound")
	}
	mapping, pack = bytes.Clone(mapping), bytes.Clone(pack)
	status, err := s.retainedPrivateRunnerGET(ctx, "status", retainedRunnerStatusBytes)
	if err != nil {
		return result, err
	}
	if err := retainedSelectedCapability(status); err != nil {
		return result, err
	}
	runner, err := readRetainedRunnerCapabilities(status)
	if err != nil {
		return result, err
	}
	profile, _, err := retainedPrivateProfile(s)
	if err != nil {
		return result, err
	}
	profileNode, err := parseRetainedJSON(profile)
	if err != nil {
		return result, err
	}
	producer, err := parseRetainedCapabilities(profileNode.member("conversionContracts").raw)
	if err != nil {
		return result, err
	}
	// Intersect all profile owners before configured/listed documents are read.
	preRoles := []string{"preview", "preparation", "producer-carrier"}
	preLower := map[string]map[string]int64{}
	for _, role := range preRoles {
		if values := lower[role]; values != nil {
			preLower[role] = values
		}
	}
	preliminary, err := negotiateRetainedRoles(retainedInstalledSelector(preRoles...), preRoles, true, retainedOwnerReports{Runner: runner, Producer: producer}, preLower)
	if err != nil {
		return result, err
	}
	profilesLimit, err := retainedArtifactLimit(preliminary, "profilesBytes")
	if err != nil {
		return result, err
	}
	producerLimits := preliminary.limits["producer-carrier"]
	producerLimits["profilesBytes"] = profilesLimit
	approved, err := s.retainedPrivateApprove(ctx, producerLimits)
	if err != nil {
		return result, err
	}
	if saved != nil {
		mapping, err = materializeRetainedInputMapping(saved.Document.Recipe.Qualification, true, approved.profileDigest)
		if err != nil {
			return result, err
		}
	}
	if err := retainedPreparationMapping(mapping, approved.profileDigest); err != nil {
		return result, err
	}
	checked, err := s.checkRetainedDialectAuthoring(ctx, retainedInstalledSelector("document-validation"), mapping, pack)
	result.Runtime = checked
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
	lower["producer-carrier"] = producerLimits
	admission, err := negotiateRetainedRoles(selection, retainedPreparationRoles(), true, retainedOwnerReports{Runtime: discovery.capabilities, Runner: runner, Producer: producer}, lower)
	if err != nil {
		return result, err
	}
	if err := retainedPreparationApproval(admission, approved); err != nil {
		return result, err
	}
	for _, document := range [][]byte{mapping, []byte(checked.Document)} {
		if err := checkRetainedArtifact(admission, "documentBytes", document); err != nil {
			return result, err
		}
	}
	// The actual JSON token can grow through the wire encoder's escaping.
	// This known document overflow must refuse before authoring a grant.
	wireMapping, err := json.Marshal(json.RawMessage(checked.Document))
	if err != nil {
		return result, err
	}
	if err := checkRetainedArtifact(admission, "documentBytes", wireMapping); err != nil {
		return result, err
	}
	// Re-read trusted selected profiles under the final minima; approval owns
	// the authenticated listing, already measured under the same profile cap.
	profiles, selected, adapter, err := s.retainedPrivateProfiles()
	if err != nil {
		return result, err
	}
	if !bytes.Equal(profile, selected) || adapter != approved.adapterDigest {
		return result, retainedContractFailure("capability-unsupported")
	}
	for _, raw := range [][]byte{profiles, selected} {
		if err := checkRetainedArtifact(admission, "profilesBytes", raw); err != nil {
			return result, err
		}
	}
	if err := ctx.Err(); err != nil {
		return result, err
	}
	signed, err := s.retainedPrivateAcquireForPreparation(ctx, doc, record, row, approved, admission)
	if err != nil {
		return result, err
	}
	request, err := retainedPreparationRequest(checked, signed, admission)
	if err != nil {
		return result, err
	}
	reply, err := s.retainedPreparationPOST(ctx, request, admission)
	if err != nil {
		return result, err
	}
	input, err := retainedPreparationReply(reply, request, signed, admission)
	if err != nil {
		return result, err
	}
	result.ReplyJSON, result.InputJSON = bytes.Clone(reply), bytes.Clone(input)
	result.SuppliedMappingJSON = bytes.Clone(mapping)
	completed, _ := parseRetainedJSON(input)
	result.RunnerMappingDigest, _ = retainedMemberString(completed.member("source"), "mappingDigest")
	result.Selection = selection
	// Negotiate returned maps anew so mutation cannot affect another result or
	// the acquisition's sealed approval. No mutable authority is returned.
	result.Limits, _ = retainedPreparationDownward(admission.limits)
	return result, nil
}

// Narrow structural binding, with leaf semantics left to actual Runtime and
// Runner. Unsupported siblings are refused whole, never removed to make fit.
func retainedPreparationMapping(raw []byte, profileDigest string) error {
	bad := retainedContractFailure("capability-unsupported")
	root, err := parseRetainedJSON(raw)
	if err != nil || !retainedExactObject(root, []string{"version", "profile", "case", "sources", "admits"}, "provider", "facts", "evidence", "unmapped", "unmappedEvidence") {
		return bad
	}
	if provider := root.member("provider"); provider != nil && string(provider.raw) != "null" {
		value, err := provider.scalarString()
		if err != nil || value != "" {
			return bad
		}
	}
	for _, key := range []string{"facts", "evidence"} {
		value := root.member(key)
		if value != nil && string(value.raw) != "null" && !(value.kind == '[' && len(value.elements) == 0) {
			return bad
		}
	}
	version, profile := root.member("version"), root.member("profile")
	if string(version.raw) != "2" {
		return bad
	}
	name, err := profile.scalarString()
	if err != nil || name != "base" {
		return bad
	}
	caseNode := root.member("case")
	if !retainedExactObject(caseNode, []string{"parameters", "facts", "evidence"}) {
		return bad
	}
	for _, key := range []string{"facts", "evidence"} {
		value := caseNode.member(key)
		if value.kind != '[' || len(value.elements) != 0 {
			return bad
		}
	}
	params := caseNode.member("parameters")
	if !retainedExactObject(params, []string{"grant", "digest"}) {
		return bad
	}
	for _, key := range []string{"grant", "digest"} {
		parameter := params.member(key)
		if !retainedExactObject(parameter, []string{"pointer", "type"}) {
			return bad
		}
		pointer, _ := retainedMemberString(parameter, "pointer")
		typeName, _ := retainedMemberString(parameter, "type")
		if pointer != "/"+key || typeName != "string" {
			return bad
		}
	}
	sources := root.member("sources")
	if sources.kind != '[' || len(sources.elements) != 1 {
		return bad
	}
	source := sources.elements[0]
	if !retainedExactObject(source, []string{"name", "kind", "profile", "profileDigest", "maxAge", "arguments", "read"}) {
		return bad
	}
	for key, want := range map[string]string{"name": "research", "kind": "operation", "profile": retainedReviewedInputProfile, "profileDigest": profileDigest} {
		got, err := retainedMemberString(source, key)
		if err != nil || got != want {
			return bad
		}
	}
	age, err := retainedPositiveLimit(source.member("maxAge"), false)
	if err != nil || age > 86400 {
		return bad
	}
	arguments, err := parseRetainedJSON([]byte(`{"tool":"` + reviewedInputTool + `","arguments":{"grant":{"$param":"grant"},"digest":{"$param":"digest"}}}`))
	if err != nil || !retainedJSONEqual(source.member("arguments"), arguments) {
		return bad
	}
	read := source.member("read")
	if !retainedExactObject(read, []string{"copy"}) || !retainedExactObject(read.member("copy"), []string{"facts", "evidence"}) {
		return bad
	}
	count := 0
	for _, group := range []struct{ name, target, leaf string }{{"facts", "target", "conversion"}, {"evidence", "requirement", "availability"}} {
		leaves := read.member("copy").member(group.name)
		if leaves.kind != '[' {
			return bad
		}
		for _, leaf := range leaves.elements {
			if !retainedExactObject(leaf, []string{group.target, "source", group.leaf}) || leaf.member(group.leaf).kind != '{' {
				return bad
			}
			pointer, err := retainedMemberString(leaf, "source")
			if err != nil || pointer != "/retained" {
				return bad
			}
			count++
		}
	}
	if count == 0 {
		return bad
	}
	return nil
}

func retainedPreparationSigned(signed retainedPrivateSignedInput, admission *retainedAdmission) (*retainedJSONNode, *retainedJSONNode, error) {
	if err := checkRetainedArtifact(admission, "responseBytes", signed.Response); err != nil {
		return nil, nil, err
	}
	response, err := parseRetainedJSON(signed.Response)
	if err != nil || !retainedExactObject(response, []string{"result", "receipt", "salts"}) {
		return nil, nil, retainedContractFailure("input-malformed")
	}
	carrier := response.member("result")
	original, frameSHA, frame, err := retainedCheckedCarrier(carrier.raw, admission)
	if err != nil {
		return nil, nil, err
	}
	if err := checkRetainedArtifact(admission, "frameBytes", frame); err != nil {
		return nil, nil, err
	}
	if original != signed.OriginalSnapshotSHA256 || sha256Digest(carrier.raw) != signed.ReceivedCarrierSHA256 || frameSHA != signed.ReceivedFrameSHA256 {
		return nil, nil, retainedContractFailure("input-malformed")
	}
	request, err := parseRetainedJSON(signed.Request)
	if err != nil || !retainedExactObject(request, []string{"session", "source", "arguments"}) {
		return nil, nil, retainedContractFailure("input-malformed")
	}
	source, _ := retainedMemberString(request, "source")
	args := request.member("arguments")
	if source != reviewedInputSource || !retainedExactObject(args, []string{"tool", "arguments"}) || !retainedExactObject(args.member("arguments"), []string{"grant", "digest"}) {
		return nil, nil, retainedContractFailure("input-malformed")
	}
	tool, _ := retainedMemberString(args, "tool")
	grant, err := retainedMemberString(args.member("arguments"), "grant")
	digest, digestErr := retainedMemberString(args.member("arguments"), "digest")
	if tool != reviewedInputTool || err != nil || !inputGrantID.MatchString(grant) || digestErr != nil || !retainedSpecDigest.MatchString(digest) {
		return nil, nil, retainedContractFailure("input-malformed")
	}
	return response, args.member("arguments"), nil
}

func retainedPreparationRequest(checked retainedDialectAuthoringCheck, signed retainedPrivateSignedInput, admission *retainedAdmission) ([]byte, error) {
	if err := retainedPreparationAdmission(admission); err != nil {
		return nil, err
	}
	if !checked.Accepted || checked.Digest != retainedDigest([]byte(checked.Document)) {
		return nil, retainedContractFailure("input-malformed")
	}
	if err := checkRetainedArtifact(admission, "documentBytes", []byte(checked.Document)); err != nil {
		return nil, err
	}
	_, args, err := retainedPreparationSigned(signed, admission)
	if err != nil {
		return nil, err
	}
	// RawMessage compacts objects. Custody is a string: its decoded UTF-8 is
	// byte-exact even when JSON escapes punctuation, newlines or opaque content.
	// Runtime's document digest and Runner's parsed mapping digest are distinct
	// domains. Omit the optional latter; only authenticated Runner derives it.
	source, err := json.Marshal(map[string]any{"conversionContract": admission.selection, "mapping": json.RawMessage(checked.Document), "case": json.RawMessage(args.raw), "sources": map[string]any{"research": map[string]string{"retainedResponseJson": string(signed.Response)}}})
	if err != nil {
		return nil, err
	}
	sourceNode, err := parseRetainedJSON(source)
	if err != nil {
		return nil, err
	}
	if err := checkRetainedArtifact(admission, "documentBytes", sourceNode.member("mapping").raw); err != nil {
		return nil, err
	}
	if err := checkRetainedArtifact(admission, "combinedBytes", source); err != nil {
		return nil, err
	}
	request := append(append([]byte(`{"source":`), source...), '}')
	if err := checkRetainedArtifact(admission, "requestBodyBytes", request); err != nil {
		return nil, err
	}
	return request, nil
}

// Fixed path, authentication and bounds; this does not call the ordinary
// inputRunner or grow that owner's ordinary request/reply ceilings.
func (s *Server) retainedPreparationPOST(ctx context.Context, raw []byte, admission *retainedAdmission) ([]byte, error) {
	if err := retainedDirectPreparationAdmission(admission); err != nil {
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if enabled, err := s.manualExecutionDemand(); err != nil {
		return nil, err
	} else if enabled {
		return nil, executionFailure("unsupported-profile")
	}
	inputPath := "/v1/inputs/next"
	if admission.generated {
		inputPath = "/v1/selected-inputs/next"
	}
	path, header, err := retainedRunnerTransport(http.MethodPost, inputPath, admission, admission.generated)
	if err != nil {
		return nil, err
	}
	if err := checkRetainedArtifact(admission, "requestBodyBytes", raw); err != nil {
		return nil, err
	}
	if s.jobs == nil {
		return nil, &inputRunnerError{Message: "The local runner is unavailable."}
	}
	endpoint, token, err := s.jobs.endpoint()
	if err != nil {
		return nil, &inputRunnerError{Message: "The local runner is unavailable."}
	}
	ctx, cancel := context.WithTimeout(ctx, 130*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint+path, bytes.NewReader(raw))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set(retainedSelectorHeaderName, header)
	transport := &http.Transport{Proxy: nil}
	defer transport.CloseIdleConnections()
	client := &http.Client{Transport: transport, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	resp, err := client.Do(req)
	if err != nil {
		return nil, &inputRunnerError{Message: "The runner did not respond. Preparation acknowledgement is unknown; review before retrying."}
	}
	defer resp.Body.Close()
	reply, err := readRetainedArtifact(resp.Body, admission, "inputReplyBytes")
	if err != nil {
		return nil, err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, &inputRunnerError{Status: resp.StatusCode, Message: "The runner refused this preparation."}
	}
	return reply, nil
}

func retainedPreparationReply(reply, request []byte, signed retainedPrivateSignedInput, admission *retainedAdmission) ([]byte, error) {
	if err := retainedPreparationAdmission(admission); err != nil {
		return nil, err
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
	verifiedAt, timeErr := retainedMemberString(prep, "verifiedAt")
	verified, parseErr := time.Parse("2006-01-02T15:04:05Z", verifiedAt)
	verification, verificationErr := retainedMemberString(prep, "verification")
	if timeErr != nil || parseErr != nil || verified.Format("2006-01-02T15:04:05Z") != verifiedAt || verificationErr != nil || verification == "" {
		return nil, bad
	}
	for _, key := range []string{"cites", "lineage", "outcomes"} {
		if prep.member(key).kind != '[' {
			return nil, bad
		}
	}
	if err := checkRetainedArtifact(admission, "preparationBytes", prep.raw); err != nil {
		return nil, err
	}
	original, err := parseRetainedJSON(request)
	if err != nil || !retainedExactObject(original, []string{"source"}) {
		return nil, bad
	}
	source := input.member("source")
	if !retainedExactObject(source, []string{"conversionContract", "mapping", "mappingDigest", "case", "sources"}) {
		return nil, bad
	}
	for _, key := range []string{"conversionContract", "case", "sources"} {
		if !retainedJSONEqual(source.member(key), original.member("source").member(key)) {
			return nil, bad
		}
	}
	if !retainedPreparationSameMapping(original.member("source").member("mapping"), source.member("mapping")) {
		return nil, bad
	}
	for _, selected := range []*retainedJSONNode{source.member("conversionContract"), prep.member("conversionContract")} {
		if selected == nil {
			return nil, bad
		}
		selection, err := parseRetainedSelector(selected.raw, retainedPreparationRoles()...)
		if err != nil || selection.Contract != admission.selection.Contract || selection.Spec != admission.selection.Spec || selection.Profile != admission.selection.Profile || !slices.Equal(selection.Roles, admission.selection.Roles) {
			return nil, bad
		}
	}
	if err := checkRetainedArtifact(admission, "combinedBytes", source.raw); err != nil {
		return nil, err
	}
	if err := checkRetainedArtifact(admission, "documentBytes", source.member("mapping").raw); err != nil {
		return nil, err
	}
	digest, err := retainedMemberString(source, "mappingDigest")
	preparedDigest, digestErr := retainedMemberString(prep, "mappingDigest")
	if err != nil || digestErr != nil || !retainedSpecDigest.MatchString(digest) || digest != preparedDigest {
		return nil, bad
	}
	sources := source.member("sources")
	if !retainedExactObject(sources, []string{"research"}) || !retainedExactObject(sources.member("research"), []string{"retainedResponseJson"}) {
		return nil, bad
	}
	text, err := retainedMemberString(sources.member("research"), "retainedResponseJson")
	if err != nil || text != string(signed.Response) {
		return nil, bad
	}
	if _, _, err := retainedPreparationSigned(signed, admission); err != nil {
		return nil, err
	}
	// Semantic verification and derivation belong to actual Runner. These
	// consistency checks cannot make a stand-in report an authenticated proof.
	return bytes.Clone(input.raw), nil
}

// Only the supported direct v2 envelope's documented encoding differences are
// permitted (Runner dialect_v3.go MarshalJSON and mapping_v2_types.go). Root
// provider/facts/evidence are emitted even when absent in input; optional empty
// unmapped lists and empty Admission maps are omitted. Every other member and
// value, including source/read/conversion/profile/arguments, must match strictly.
// This comparison creates no canonical document or digest and rewrites no input.
func retainedPreparationSameMapping(written, returned *retainedJSONNode) bool {
	if written == nil || returned == nil {
		return false
	}
	sources := written.member("sources")
	if sources == nil || sources.kind != '[' || len(sources.elements) != 1 {
		return false
	}
	profile, _ := retainedMemberString(sources.elements[0], "profileDigest")
	if retainedPreparationMapping(written.raw, profile) != nil || retainedPreparationMapping(returned.raw, profile) != nil {
		return false
	}
	// Parse detached nodes: local deletion changes only this comparison's index.
	left, err := parseRetainedJSON(written.raw)
	if err != nil {
		return false
	}
	right, err := parseRetainedJSON(returned.raw)
	if err != nil {
		return false
	}
	for _, key := range []string{"provider", "facts", "evidence"} {
		if right.member(key) == nil {
			return false
		}
		defaultJSON := []byte("null")
		if key == "provider" {
			defaultJSON = []byte(`""`)
		}
		value := left.member(key)
		if value == nil || key == "provider" && string(value.raw) == "null" {
			raw, _ := json.Marshal(key)
			left.members[retainedJSONKey(raw)], _ = parseRetainedJSON(defaultJSON)
		}
	}
	for _, root := range []*retainedJSONNode{left, right} {
		removeEmpty := func(object *retainedJSONNode, key string, kind byte) {
			value := object.member(key)
			if value != nil && (string(value.raw) == "null" || value.kind == kind && len(value.elements) == 0 && len(value.members) == 0) {
				raw, _ := json.Marshal(key)
				delete(object.members, retainedJSONKey(raw))
			}
		}
		for _, key := range []string{"unmapped", "unmappedEvidence"} {
			removeEmpty(root, key, '[')
		}
		for _, key := range []string{"facts", "evidence"} {
			removeEmpty(root.member("admits"), key, '{')
		}
	}
	return retainedJSONEqual(left, right)
}
