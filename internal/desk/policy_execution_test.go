package desk

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

type executionFixture struct {
	s               *Server
	core            policyExecutionService
	policy          policyService
	actor           policyActor
	registration    policyExecutionRegisteredResult
	registerRequest policyExecutionRegisterRequest
	clock           *time.Time
	session         policyExecutionSession
	adoption        policyMutationReply
}

func newExecutionFixture(t *testing.T) *executionFixture {
	t.Helper()
	ctx := context.Background()
	s, token, service, actor, q := adoptionFixture(t)
	second := policyApprove(t, s, token, policyCreate(t, s, token))
	second = policyMutation(t, policyCall(s, token, "POST", "/api/policies/"+second.Document.ID+"/publish", policyPublishBody(t, second))).Policy
	q.Coverages = append(q.Coverages, adoptionApprovedCoverage(t, service, actor, second, *q.Pack))
	prepared, err := service.adoptionCommand(ctx, actor, q)
	if err != nil {
		t.Fatal(err)
	}
	approved, err := service.adoptionCommand(ctx, actor, adoptionReviewRequest(t, prepared))
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2030, 1, 1, 12, 0, 0, 0, time.UTC)
	service.clock = func() time.Time { return now }
	for _, d := range approved.Adoption.Document.Candidate.Dependencies {
		p, err := service.get(ctx, d.Selection.PolicyID)
		if err != nil {
			t.Fatal(err)
		}
		lifecycleDo(t, service, p, lifecycleBody(t, service, p, "activate"))
	}
	stopAuthorityStoreWorker(s)
	identity := policyExecutionIdentity{strings.Repeat("a", 64), strings.Repeat("b", 64), "runner-distinct", strings.Repeat("c", 64)}
	anchor, err := newPolicyAuthorityAnchor(filepath.Join(t.TempDir(), "authority"), policyAuthorityAnchorIdentity{identity.InstallationID, identity.MachineWorkspace})
	if err != nil {
		t.Fatal(err)
	}
	root, err := s.openPolicies(false)
	if err != nil {
		t.Fatal(err)
	}
	raw, err := readPrivateData(root, "ledger.json", policyLedgerLimit)
	root.Close()
	if err != nil {
		t.Fatal(err)
	}
	if err = anchor.Enroll(ctx, raw); err != nil {
		t.Fatal(err)
	}
	repository := filePolicyRepository{open: s.openPolicies, anchor: anchor}
	service.repository = repository
	pack, err := s.artifactRepository().Get(ctx, q.Pack.Content)
	if err != nil {
		t.Fatal(err)
	}
	releaseRaw, _ := json.Marshal(map[string]any{"schemaVersion": "1", "id": "release_fixture", "pack": string(pack), "packDigest": "sha256:" + digestOf(pack), "packId": q.Pack.ArtifactID, "packVersion": q.Pack.Version, "title": "Fixture", "runtimeDigest": "sha256:" + strings.Repeat("4", 64), "createdAt": "2030-01-01T12:00:00Z", "config": "{}", "lock": "{}", "validation": map[string]any{}, "sample": map[string]any{"facts": false}, "preview": map[string]any{}, "tests": "passed"})
	releaseRaw, err = gatewayCanonicalJSON(ctx, releaseRaw)
	if err != nil {
		t.Fatal(err)
	}
	release := policyExecutionResolvedRelease{identity.RunnerWorkspace, policyExecutionResource{"job_fixture", "1", "release_fixture", policyExecutionReleaseProfile, "sha256:" + digestOf(releaseRaw)}, releaseRaw}
	core := policyExecutionService{repository: repository, artifacts: s.artifactRepository(), identity: identity, authorize: func() bool { return true }, clock: func() time.Time { return now }, resolveRelease: func(context.Context, string) (policyExecutionResolvedRelease, error) { return release, nil }, resolveAdoption: resolvePolicyExecutionAdoption(s.artifactRepository())}
	if err = core.initialize(ctx); err != nil {
		t.Fatal(err)
	}
	register := policyExecutionRegisterRequest{policyOp(t), "job_fixture", approved.Adoption.Document.ID, approved.Adoption.Document.CurrentReviewID}
	registration, err := core.register(ctx, register)
	if err != nil {
		t.Fatal(err)
	}
	session := policyExecutionSession{identity, strings.Repeat("e", 64), strings.Repeat("d", 64), func() error { return nil }}
	activated, err := core.activate(ctx, session, policyExecutionActivateRequest{session.challenge, policyExecutionSigning{Mode: "off"}})
	if err != nil || activated.Delivery != "committed" {
		t.Fatal(activated, err)
	}
	return &executionFixture{s, core, service, actor, registration, register, &now, session, approved}
}
func (f *executionFixture) raw(t *testing.T) []byte {
	t.Helper()
	root, err := f.s.openPolicies(false)
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	raw, err := readPrivateData(root, "ledger.json", policyLedgerLimit)
	if err != nil {
		t.Fatal(err)
	}
	return raw
}
func (f *executionFixture) ledger(t *testing.T) policyLedger {
	t.Helper()
	l, err := decodePolicyLedger(f.raw(t))
	if err != nil {
		t.Fatal(err)
	}
	return l
}
func (f *executionFixture) intent(t *testing.T, key string, raw string) (policyExecutionIntent, policyExecutionPayload) {
	t.Helper()
	i, delivery, err := f.core.prepareIntent(context.Background(), f.registration.Resource, f.registration.Registration, key, []byte(raw))
	if err != nil || delivery != "committed" {
		t.Fatal(i, delivery, err)
	}
	r := f.ledger(t).Execution.Resources[i.ResourceID]
	ref := func(r retainedArtifactRef) policyExecutionExactBytes {
		return policyExecutionExactBytes{"exact-bytes", "sha256:" + r.SHA256, r.Bytes}
	}
	d := policyExecutionDescriptor{policyExecutionPayloadProfile, r.Content.Resource.ReleaseDigest, "sha256:" + r.Content.Pack.SHA256, r.Content.RuntimeDigest, ref(i.Facts), policyExecutionEvidence{Presence: "omitted"}, policyExecutionSigning{Mode: "off"}}
	if i.Evidence != nil {
		v := ref(*i.Evidence)
		d.Evidence = policyExecutionEvidence{"supplied", &v}
	}
	b, _ := json.Marshal(d)
	canonical, err := gatewayCanonicalJSON(context.Background(), b)
	if err != nil {
		t.Fatal(err)
	}
	return i, policyExecutionPayload{policyExecutionPayloadProfile, "sha256:" + digestOf(canonical), base64.StdEncoding.EncodeToString(canonical)}
}
func (f *executionFixture) reserve(t *testing.T, key string) (policyExecutionReserveRequest, policyExecutionReserveResult) {
	t.Helper()
	i, p := f.intent(t, key, `{"facts":false}`)
	q := policyExecutionReserveRequest{i.ID, i.Resource, i.Registration, p, "sha256:" + i.Request.SHA256}
	r, err := f.core.reserve(context.Background(), f.session, q)
	if err != nil {
		t.Fatal(err)
	}
	return q, r
}
func executionStart(q policyExecutionReserveRequest, r policyExecutionReserveResult) policyExecutionStartRequest {
	return policyExecutionStartRequest{q.IntentID, q.Resource, q.Registration, q.Payload, r.OperationID, r.AttemptID, r.Epoch, q.RawRequestDigest}
}
func (f *executionFixture) withdraw(t *testing.T) {
	t.Helper()
	d := f.adoption.Adoption.Document.Candidate.Dependencies[0]
	p, err := f.policy.get(context.Background(), d.Selection.PolicyID)
	if err != nil {
		t.Fatal(err)
	}
	q := lifecycleBody(t, f.policy, p, "withdraw")
	q.Rationale = "Operator withdrew this publication"
	lifecycleDo(t, f.policy, p, q)
}

