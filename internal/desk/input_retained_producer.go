package desk

// The retained generated producer is deliberately private. D4 must negotiate
// Runtime and Runner roles before it can bind this carrier to a public input.
import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"os"
	"time"
)

const retainedReviewedInputProfile = "desk-reviewed-research-v2"
const retainedGrantSuffix = ".retained.json"

// Status is control-plane capability discovery, independent of the negotiated
// producer profiles document. Keep its existing conservative fixed ceiling.
const retainedRunnerStatusBytes int64 = 81920

type retainedPrivateProducerApproval struct {
	profileDigest string
	adapterDigest string
	admission     *retainedAdmission
	limitsDigest  string
}

type retainedPrivateSignedInput struct {
	Response               []byte
	Request                []byte
	CarrierSHA256          string
	OriginalSnapshotSHA256 string
	ProjectedFrameSHA256   string
	ReceivedCarrierSHA256  string
	ReceivedFrameSHA256    string
	ProfileDigest          string
}

// A file's presence, including an unreadable present file, is authoritative
// for dispatch. The request and the Gateway source have no selected-mode flag.
func retainedInputGrantSelection(root *os.Root, id string) (bool, error) {
	legacy, legacyErr := root.Lstat(id + ".json")
	selected, selectedErr := root.Lstat(id + retainedGrantSuffix)
	if legacyErr != nil && !errors.Is(legacyErr, os.ErrNotExist) || selectedErr != nil && !errors.Is(selectedErr, os.ErrNotExist) {
		return false, errors.New("reviewed-input grant presence is unavailable")
	}
	if legacy != nil && selected != nil {
		return false, errors.New("reviewed-input grant channels coexist")
	}
	return selected != nil, nil
}

func retainedPrivateProfile(s *Server) (json.RawMessage, string, error) {
	if s.localGateway == nil {
		return nil, "", retainedContractFailure("capability-unsupported")
	}
	public, err := localIdentity(s.assistant)
	if err != nil {
		return nil, "", err
	}
	_, adapter, err := deskInputAdapter()
	if err != nil {
		return nil, "", err
	}
	adapter["version"] = "2"
	selection := retainedInstalledSelector("producer-carrier")
	grammar := retainedProducerGrammar
	capability := retainedConversionCapability{Contract: selection.Contract, Spec: selection.Spec, Profile: selection.Profile, Role: "producer-carrier", Limits: retainedRoleCaps("producer-carrier"), ProducerGrammar: &grammar}
	profile := map[string]any{
		"id": retainedReviewedInputProfile, "class": "generated", "source": reviewedInputSource,
		"authority": localAuthority, "publicKey": public, "shape": "mcp",
		"adapter": adapter, "endpoint": reviewedInputEndpoint, "tools": []string{reviewedInputTool},
		"retainedColumnProducer": map[string]string{"contractVersion": "1", "grammar": grammar},
		"conversionContracts":    []retainedConversionCapability{capability},
	}
	raw, err := json.Marshal(profile)
	if err != nil {
		return nil, "", err
	}
	if int64(len(raw)) > retainedRoleCaps("producer-carrier")["profilesBytes"] {
		return nil, "", retainedContractFailure("input-over-bound")
	}
	return raw, adapter["digest"], nil
}

// Selected preparation holds only its selected profile document. The ordinary
// registry remains owned by ordinary Jobs, including when all 16 slots are full.
func (s *Server) retainedPrivateProfiles() (json.RawMessage, json.RawMessage, string, error) {
	selected, digest, err := retainedPrivateProfile(s)
	if err != nil {
		return nil, nil, "", err
	}
	document, err := json.Marshal([]json.RawMessage{selected})
	if err != nil {
		return nil, nil, "", err
	}
	return document, selected, digest, nil
}

const retainedSelectedProfileContract = "selected-profile-preparation/1"