func TestPolicyExecutionImmutableClosureAndExactManualIntent(t *testing.T) {
	f := newExecutionFixture(t)
	ctx := context.Background()
	registered, err := f.core.register(ctx, f.registerRequest)
	if err != nil || registered.Delivery != "recovered" || registered.Registration != f.registration.Registration {
		t.Fatal(registered, err)
	}
	unavailable := f.core
	unavailable.resolveRelease = func(context.Context, string) (policyExecutionResolvedRelease, error) {
		t.Fatal("recovery fetched mutable Runner state")
		return policyExecutionResolvedRelease{}, nil
	}
	if _, err = unavailable.register(ctx, f.registerRequest); err != nil {
		t.Fatal("registration recovery depended on Runner availability", err)
	}
	changed := f.registerRequest
	changed.OperationID = policyOp(t)
	_, err = f.core.register(ctx, changed)
	lifecycleRefused(t, err, "resource-changed")
	for n, raw := range []string{`{"facts":false}`, `{"facts":0}`, `{"facts":null}`, `{"facts":{ "n":9007199254740993.00,"x":1e+2 },"evidence":null}`} {
		key := string(rune('a' + n))
		i, p := f.intent(t, key, raw)
		facts, _ := f.core.artifacts.Get(ctx, i.Facts)
		if !bytes.Contains([]byte(raw), facts) || i.ID != executionIntentID(f.core.identity, i.Resource.JobID, key) || !executionPayloadShape(p) {
			t.Fatal("input bytes changed", string(facts), i)
		}
		if n == 3 && (i.Evidence == nil || i.Evidence.SHA256 != digestOf([]byte("null")) || !bytes.Contains(facts, []byte("9007199254740993.00"))) {
			t.Fatal("lost exact input presence/lexeme")
		}
		again, delivery, err := f.core.prepareIntent(ctx, i.Resource, i.Registration, key, []byte(raw))
		if err != nil || delivery != "recovered" || again.ID != i.ID {
			t.Fatal(again, delivery, err)
		}
		_, _, err = f.core.prepareIntent(ctx, i.Resource, i.Registration, key, []byte(`{"facts":true}`))
		lifecycleRefused(t, err, "binding-conflict")
		changed := i.Resource
		changed.JobRevision = "2"
		_, _, err = f.core.prepareIntent(ctx, changed, i.Registration, key, []byte(raw))
		lifecycleRefused(t, err, "binding-conflict")
	}
	for _, raw := range []string{`{"facts":false,"source":{}}`, `{"facts":false,"preparation":{}}`, `{"facts":false,"facts":true}`, `{"Facts":false}`, `{"facts":"\ud800"}`} {
		_, _, err = f.core.prepareIntent(ctx, f.registration.Resource, f.registration.Registration, "refused", []byte(raw))
		if err == nil {
			t.Fatal("accepted unsupported input", raw)
		}
	}
	view, err := f.core.inspect(ctx, f.session, f.registration.Resource)
	if err != nil || !view.Eligible || view.Delivery != "observed" {
		t.Fatal(view, err)
	}
	// A later unrelated policy draft does not alter the registered publication.
	p, err := f.policy.get(ctx, f.adoption.Adoption.Document.Candidate.Dependencies[0].Selection.PolicyID)
	if err != nil {
		t.Fatal(err)
	}
	q := policyEditBody(t, p)
	q.Narrative = "Unrelated future draft"
	_, err = f.policy.execute(ctx, policyCommand{Kind: "save-draft", PolicyID: p.Document.ID, OperationID: q.OperationID, RequestDigest: strings.Repeat("7", 64), Actor: f.actor, Edit: &q})
	if err != nil {
		t.Fatal(err)
	}
	view, err = f.core.inspect(ctx, f.session, f.registration.Resource)
	if err != nil || !view.Eligible {
		t.Fatal("unrelated draft revoked exact publication", view, err)
	}
}

func TestPolicyExecutionStartWithdrawalOrderingAndRecovery(t *testing.T) {
	for _, withdrawFirst := range []bool{true, false} {
		t.Run(map[bool]string{true: "withdraw-first", false: "commit-first"}[withdrawFirst], func(t *testing.T) {
			f := newExecutionFixture(t)
			ctx := context.Background()
			q, r := f.reserve(t, "logical-key")
			start := executionStart(q, r)
			if withdrawFirst {
				f.withdraw(t)
			}
			if !withdrawFirst {
				// Valid historical JSON whitespace is part of the checked head.
				root, e := f.s.openPolicies(false)
				if e != nil {
					t.Fatal(e)
				}
				exact := f.raw(t)
				var pretty bytes.Buffer
				_ = json.Indent(&pretty, exact, "", "  ")
				e = f.core.repository.anchor.WithCurrent(ctx, exact, func(h *policyAuthorityAnchorHeld) error {
					return h.Advance(ctx, pretty.Bytes(), func(next []byte) error { return writePrivateData(root, "ledger.json", next) })
				})
				root.Close()
				if e != nil {
					t.Fatal(e)
				}
			}
			before := f.raw(t)
			result, err := f.core.commitStart(ctx, f.session, start)
			if err != nil || result.Delivery != "committed" || result.AuthorityRevision != digestOf(before) {
				t.Fatal(result, err)
			}
			want := "start-committed"
			if withdrawFirst {
				want = "blocked"
			}
			if result.Decision != want || withdrawFirst && result.Reason != "withdrawn" {
				t.Fatal(result)
			}
			if !withdrawFirst {
				f.withdraw(t)
			}
			recovered, err := f.core.commitStart(ctx, f.session, start)
			result.Delivery = "recovered"
			if err != nil || recovered != result {
				t.Fatal("recovery changed historical result", recovered, result, err)
			}
			h, err := f.core.history(ctx, f.session, q.IntentID)
			if err != nil || h.Delivery != "observed" || h.State != want || h.RequestDigest != result.RequestDigest {
				t.Fatal(h, err)
			}
		})
	}
}