// A defensive transport ceiling for report wrappers and set-aside reasons,
// separate from the negotiated bound on the exact array of profile documents.
// This is a Desk refusal bound, not an upstream maximum-envelope guarantee.
const retainedSelectedControlBytes int64 = 2 * 81920

// This selector selects the installed preparation dialect. It does not replace
// negotiation with the independent Runtime, Runner and producer owners, and
// admission of an immutable profile authorizes no acquisition or evaluation.
func retainedSelectedControlHeader() (string, error) {
	return retainedSelectorHeader(retainedInstalledSelector(retainedPreparationRoles()...))
}

func retainedSelectedCapability(raw []byte) error {
	node, err := parseRetainedJSON(raw)
	if err != nil || node.kind != '{' {
		return retainedContractFailure("input-malformed")
	}
	capabilities := node.member("capabilities")
	if capabilities == nil || capabilities.kind != '[' {
		return retainedContractFailure("capability-unsupported")
	}
	found := false
	for _, capability := range capabilities.elements {
		if capability.kind != '"' {
			return retainedContractFailure("input-malformed")
		}
		var name string
		if json.Unmarshal(capability.raw, &name) != nil {
			return retainedContractFailure("input-malformed")
		}
		if name == "selected-profile-preparation" {
			if found {
				return retainedContractFailure("input-malformed")
			}
			found = true
		}
	}
	if !found {
		return retainedContractFailure("capability-unsupported")
	}
	return nil
}

// The control request always uses the current ordinary companion's endpoint:
// no additional store, process or executor lease is opened for selection.
func (s *Server) retainedSelectedControl(ctx context.Context, method string, profile []byte, limit int64) ([]byte, error) {
	if s.jobs == nil || limit < 1 || limit > retainedSelectedControlBytes {
		return nil, retainedContractFailure("capability-unsupported")
	}
	if method != http.MethodGet && method != http.MethodPost || method == http.MethodGet && profile != nil || method == http.MethodPost && (len(profile) == 0 || len(profile) > 32<<10) {
		return nil, retainedContractFailure("input-malformed")
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	header, err := retainedSelectedControlHeader()
	if err != nil {
		return nil, err
	}
	endpoint, token, err := s.jobs.endpoint()
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, method, endpoint+retainedRunnerPrefix+"selected-profiles", bytes.NewReader(profile))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set(retainedSelectorHeaderName, header)
	if method == http.MethodPost {
		req.Header.Set("Content-Type", "application/json")
	}
	transport := &http.Transport{Proxy: nil}
	defer transport.CloseIdleConnections()
	client := &http.Client{Transport: transport, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	response, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer response.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(response.Body, limit+1))
	if err != nil {
		return nil, err
	}
	if int64(len(raw)) > limit {
		return nil, retainedContractFailure("input-over-bound")
	}
	if response.StatusCode != http.StatusOK && !(method == http.MethodPost && response.StatusCode == http.StatusCreated) {
		return nil, retainedContractFailure("capability-unsupported")
	}
	return raw, nil
}

func (s *Server) retainedPrivateRunnerGET(ctx context.Context, path string, limit int64) ([]byte, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if s.jobs == nil || limit < 1 || limit > retainedRoleCaps("producer-carrier")["profilesBytes"] {
		return nil, retainedContractFailure("capability-unsupported")
	}
	endpoint, token, err := s.jobs.endpoint()
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint+"/v1/"+path, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	transport := &http.Transport{Proxy: nil}
	defer transport.CloseIdleConnections()
	client := &http.Client{Transport: transport, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, limit+1))
	if err != nil {
		return nil, err
	}
	if int64(len(raw)) > limit {
		return nil, retainedContractFailure("input-over-bound")
	}
	if resp.StatusCode != http.StatusOK {
		return nil, retainedContractFailure("capability-unsupported")
	}
	return raw, nil
}