func TestPolicyExecutionRecoveredReserveIsHeldAndEpochCannotReopen(t *testing.T) {
	f := newExecutionFixture(t)
	ctx := context.Background()
	q, r := f.reserve(t, "one-attempt")
	before := f.ledger(t).Execution.Attempts[q.IntentID]
	*f.clock = f.clock.Add(time.Second)
	recovered, err := f.core.reserve(ctx, f.session, q)
	if err != nil || recovered.Delivery != "recovered" || recovered.OperationID != r.OperationID || recovered.AttemptID != r.AttemptID {
		t.Fatal(recovered, err)
	}
	after := f.ledger(t).Execution.Attempts[q.IntentID]
	if after.Reserved != before.Reserved || after.At != before.At || after.Hold == nil {
		t.Fatal("recovery rewrote original receipt", before, after)
	}
	_, err = f.core.commitStart(ctx, f.session, executionStart(q, r))
	lifecycleRefused(t, err, "operation-held")
	second := f.session
	second.id = strings.Repeat("f", 64)
	second.challenge = strings.Repeat("1", 64)
	activated, err := f.core.activate(ctx, second, policyExecutionActivateRequest{second.challenge, policyExecutionSigning{Mode: "off"}})
	if err != nil || activated.Epoch != "2" {
		t.Fatal(activated, err)
	}
	_, err = f.core.reserve(ctx, second, q)
	lifecycleRefused(t, err, "operation-held")
	_, err = f.core.commitStart(ctx, f.session, executionStart(q, r))
	lifecycleRefused(t, err, "stale-epoch")
	h, err := f.core.history(ctx, second, q.IntentID)
	if err != nil || h.State != "blocked" || h.Reason != "operation-held" || h.Epoch != "1" {
		t.Fatal(h, err)
	}
	if len(f.ledger(t).Execution.Attempts) != 1 {
		t.Fatal("new epoch created another attempt")
	}
}