// The local authenticated Runner lists a digest for the complete approved
// profile. Its listing is checked against a separately constructed Desk profile;
// a matching parsed report cannot itself create the approval.
func (s *Server) retainedPrivateApprove(ctx context.Context, downward map[string]int64) (retainedPrivateProducerApproval, error) {
	var empty retainedPrivateProducerApproval
	approvedProfile, adapterDigest, err := retainedPrivateProfile(s)
	if err != nil {
		return empty, err
	}
	approvedNode, err := parseRetainedJSON(approvedProfile)
	if err != nil {
		return empty, err
	}
	// Authority comes only from the independently built trusted profile. Detach
	// and intersect its limits before reading configured or listed profiles.
	producer, err := parseRetainedCapabilities(approvedNode.member("conversionContracts").raw)
	if err != nil {
		return empty, err
	}
	var lower map[string]map[string]int64
	if downward != nil {
		lower = map[string]map[string]int64{"producer-carrier": downward}
	}
	admission, err := negotiateRetainedRoles(retainedInstalledSelector("producer-carrier"), []string{"producer-carrier"}, true, retainedOwnerReports{Producer: producer}, lower)
	if err != nil {
		return empty, err
	}
	profilesDocument, configuredProfile, configuredAdapter, err := s.retainedPrivateProfiles()
	if err != nil {
		return empty, err
	}
	if !bytes.Equal(configuredProfile, approvedProfile) || configuredAdapter != adapterDigest {
		return empty, retainedContractFailure("capability-unsupported")
	}
	if err := checkRetainedArtifact(admission, "profilesBytes", profilesDocument); err != nil {
		return empty, err
	}
	status, err := s.retainedPrivateRunnerGET(ctx, "status", retainedRunnerStatusBytes)
	if err != nil {
		return empty, err
	}
	if err := retainedSelectedCapability(status); err != nil {
		return empty, err
	}
	runnerCaps, err := readRetainedRunnerCapabilities(status)
	if err != nil {
		return empty, err
	}
	if _, err := negotiateRetainedRoles(retainedInstalledSelector("preview", "preparation"), []string{"preview", "preparation"}, true, retainedOwnerReports{Runner: runnerCaps}, nil); err != nil {
		return empty, err
	}
	// Admit the exact immutable profile, then check the complete authenticated
	// selected listing. A conflict or missing capability never falls back to Jobs.
	admittedRaw, err := s.retainedSelectedControl(ctx, http.MethodPost, approvedProfile, retainedSelectedControlBytes)
	if err != nil {
		return empty, err
	}
	admitted, err := parseRetainedJSON(admittedRaw)
	if err != nil || !retainedExactObject(admitted, []string{"profile", "digest"}) || !retainedJSONEqual(admitted.member("profile"), approvedNode) {
		return empty, retainedContractFailure("input-malformed")
	}
	admittedDigest, err := retainedMemberString(admitted, "digest")
	if err != nil || !retainedSpecDigest.MatchString(admittedDigest) {
		return empty, retainedContractFailure("input-malformed")
	}
	listingRaw, err := s.retainedSelectedControl(ctx, http.MethodGet, nil, retainedSelectedControlBytes)
	if err != nil {
		return empty, err
	}
	report, err := parseRetainedJSON(listingRaw)
	if err != nil || !retainedExactObject(report, []string{"contract", "bound", "profiles"}) {
		return empty, retainedContractFailure("input-malformed")
	}
	contract, err := retainedMemberString(report, "contract")
	bound := report.member("bound")
	if err != nil || contract != retainedSelectedProfileContract || !retainedExactObject(bound, []string{"profiles", "profilesBytes"}) || string(bound.member("profiles").raw) != "16" || string(bound.member("profilesBytes").raw) != "81920" {
		return empty, retainedContractFailure("capability-unsupported")
	}
	listNode := report.member("profiles")
	if listNode.kind != '[' || len(listNode.elements) > 16 {
		return empty, retainedContractFailure("input-malformed")
	}
	var profileDigest string
	seen := map[string]bool{}
	// Preserve every profile's exact bytes, including set-aside documents.
	// Entry wrappers, digests and reasons belong only to the transport envelope.
	profileArray := []byte{'['}
	for index, entry := range listNode.elements {
		if !retainedExactObject(entry, []string{"profile", "digest"}, "state", "reason") {
			return empty, retainedContractFailure("input-malformed")
		}
		if index > 0 {
			profileArray = append(profileArray, ',')
		}
		profileArray = append(profileArray, entry.member("profile").raw...)
		id, idErr := retainedMemberString(entry.member("profile"), "id")
		digest, digestErr := retainedMemberString(entry, "digest")
		if idErr != nil || id == "" || seen[id] || id == reviewedInputProfile || digestErr != nil || !retainedSpecDigest.MatchString(digest) {
			return empty, retainedContractFailure("input-malformed")
		}
		seen[id] = true
		if id != retainedReviewedInputProfile {
			continue
		}
		if entry.member("state") != nil || entry.member("reason") != nil || !retainedJSONEqual(entry.member("profile"), approvedNode) || digest != admittedDigest {
			return empty, retainedContractFailure("capability-unsupported")
		}
		profileDigest = digest
	}
	profileArray = append(profileArray, ']')
	if err := checkRetainedArtifact(admission, "profilesBytes", profileArray); err != nil {
		return empty, err
	}
	if profileDigest == "" {
		return empty, retainedContractFailure("capability-unsupported")
	}
	approved := retainedPrivateProducerApproval{profileDigest: profileDigest, adapterDigest: adapterDigest, admission: admission}
	approved.limitsDigest, err = retainedPrivateApprovalDigest(approved)
	if err != nil {
		return empty, err
	}
	return approved, nil
}

func retainedPrivateApprovalDigest(approved retainedPrivateProducerApproval) (string, error) {
	if approved.admission == nil {
		return "", retainedContractFailure("capability-unsupported")
	}
	if err := retainedValidateAdmission(approved.admission); err != nil {
		return "", err
	}
	encoded, err := json.Marshal(map[string]any{"selector": approved.admission.selection, "limits": approved.admission.limits, "profileDigest": approved.profileDigest, "adapterDigest": approved.adapterDigest})
	if err != nil {
		return "", err
	}
	return sha256Digest(encoded), nil
}

func retainedCheckedCarrier(raw []byte, admission *retainedAdmission) (string, string, []byte, error) {
	if err := checkRetainedArtifact(admission, "carrierBytes", raw); err != nil {
		return "", "", nil, err
	}
	node, err := parseRetainedJSON(raw)
	if err != nil || !retainedExactObject(node, []string{"contractVersion", "originalSnapshotJson", "originalSnapshotSha256", "retained"}) {
		return "", "", nil, retainedContractFailure("input-malformed")
	}
	version, err := retainedMemberString(node, "contractVersion")
	if err != nil || version != "1" {
		return "", "", nil, retainedContractFailure("capability-unsupported")
	}
	original, err := retainedMemberString(node, "originalSnapshotJson")
	if err != nil || checkRetainedArtifact(admission, "originalSnapshotBytes", []byte(original)) != nil {
		return "", "", nil, retainedContractFailure("input-over-bound")
	}
	originalSHA, err := retainedMemberString(node, "originalSnapshotSha256")
	if err != nil || originalSHA != sha256Digest([]byte(original)) {
		return "", "", nil, retainedContractFailure("input-malformed")
	}
	frame := node.member("retained")
	if frame == nil || frame.kind != '{' || int64(len(frame.raw)) > retainedFrameBytes {
		return "", "", nil, retainedContractFailure("input-over-bound")
	}
	return originalSHA, sha256Digest(frame.raw), frame.raw, nil
}