func TestPolicyExecutionTimeHighWaterSurvivesBlockedAndDeniedCommands(t *testing.T) {
	f := newExecutionFixture(t)
	ctx := context.Background()
	q, r := f.reserve(t, "expiry-at-start")
	// Expiry is exclusive. Record it on the actual activation, preserving the
	// same historical publication/adoption identities in this fixture.
	err := f.core.repository.Update(ctx, func(l *policyLedger) (bool, error) {
		id := f.adoption.Adoption.Document.Candidate.Dependencies[0].Selection.PolicyID
		l.Lifecycles[id].Events[0].ExpiresAt = "2030-01-01T13:00:00Z"
		return true, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	*f.clock = time.Date(2030, 1, 1, 13, 0, 0, 0, time.UTC)
	blocked, err := f.core.commitStart(ctx, f.session, executionStart(q, r))
	if err != nil || blocked.Decision != "blocked" || blocked.Reason != "expired" {
		t.Fatal(blocked, err)
	}
	if f.ledger(t).Execution.LastTime != "2030-01-01T13:00:00Z" {
		t.Fatal("expiry observation lost")
	}
	*f.clock = time.Date(2030, 1, 1, 12, 30, 0, 0, time.UTC)
	_, err = f.core.commitStart(ctx, f.session, executionStart(q, r))
	lifecycleRefused(t, err, "clock-unavailable")
	*f.clock = time.Date(2030, 1, 1, 14, 0, 0, 0, time.UTC)
	_, _, err = f.core.prepareIntent(ctx, f.registration.Resource, f.registration.Registration, "denied-after-expiry", []byte(`{"facts":0}`))
	lifecycleRefused(t, err, "eligibility-blocked")
	if f.ledger(t).Execution.LastTime != "2030-01-01T14:00:00Z" {
		t.Fatal("denied command forgot observed time")
	}
	*f.clock = time.Date(2030, 1, 1, 12, 45, 0, 0, time.UTC)
	_, _, err = f.core.prepareIntent(ctx, f.registration.Resource, f.registration.Registration, "denied-after-expiry", []byte(`{"facts":0}`))
	lifecycleRefused(t, err, "clock-unavailable")
}

func TestPolicyExecutionConcurrentOneAttemptAndUncertainPersistence(t *testing.T) {
	t.Run("one-attempt", func(t *testing.T) {
		f := newExecutionFixture(t)
		i, p := f.intent(t, "same-op", `{"facts":false}`)
		q := policyExecutionReserveRequest{i.ID, i.Resource, i.Registration, p, "sha256:" + i.Request.SHA256}
		var wg sync.WaitGroup
		var results [2]policyExecutionReserveResult
		var errs [2]error
		for n := range results {
			wg.Add(1)
			go func(n int) { defer wg.Done(); results[n], errs[n] = f.core.reserve(context.Background(), f.session, q) }(n)
		}
		wg.Wait()
		if errs[0] != nil || errs[1] != nil || results[0].AttemptID != results[1].AttemptID || results[0].OperationID != results[1].OperationID || len(f.ledger(t).Execution.Attempts) != 1 {
			t.Fatal(results, errs)
		}
		if f.ledger(t).Execution.Attempts[i.ID].Hold == nil {
			t.Fatal("recovered concurrency did not hold")
		}
	})
	for _, phase := range []policyAuthorityAnchorWritePhase{policyAuthorityAnchorIntentWrite, policyAuthorityAnchorFinalWrite} {
		t.Run(string(phase), func(t *testing.T) {
			f := newExecutionFixture(t)
			q, r := f.reserve(t, "faulted-start")
			f.core.repository.anchor.persistForTest = func(p policyAuthorityAnchorWritePhase, root *os.Root, raw []byte) error {
				if err := writePrivateData(root, policyAuthorityAnchorName, raw); err != nil {
					return err
				}
				if p == phase {
					return errors.New("controlled lost persistence acknowledgement")
				}
				return nil
			}
			result, err := f.core.commitStart(context.Background(), f.session, executionStart(q, r))
			var failure *policyFailure
			if !errors.As(err, &failure) || !failure.Uncertain {
				t.Fatal("uncertain write returned permission", result, err)
			}
			f.core.repository.anchor.persistForTest = nil
			_, err = f.core.commitStart(context.Background(), f.session, executionStart(q, r))
			if err == nil {
				t.Fatal("uncertain channel automatically recovered")
			}
			// Retained history can diagnose exact state without becoming current.
			reader := filePolicyHistoryReader{open: f.s.openPolicies, requireExisting: true}
			if err = reader.ReadHistory(context.Background(), func(l policyLedger) error {
				a := l.Execution.Attempts[q.IntentID]
				if phase == policyAuthorityAnchorFinalWrite && (a.Start == nil || a.Start.Decision != "start-committed") {
					t.Fatal("durable lost reply not retained")
				}
				return nil
			}); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestPolicyExecutionSchemaClosedAndRecoveryCannotStripState(t *testing.T) {
	f := newExecutionFixture(t)
	ctx := context.Background()
	raw := f.raw(t)
	ledger := f.ledger(t)
	for name, edit := range map[string]func(map[string]any){
		"null":          func(m map[string]any) { m["execution"] = nil },
		"missing":       func(m map[string]any) { delete(m, "execution") },
		"downgrade":     func(m map[string]any) { m["schemaVersion"] = 2 },
		"case":          func(m map[string]any) { m["Execution"] = m["execution"]; delete(m, "execution") },
		"unknown":       func(m map[string]any) { m["execution"].(map[string]any)["permit"] = true },
		"missing-map":   func(m map[string]any) { delete(m["execution"].(map[string]any), "attempts") },
		"null-map":      func(m map[string]any) { m["execution"].(map[string]any)["intents"] = nil },
		"fraction-time": func(m map[string]any) { m["execution"].(map[string]any)["lastTime"] = "2030-01-01T12:00:00.1Z" },
	} {
		t.Run(name, func(t *testing.T) {
			var value map[string]any
			_ = json.Unmarshal(raw, &value)
			edit(value)
			data, _ := json.Marshal(value)
			if _, err := decodePolicyLedger(data); err == nil {
				t.Fatal("accepted malformed execution state")
			}
		})
	}
	if _, err := decodePolicyLedger(bytes.Replace(raw, []byte(`"execution":{`), []byte(`"execution":{"version":1,`), 1)); err == nil {
		t.Fatal("accepted duplicate")
	}
	_, err := policyArchiveClosure(ctx, ledger, f.core.artifacts)
	lifecycleRefused(t, err, "archive-unsupported")
	err = restorePolicyRecoveryBarrier(&ledger, policyRecoverySnapshot{})
	lifecycleRefused(t, err, "archive-unsupported")
	if !bytes.Equal(raw, f.raw(t)) {
		t.Fatal("unsupported recovery mutated authority")
	}
	legacy := emptyPolicyLedger()
	legacyRaw, _ := json.Marshal(legacy)
	if _, err = decodePolicyLedger(legacyRaw); err != nil {
		t.Fatal(err)
	}
	legacyRaw = bytes.Replace(legacyRaw, []byte(`"schemaVersion":2`), []byte(`"schemaVersion":2,"execution":null`), 1)
	if _, err = decodePolicyLedger(legacyRaw); err == nil {
		t.Fatal("legacy explicit execution null accepted")
	}
}

func TestPolicyExecutionSharedWireDigestsAndExactDescriptor(t *testing.T) {
	raw, err := os.ReadFile("../../docs/design/policy-manual-admission-wire.json")
	if err != nil {
		t.Fatal(err)
	}
	var fixture struct {
		Vectors []struct {
			Name    string `json:"name"`
			Request struct {
				Method string          `json:"method"`
				Body   json.RawMessage `json:"body"`
			} `json:"request"`
			RequestDigest string `json:"requestDigest"`
		} `json:"vectors"`
		Synthetic struct {
			DescriptorBytes string `json:"descriptorBytes"`
		} `json:"synthetic"`
	}
	if err = json.Unmarshal(raw, &fixture); err != nil {
		t.Fatal(err)
	}
	count := 0
	for _, v := range fixture.Vectors {
		if v.Request.Method == "" {
			continue
		}
		count++
		if got := executionRequestDigest(v.Request.Method, v.Request.Body); got != v.RequestDigest {
			t.Fatal(v.Name, got, v.RequestDigest)
		}
	}
	if count != 5 {
		t.Fatal("shared fixture methods changed")
	}
	descriptor, err := base64.StdEncoding.DecodeString(fixture.Synthetic.DescriptorBytes)
	if err != nil {
		t.Fatal(err)
	}
	if !executionPayloadShape(policyExecutionPayload{policyExecutionPayloadProfile, "sha256:" + digestOf(descriptor), fixture.Synthetic.DescriptorBytes}) {
		t.Fatal("shared exact descriptor refused")
	}
}

func TestPolicyExecutionLifecycleSupersessionAndDeniedTime(t *testing.T) {
	f := newExecutionFixture(t)
	ctx := context.Background()
	dependency := f.adoption.Adoption.Document.Candidate.Dependencies[0]
	p, err := f.policy.get(ctx, dependency.Selection.PolicyID)
	if err != nil {
		t.Fatal(err)
	}
	edit := policyEditBody(t, p)
	edit.ArtifactVersion = "2.0.0"
	edit.Narrative = "New publication supersedes the registered one"
	command := func(q policyCommand) policyMutationReply {
		t.Helper()
		q.Actor = f.actor
		q.PolicyID = p.Document.ID
		raw, _ := json.Marshal(q)
		q.RequestDigest = digestOf(raw)
		result, err := f.policy.execute(ctx, q)
		if err != nil {
			t.Fatal(err)
		}
		return result
	}
	draft := command(policyCommand{Kind: "save-draft", OperationID: edit.OperationID, Edit: &edit})
	review := policyReviewBody(t, draft.Policy, "approve")
	approved := command(policyCommand{Kind: "review", OperationID: review.OperationID, Review: &review})
	publish := policyPublishBody(t, approved.Policy)
	published := command(policyCommand{Kind: "publish", OperationID: publish.OperationID, Publish: &publish})
	*f.clock = f.clock.Add(time.Hour)
	q := lifecycleBody(t, f.policy, published.Policy, "activate")
	q.SupersedesPublicationID = dependency.PolicyPublicationID
	lifecycleDo(t, f.policy, published.Policy, q)
	if f.ledger(t).Execution.LastTime != "2030-01-01T13:00:00Z" {
		t.Fatal("lifecycle did not share high water")
	}
	view, err := f.core.inspect(ctx, f.session, f.registration.Resource)
	if err != nil || view.Eligible || view.Reason != "superseded" {
		t.Fatal(view, err)
	}
	*f.clock = f.clock.Add(-time.Minute)
	_, err = f.core.inspect(ctx, f.session, f.registration.Resource)
	lifecycleRefused(t, err, "clock-unavailable")
	*f.clock = time.Date(2030, 1, 1, 14, 0, 0, 0, time.UTC)
	denied := lifecycleBody(t, f.policy, published.Policy, "withdraw")
	denied.Rationale = "Operator restriction"
	denied.BaseLifecycleRevision = strings.Repeat("0", 64)
	before := f.ledger(t)
	_, err = f.policy.lifecycleCommand(ctx, p.Document.ID, f.actor, denied)
	lifecycleRefused(t, err, "lifecycle-conflict")
	after := f.ledger(t)
	if after.Execution.LastTime != "2030-01-01T14:00:00Z" || !samePolicyStatusValue(before.Lifecycles, after.Lifecycles) || !samePolicyStatusValue(before.Operations, after.Operations) {
		t.Fatal("denied lifecycle lost time or changed domain state")
	}
	*f.clock = time.Date(2030, 1, 1, 15, 0, 0, 0, time.UTC)
	malformed := denied
	malformed.Rationale = ""
	_, err = f.policy.lifecycleCommand(ctx, p.Document.ID, f.actor, malformed)
	if err == nil {
		t.Fatal("accepted malformed withdrawal")
	}
	if f.ledger(t).Execution.LastTime != "2030-01-01T14:00:00Z" {
		t.Fatal("malformed command wrote authority")
	}
	unauthorized := f.policy
	unauthorized.authorize = func() bool { return false }
	_, err = unauthorized.lifecycleCommand(ctx, p.Document.ID, f.actor, denied)
	if err == nil || f.ledger(t).Execution.LastTime != "2030-01-01T14:00:00Z" {
		t.Fatal("unauthorized command wrote authority", err)
	}
}

func TestPolicyExecutionBindingRefusalAndReserveDenialBarrier(t *testing.T) {
	f := newExecutionFixture(t)
	ctx := context.Background()
	i, p := f.intent(t, "denied-reserve", `{"facts":false,"evidence":null}`)
	q := policyExecutionReserveRequest{i.ID, i.Resource, i.Registration, p, "sha256:" + i.Request.SHA256}
	changedRaw := q
	changedRaw.RawRequestDigest = "sha256:" + digestOf([]byte(`{ "facts":false,"evidence":null}`))
	_, err := f.core.reserve(ctx, f.session, changedRaw)
	lifecycleRefused(t, err, "binding-conflict")
	if len(f.ledger(t).Execution.Attempts) != 0 {
		t.Fatal("different original body allocated an attempt")
	}
	var d policyExecutionDescriptor
	raw, _ := base64.StdEncoding.DecodeString(p.Bytes)
	_ = json.Unmarshal(raw, &d)
	d.Facts.Digest = "sha256:" + strings.Repeat("0", 64)
	wrong, _ := json.Marshal(d)
	wrong, _ = gatewayCanonicalJSON(ctx, wrong)
	changed := q
	changed.Payload = policyExecutionPayload{p.Profile, "sha256:" + digestOf(wrong), base64.StdEncoding.EncodeToString(wrong)}
	_, err = f.core.reserve(ctx, f.session, changed)
	lifecycleRefused(t, err, "binding-conflict")
	if len(f.ledger(t).Execution.Attempts) != 0 {
		t.Fatal("invalid payload allocated an attempt")
	}
	wrongSession := f.session
	wrongSession.owned = nil
	_, err = f.core.activate(ctx, wrongSession, policyExecutionActivateRequest{wrongSession.challenge, policyExecutionSigning{Mode: "off"}})
	lifecycleRefused(t, err, "unauthorized")
	wrongSession = f.session
	wrongSession.id = strings.Repeat("f", 64)
	wrongSession.owned = func() error { return errors.New("lease not held") }
	_, err = f.core.activate(ctx, wrongSession, policyExecutionActivateRequest{wrongSession.challenge, policyExecutionSigning{Mode: "off"}})
	lifecycleRefused(t, err, "authority-unavailable")
	if len(f.ledger(t).Execution.Epochs) != 1 {
		t.Fatal("unowned activation allocated epoch")
	}
	f.withdraw(t)
	_, err = f.core.reserve(ctx, f.session, q)
	lifecycleRefused(t, err, "eligibility-blocked")
	h, err := f.core.history(ctx, f.session, i.ID)
	if err != nil || h.State != "blocked" || h.Reason != "withdrawn" || h.RequestDigest != executionRequestDigest("reserve", q) {
		t.Fatal(h, err)
	}
	_, err = f.core.reserve(ctx, f.session, q)
	lifecycleRefused(t, err, "operation-held")
	if len(f.ledger(t).Execution.Attempts) != 1 {
		t.Fatal("denied reserve barrier lost")
	}
}

func TestPolicyExecutionDeniedTimeStorageFailureHolds(t *testing.T) {
	f := newExecutionFixture(t)
	ctx := context.Background()
	f.withdraw(t)
	*f.clock = f.clock.Add(time.Hour)
	f.core.repository.anchor.persistForTest = func(policyAuthorityAnchorWritePhase, *os.Root, []byte) error {
		return errors.New("controlled denial-time persistence failure")
	}
	_, _, err := f.core.prepareIntent(ctx, f.registration.Resource, f.registration.Registration, "denied-uncertain", []byte(`{"facts":false}`))
	var failure *policyFailure
	if !errors.As(err, &failure) || !failure.Uncertain || failure.Code == "eligibility-blocked" {
		t.Fatal("domain denial hid uncertain time write", err)
	}
	f.core.repository.anchor.persistForTest = nil
	*f.clock = f.clock.Add(-time.Hour)
	_, _, err = f.core.prepareIntent(ctx, f.registration.Resource, f.registration.Registration, "denied-uncertain", []byte(`{"facts":false}`))
	if err == nil {
		t.Fatal("uncertain time write retried automatically")
	}
}

func TestPolicyExecutionPersistedDecisionBindingsAreClosed(t *testing.T) {
	f := newExecutionFixture(t)
	q, r := f.reserve(t, "closed-decision")
	_, err := f.core.commitStart(context.Background(), f.session, executionStart(q, r))
	if err != nil {
		t.Fatal(err)
	}
	for name, mutate := range map[string]func(*policyExecutionAttempt){
		"reason":   func(a *policyExecutionAttempt) { a.Start.Reason = "eligible"; a.Reason = "eligible" },
		"decision": func(a *policyExecutionAttempt) { a.Start.Decision = "blocked"; a.State = "blocked" },
		"raw-body": func(a *policyExecutionAttempt) { a.Reserve.RawRequestDigest = "sha256:" + strings.Repeat("0", 64) },
		"epoch":    func(a *policyExecutionAttempt) { a.Reserved.Epoch = "18446744073709551616" },
	} {
		t.Run(name, func(t *testing.T) {
			l := f.ledger(t)
			a := l.Execution.Attempts[q.IntentID]
			mutate(&a)
			l.Execution.Attempts[q.IntentID] = a
			if validatePolicyLedger(l) == nil {
				t.Fatal("accepted contradictory decision binding")
			}
		})
	}
	for _, raw := range []string{`{"mode":"off","publicKey":""}`, `{"mode":"off","Mode":"off"}`, `{"mode":"ed25519","publicKey":null}`} {
		var signing policyExecutionSigning
		if executionDecode([]byte(raw), &signing) == nil {
			t.Fatal("accepted malformed signing", raw)
		}
	}
}