func retainedSelectedGrant(raw []byte, requestDigest, adapterDigest string) (*retainedJSONNode, *retainedAdmission, error) {
	if requestDigest != sha256Digest(raw) {
		return nil, nil, retainedContractFailure("input-malformed")
	}
	node, err := parseRetainedJSON(raw)
	if err != nil || !retainedExactObject(node, []string{"handoffVersion", "channel", "expires", "profileId", "profileDigest", "adapterDigest", "selector", "producerLimits", "carrierSha256", "result"}) {
		return nil, nil, retainedContractFailure("input-malformed")
	}
	channel, _ := retainedMemberString(node, "channel")
	profile, _ := retainedMemberString(node, "profileId")
	digest, _ := retainedMemberString(node, "adapterDigest")
	profileDigest, _ := retainedMemberString(node, "profileDigest")
	carrierSHA, _ := retainedMemberString(node, "carrierSha256")
	expires, _ := retainedMemberString(node, "expires")
	until, timeErr := time.Parse(time.RFC3339Nano, expires)
	if string(node.member("handoffVersion").raw) != "2" || channel != "retained-column/1" || profile != retainedReviewedInputProfile || digest != adapterDigest || !retainedSpecDigest.MatchString(profileDigest) || !retainedSpecDigest.MatchString(carrierSHA) || timeErr != nil || !time.Now().Before(until) {
		return nil, nil, retainedContractFailure("capability-unsupported")
	}
	selection, err := parseRetainedSelector(node.member("selector").raw, "producer-carrier")
	if err != nil || len(selection.Roles) != 1 {
		return nil, nil, retainedContractFailure("capability-unsupported")
	}
	limits, err := retainedReadLimits(node.member("producerLimits"), "producer-carrier")
	if err != nil {
		return nil, nil, err
	}
	admission := &retainedAdmission{selection: selection, limits: map[string]map[string]int64{"producer-carrier": limits}, generated: true}
	if err := retainedValidateAdmission(admission); err != nil {
		return nil, nil, err
	}
	if err := checkRetainedArtifact(admission, "grantBytes", raw); err != nil {
		return nil, nil, err
	}
	result := node.member("result")
	if result == nil || carrierSHA != sha256Digest(result.raw) {
		return nil, nil, retainedContractFailure("input-malformed")
	}
	if _, _, _, err := retainedCheckedCarrier(result.raw, admission); err != nil {
		return nil, nil, err
	}
	return node, admission, nil
}

func runRetainedInputSourceAdapter(root *os.Root, id, requestDigest string, output io.Writer) error {
	raw, err := readPrivateData(root, id+retainedGrantSuffix, int(retainedRoleCaps("producer-carrier")["grantBytes"]))
	if err != nil {
		return errors.New("Reviewed-input grant is unavailable.")
	}
	_, adapter, err := deskInputAdapter()
	if err != nil {
		return err
	}
	node, admission, err := retainedSelectedGrant(raw, requestDigest, adapter["digest"])
	if err != nil {
		return err
	}
	if selected, err := retainedInputGrantSelection(root, id); err != nil || !selected {
		return errors.New("Reviewed-input grant is unavailable.")
	}
	acquisition, err := json.Marshal(map[string]any{"adapter": map[string]string{"name": adapter["name"], "version": "2", "digest": adapter["digest"]}, "endpoint": reviewedInputEndpoint, "statement": nil, "snapshot": sha256Digest(node.member("result").raw), "peerIdentity": nil, "schema": nil, "upstreamToken": nil, "observedAt": time.Now().UTC().Format(time.RFC3339)})
	if err != nil {
		return err
	}
	var stdout bytes.Buffer
	stdout.WriteString(`{"result":`)
	stdout.Write(node.member("result").raw)
	stdout.WriteString(`,"acquisition":`)
	stdout.Write(acquisition)
	stdout.WriteString("}\n")
	if err := checkRetainedArtifact(admission, "adapterStdoutBytes", stdout.Bytes()); err != nil {
		return err
	}
	_, err = output.Write(stdout.Bytes())
	return err
}

func retainedGrantBytes(expires, profileDigest, adapterDigest string, production retainedReviewedInputProduction, admission *retainedAdmission) ([]byte, error) {
	if err := retainedValidateAdmission(admission); err != nil {
		return nil, err
	}
	originalSHA, frameSHA, _, err := retainedCheckedCarrier(production.CarrierJSON, admission)
	if err != nil || originalSHA != production.OriginalSnapshotSHA256 || frameSHA != production.ProjectedFrameSHA256 || !retainedSpecDigest.MatchString(profileDigest) || !retainedSpecDigest.MatchString(adapterDigest) {
		return nil, retainedContractFailure("input-malformed")
	}
	metadata, err := json.Marshal(map[string]any{"handoffVersion": 2, "channel": "retained-column/1", "expires": expires, "profileId": retainedReviewedInputProfile, "profileDigest": profileDigest, "adapterDigest": adapterDigest, "selector": admission.selection, "producerLimits": admission.limits["producer-carrier"], "carrierSha256": sha256Digest(production.CarrierJSON)})
	if err != nil {
		return nil, err
	}
	grant := append(append(append([]byte(nil), metadata[:len(metadata)-1]...), []byte(`,"result":`)...), production.CarrierJSON...)
	grant = append(grant, '}')
	if err := checkRetainedArtifact(admission, "grantBytes", grant); err != nil {
		return nil, err
	}
	return grant, nil
}

// Only a private caller with a freshly checked actual Runner approval may
// reach the existing Gateway signing source. The result is not a D4 input.
func (s *Server) retainedPrivateAcquire(ctx context.Context, doc inputPreparation, record preparedInputRecord, row inputPreparedRow, approved retainedPrivateProducerApproval) (retainedPrivateSignedInput, error) {
	return s.retainedPrivateAcquireUsing(ctx, doc, record, row, approved, nil)
}

// Preparation must check the very production object that will be signed. A
// separate preflight projection could change before the grant is authored.
func (s *Server) retainedPrivateAcquireForPreparation(ctx context.Context, doc inputPreparation, record preparedInputRecord, row inputPreparedRow, approved retainedPrivateProducerApproval, admission *retainedAdmission) (retainedPrivateSignedInput, error) {
	if err := retainedPreparationApproval(admission, approved); err != nil {
		return retainedPrivateSignedInput{}, err
	}
	return s.retainedPrivateAcquireUsing(ctx, doc, record, row, approved, admission)
}

func (s *Server) retainedPrivateAcquireUsing(ctx context.Context, doc inputPreparation, record preparedInputRecord, row inputPreparedRow, approved retainedPrivateProducerApproval, preparation *retainedAdmission) (retainedPrivateSignedInput, error) {
	var empty retainedPrivateSignedInput
	if approved.admission == nil || approved.profileDigest == "" || approved.adapterDigest == "" || s.localGateway == nil {
		return empty, retainedContractFailure("capability-unsupported")
	}
	seal, err := retainedPrivateApprovalDigest(approved)
	if err != nil || seal != approved.limitsDigest {
		return empty, retainedContractFailure("capability-unsupported")
	}
	if _, err := retainedProducerLimits(approved); err != nil {
		return empty, err
	}
	_, adapter, err := deskInputAdapter()
	if err != nil || adapter["digest"] != approved.adapterDigest {
		return empty, retainedContractFailure("capability-unsupported")
	}
	if err := retainedValidateAdmission(approved.admission); err != nil {
		return empty, err
	}
	production, err := s.retainedReviewedInputSnapshot(ctx, doc, record, row)
	if err != nil {
		return empty, err
	}
	if preparation != nil {
		if err := retainedPreparationProduction(production, preparation); err != nil {
			return empty, err
		}
		if err := ctx.Err(); err != nil {
			return empty, err
		}
	}
	grant, err := retainedGrantBytes(time.Now().Add(2*time.Minute).UTC().Format(time.RFC3339Nano), approved.profileDigest, approved.adapterDigest, production, approved.admission)
	if err != nil {
		return empty, err
	}
	pin, err := s.localGateway.ensure(s.assistant)
	if err != nil {
		return empty, err
	}
	root, err := openPrivateDataRoot(s.inputHandoffPath(), true)
	if err != nil {
		return empty, err
	}
	defer root.Close()
	var nonce [16]byte
	if _, err := rand.Read(nonce[:]); err != nil {
		return empty, err
	}
	id := "grant-" + hex.EncodeToString(nonce[:])
	if err := writePrivateData(root, id+retainedGrantSuffix, grant); err != nil {
		return empty, err
	}
	defer root.Remove(id + retainedGrantSuffix)
	requestDigest := sha256Digest(grant)
	requestBody := map[string]any{"session": "review-" + id, "source": reviewedInputSource, "arguments": map[string]any{"tool": reviewedInputTool, "arguments": map[string]string{"grant": id, "digest": requestDigest}}}
	request, err := json.Marshal(requestBody)
	if err != nil {
		return empty, err
	}
	response, err := retainedPrivateAcquireBounded(ctx, pin, request, approved.admission)
	if err != nil {
		return empty, err
	}
	node, err := parseRetainedJSON(response)
	if err != nil || !retainedExactObject(node, []string{"result", "receipt", "salts"}) {
		return empty, retainedContractFailure("input-malformed")
	}
	result := node.member("result")
	receivedOriginalSHA, _, frame, err := retainedCheckedCarrier(result.raw, approved.admission)
	if err != nil {
		return empty, err
	}
	producedNode, err := parseRetainedJSON(production.CarrierJSON)
	if err != nil || receivedOriginalSHA != production.OriginalSnapshotSHA256 || !retainedJSONEqual(result, producedNode) {
		return empty, retainedContractFailure("input-malformed")
	}
	return retainedPrivateSignedInput{Response: response, Request: request, CarrierSHA256: sha256Digest(production.CarrierJSON), OriginalSnapshotSHA256: production.OriginalSnapshotSHA256, ProjectedFrameSHA256: production.ProjectedFrameSHA256, ReceivedCarrierSHA256: sha256Digest(result.raw), ReceivedFrameSHA256: sha256Digest(frame), ProfileDigest: approved.profileDigest}, nil
}

func retainedPrivateAcquireBounded(ctx context.Context, pin *localGatewayPin, request []byte, admission *retainedAdmission) ([]byte, error) {
	if err := retainedValidateAdmission(admission); err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	if err := checkLocalGateway(ctx, pin.URL, pin.Signer.Public); err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, pin.URL+"/acquire", bytes.NewReader(request))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	transport := &http.Transport{Proxy: nil}
	defer transport.CloseIdleConnections()
	client := &http.Client{Transport: transport, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	response, err := readRetainedArtifact(resp.Body, admission, "responseBytes")
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		return nil, errors.New("The local gateway refused the retained reviewed input.")
	}
	return response, nil
}

// Recheck the six lower ceilings before a caller reuses an approval; no
// untrusted report can replace the approved profile or grant's exact limits.
func retainedProducerLimits(approval retainedPrivateProducerApproval) (map[string]int64, error) {
	if approval.admission == nil {
		return nil, retainedContractFailure("capability-unsupported")
	}
	limits := approval.admission.limits["producer-carrier"]
	if len(limits) != len(retainedRoleCaps("producer-carrier")) {
		return nil, retainedContractFailure("input-malformed")
	}
	out := map[string]int64{}
	for key, cap := range retainedRoleCaps("producer-carrier") {
		value := limits[key]
		if value < 1 || value > cap {
			return nil, retainedContractFailure("input-malformed")
		}
		out[key] = value
	}
	return out, nil
}
