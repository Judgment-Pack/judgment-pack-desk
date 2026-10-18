package desk

import (
	"context"
	"encoding/json"
	"errors"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

type policySourceFixture struct {
	s           *Server
	token       string
	policy      policyReply
	binding     policySourceBinding
	observation json.RawMessage
	calls       []string
	revoked     bool
}

func newPolicySourceFixture(t *testing.T) *policySourceFixture {
	t.Helper()
	s, token := policyTestServer(t)
	s.policySourceCancel()
	<-s.policySourceDone
	s.policySourceCancel = nil
	f := &policySourceFixture{s: s, token: token, policy: policyCreate(t, s, token)}
	s.policySourceCanon = func(_ context.Context, raw []byte) ([]byte, error) {
		out := canonicalApprovalJSON(raw)
		if out == nil {
			return nil, errors.New("invalid JSON")
		}
		return out, nil
	}
	s.policySourceCall = func(_ context.Context, method string, params any) (json.RawMessage, error) {
		f.calls = append(f.calls, method)
		switch method {
		case "source-bind":
			q := params.(map[string]string)
			f.binding = policySourceBinding{Version: 1, OperationID: q["operationId"], BindingID: digestOf([]byte(q["operationId"])), Source: policyConnectedIdentity{Provider: "google-drive", AccountID: "account-A", ResourceID: q["resourceId"]}, CreatedAt: "2026-10-18T06:40:13Z", State: "active", Coverage: "snapshot-only", Lifecycle: policySourceLifecycle{Kind: "not-exposed"}}
			f.observation = policyObservationFixture(t, s, f.binding, "", "2026-10-18T06:40:13Z", 0)
			return json.Marshal(f.binding)
		case "source-observe":
			return f.observation, nil
		case "source-bindings":
			return json.Marshal(map[string]any{"version": 1, "bindings": []policySourceBinding{f.binding}})
		case "source-revoke":
			f.revoked = true
			return json.RawMessage(`{"revoked":true}`), nil
		}
		return nil, errors.New("unexpected operation")
	}
	return f
}
func policyObservationFixture(t *testing.T, s *Server, b policySourceBinding, text, at string, duration int) json.RawMessage {
	t.Helper()
	raw, err := os.ReadFile("testdata/policy-source-observation.json")
	if err != nil {
		t.Fatal(err)
	}
	var o policyGatewayObservation
	if json.Unmarshal(raw, &o) != nil {
		t.Fatal("bad fixture")
	}
	o.BindingID, o.Source, o.ObservedAt, o.ObservationID = b.BindingID, b.Source, at, digestOf([]byte(b.BindingID+at+text+string(rune(duration))))
	var record map[string]any
	_ = json.Unmarshal(o.Document, &record)
	v := record["provenance"].(map[string]any)
	v["observedAt"] = at
	v["source"].(map[string]any)["fileId"] = b.Source.ResourceID
	record["processing"].(map[string]any)["durationMs"] = duration
	if text != "" {
		content := record["content"].(map[string]any)
		pages := content["pages"].([]any)
		page := pages[0].(map[string]any)
		page["text"], page["chars"] = text, len([]rune(text))
		content["chars"] = len([]rune(text))
	}
	raw, _ = json.Marshal(record)
	raw, err = s.canonicalPolicySource(context.Background(), raw)
	if err != nil {
		t.Fatal(err)
	}
	o.Document = raw
	o.Extraction.Content.BytesDigest = "sha256:" + digestOf(raw)
	o.Extraction.Content.ByteLength = len(raw)
	out, _ := json.Marshal(o)
	return out
}
func (f *policySourceFixture) sync(t *testing.T) policySourceSyncReply {
	t.Helper()
	w := policyCall(f.s, f.token, "GET", "/api/policies/"+f.policy.Document.ID+"/source-sync", nil)
	if w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	var result policySourceSyncReply
	_ = json.Unmarshal(w.Body.Bytes(), &result)
	return result
}
func (f *policySourceFixture) action(t *testing.T, action string, patch func(*policySourceSyncRequest)) policyMutationReply {
	t.Helper()
	sync := f.sync(t)
	q := policySourceSyncRequest{OperationID: policyOp(t), BaseRevision: f.policy.Revision, BaseSourceRevision: sync.Revision, Action: action}
	if patch != nil {
		patch(&q)
	}
	result := policyMutation(t, policyCall(f.s, f.token, "POST", "/api/policies/"+f.policy.Document.ID+"/source-sync", q))
	return result
}
func (f *policySourceFixture) bind(t *testing.T) {
	f.action(t, "bind", func(q *policySourceSyncRequest) {
		q.ResourceID = "file-A"
		q.SelectionContext = strings.Repeat("a", 64)
		q.Consent = "recurring-read-after-sign-out"
		q.FrequencyMinutes = 5
	})
}
func (f *policySourceFixture) poll(t *testing.T) {
	t.Helper()
	err := (filePolicyRepository{open: f.s.openPolicies}).Update(context.Background(), func(l *policyLedger) (bool, error) { l.Sources[f.policy.Document.ID].NextCheck = ""; return true, nil })
	if err != nil {
		t.Fatal(err)
	}
	f.s.pollPolicySources(context.Background(), strings.Repeat("f", 64))
}
func (f *policySourceFixture) keep(t *testing.T) {
	t.Helper()
	sync := f.sync(t)
	if len(sync.State.Observations) == 0 {
		t.Fatalf("no observation retained: %+v", sync.State)
	}
	latest := sync.State.Observations[len(sync.State.Observations)-1]
	f.action(t, "keep-current", func(q *policySourceSyncRequest) {
		q.ObservationID = latest.ID
		q.CandidateDigest = f.policy.Document.Candidate.Digest
		q.WorkflowDigest = f.policy.Document.WorkflowDigest
		q.Comment = "Reviewed the source; retain this candidate."
	})
}
func (f *policySourceFixture) artifactCount(t *testing.T) int {
	t.Helper()
	rows, e := os.ReadDir(filepath.Join(f.s.projectDir, retainedArtifactDirectory))
	if e != nil {
		t.Fatal(e)
	}
	return len(rows)
}

func TestPolicySourceSyncRetainsUpdatesDeduplicatesTimingAndGatesPublication(t *testing.T) {
	f := newPolicySourceFixture(t)
	f.policy = policyApprove(t, f.s, f.token, f.policy)
	f.bind(t)
	before := f.artifactCount(t)
	f.poll(t)
	sync := f.sync(t)
	if !sync.Pending || len(sync.State.Observations) != 1 || sync.State.State != "active" {
		t.Fatal(sync)
	}
	if f.artifactCount(t) != before+2 {
		t.Fatal("original and extraction were not separately retained")
	}
	if got := policyRead(t, f.s, f.token, f.policy.Document.ID); got.Revision != f.policy.Revision {
		t.Fatal("source polling changed Policy editor basis")
	}
	policyError(t, policyCall(f.s, f.token, "POST", "/api/policies/"+f.policy.Document.ID+"/publish", policyPublishBody(t, f.policy)), 409, "source-update-unresolved")
	f.keep(t)
	if f.sync(t).Pending {
		t.Fatal("reviewed current source not resolved")
	}
	before = f.artifactCount(t)
	f.observation = policyObservationFixture(t, f.s, f.binding, "", "2026-10-18T06:40:14Z", 123)
	f.poll(t)
	if len(f.sync(t).State.Observations) != 1 || f.artifactCount(t) != before {
		t.Fatal("unchanged scheduled check grew retained history or artifacts")
	}
	f.observation = policyObservationFixture(t, f.s, f.binding, "Changed extracted text", "2026-10-18T06:40:15Z", 456)
	f.poll(t)
	if !f.sync(t).Pending || len(f.sync(t).State.Observations) != 2 {
		t.Fatal("changed extraction not retained")
	}
	f.keep(t)
	edit := policyEditBody(t, f.policy)
	edit.Narrative += "\nExplicit updated candidate."
	f.policy = policyMutation(t, policyCall(f.s, f.token, "PUT", "/api/policies/"+f.policy.Document.ID, edit)).Policy
	if !f.sync(t).Pending {
		t.Fatal("source resolution transferred to changed candidate")
	}
}

func TestPolicySourceSyncRebindRequiresNewBindingSnapshotAndReview(t *testing.T) {
	f := newPolicySourceFixture(t)
	f.policy = policyApprove(t, f.s, f.token, f.policy)
	f.bind(t)
	f.poll(t)
	f.keep(t)
	f.action(t, "revoke", nil)
	f.action(t, "bind", func(q *policySourceSyncRequest) {
		q.ResourceID = "file-B"
		q.SelectionContext = strings.Repeat("b", 64)
		q.Consent = "recurring-read-after-sign-out"
		q.FrequencyMinutes = 15
	})
	if !f.sync(t).Pending {
		t.Fatal("old source resolution bypassed pending bind")
	}
	policyError(t, policyCall(f.s, f.token, "POST", "/api/policies/"+f.policy.Document.ID+"/publish", policyPublishBody(t, f.policy)), 409, "source-update-unresolved")
	f.poll(t)
	if !f.sync(t).Pending {
		t.Fatal("new source auto-resolved")
	}
	f.keep(t)
	if f.sync(t).Pending {
		t.Fatal("new source review failed")
	}
}

func TestPolicySourceSyncPauseAndAuthChangeDuringReadRetainNothing(t *testing.T) {
	for _, change := range []string{"pause", "rotate"} {
		t.Run(change, func(t *testing.T) {
			f := newPolicySourceFixture(t)
			f.bind(t)
			entered, release := make(chan struct{}), make(chan struct{})
			base := f.s.policySourceCall
			f.s.policySourceCall = func(ctx context.Context, m string, p any) (json.RawMessage, error) {
				if m == "source-observe" {
					close(entered)
					<-release
				}
				return base(ctx, m, p)
			}
			done := make(chan struct{})
			go func() { defer close(done); f.s.pollPolicySources(context.Background(), strings.Repeat("f", 64)) }()
			<-entered
			if change == "pause" {
				f.action(t, "pause", nil)
			} else {
				f.s.signIn.policySourceMu.Lock()
				if err := f.s.assistant.rotatePolicySourceAuthority(); err != nil {
					t.Fatal(err)
				}
				f.s.signIn.policySourceMu.Unlock()
			}
			before := f.artifactCount(t)
			close(release)
			<-done
			if len(f.sync(t).State.Observations) != 0 || f.artifactCount(t) != before {
				t.Fatal("disabled inflight response retained an artifact")
			}
		})
	}
}

func TestPolicySourceSyncRevokesLatePendingBindAndLostReply(t *testing.T) {
	for _, lost := range []bool{false, true} {
		t.Run(map[bool]string{false: "late", true: "lost"}[lost], func(t *testing.T) {
			f := newPolicySourceFixture(t)
			f.bind(t)
			entered, release := make(chan struct{}), make(chan struct{})
			base := f.s.policySourceCall
			f.s.policySourceCall = func(ctx context.Context, m string, p any) (json.RawMessage, error) {
				if m == "source-bind" {
					close(entered)
					<-release
					raw, err := base(ctx, m, p)
					if lost {
						return nil, errors.New("lost reply")
					}
					return raw, err
				}
				return base(ctx, m, p)
			}
			done := make(chan struct{})
			go func() { defer close(done); f.s.pollPolicySources(context.Background(), strings.Repeat("f", 64)) }()
			<-entered
			f.action(t, "revoke", nil)
			close(release)
			<-done
			f.poll(t)
			if !f.revoked || contains(f.calls, "source-observe") || len(f.sync(t).State.Observations) != 0 {
				t.Fatal("late grant was observed or not revoked", f.calls)
			}
		})
	}
}

func TestPolicySourceSyncDurableGrantLogoutRestartEpochAndReadFailure(t *testing.T) {
	f := newPolicySourceFixture(t)
	f.bind(t)
	f.s.sessions.revoke(f.token)
	f.poll(t)
	f.token = policyTestSession(t, f.s)
	if len(f.sync(t).State.Observations) != 1 {
		t.Fatal("ordinary logout ended durable explicit grant")
	}
	config, call, canon := f.s.cfg, f.s.policySourceCall, f.s.policySourceCanon
	if err := f.s.Close(); err != nil {
		t.Fatal(err)
	}
	again, err := New(config)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = again.Close() })
	again.policySourceCancel()
	<-again.policySourceDone
	again.policySourceCancel = nil
	f.s = again
	f.token = policyTestSession(t, again)
	again.policySourceCall, again.policySourceCanon = call, canon
	f.poll(t)
	if len(f.sync(t).State.Observations) != 1 {
		t.Fatal("actual server restart lost source history or consent")
	}
	// A different worker owner reclaims an interrupted admission after restart.
	_ = (filePolicyRepository{open: f.s.openPolicies}).Update(context.Background(), func(l *policyLedger) (bool, error) {
		source := l.Sources[f.policy.Document.ID]
		source.Poll = &policySourcePoll{ID: strings.Repeat("c", 64), Owner: strings.Repeat("d", 64), Deadline: "2026-10-17T00:00:00Z", Generation: source.Generation}
		source.NextCheck = ""
		return true, nil
	})
	f.s.pollPolicySources(context.Background(), strings.Repeat("e", 64))
	if f.sync(t).State.Poll != nil {
		t.Fatal("restart did not recover poll admission")
	}
	before := len(f.calls)
	if err := f.s.assistant.rotatePolicySourceAuthority(); err != nil {
		t.Fatal(err)
	}
	f.poll(t)
	if f.sync(t).State.State != "authorization-ended" || len(f.calls) != before {
		t.Fatal("identical-config reset revived old grant")
	}
	// Corrupt existing epoch is never repaired into usable authority.
	if err := os.WriteFile(filepath.Join(f.s.configDir, secretsDirName, policySourceAuthorityFile), []byte(`{}`), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := f.s.policySourceAuthorityBasis(); err == nil {
		t.Fatal("unreadable authority accepted")
	}
}

func TestPolicySourceSyncRefusalIsGapAndCorruptArtifactsFailClosed(t *testing.T) {
	f := newPolicySourceFixture(t)
	f.bind(t)
	f.poll(t)
	original := f.sync(t).State.Observations[0]
	f.s.policySourceCall = func(context.Context, string, any) (json.RawMessage, error) {
		return nil, policySourceFailure("permission-required")
	}
	before := f.artifactCount(t)
	f.poll(t)
	sync := f.sync(t)
	if !sync.State.Gap || sync.State.ErrorCode != "permission-required" || len(sync.State.Observations) != 1 || f.artifactCount(t) != before {
		t.Fatal("provider refusal fabricated source change", sync)
	}
	if err := os.WriteFile(filepath.Join(f.s.projectDir, retainedArtifactDirectory, original.Document.SHA256), []byte("damaged"), 0600); err != nil {
		t.Fatal(err)
	}
	w := policyCall(f.s, f.token, "GET", "/api/policies/"+f.policy.Document.ID+"/source-sync/observations/"+original.ID, nil)
	if w.Code != 503 {
		t.Fatal("corrupt retained extraction exposed", w.Code)
	}
}

func TestPolicySourceSyncCapacityAndBackwardsClock(t *testing.T) {
	f := newPolicySourceFixture(t)
	f.bind(t)
	f.poll(t)
	f.observation = policyObservationFixture(t, f.s, f.binding, "Meaningful change after wall clock moved backwards", "2026-10-17T01:00:00Z", 5)
	f.poll(t)
	sync := f.sync(t)
	if len(sync.State.Observations) != 2 || sync.State.LastSuccess != "2026-10-17T01:00:00Z" {
		t.Fatal("wall clock used as observation commit order", sync.State)
	}
	err := (filePolicyRepository{open: f.s.openPolicies}).Update(context.Background(), func(l *policyLedger) (bool, error) {
		source := l.Sources[f.policy.Document.ID]
		for len(source.Observations) < policyHistoryLimit {
			item := source.Observations[0]
			item.ID = digestOf([]byte(string(rune(len(source.Observations)))))
			source.Observations = append(source.Observations, item)
		}
		return true, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	before := f.artifactCount(t)
	f.observation = policyObservationFixture(t, f.s, f.binding, "Next meaningful update after capacity", "2026-10-18T01:00:00Z", 7)
	f.poll(t)
	sync = f.sync(t)
	if sync.State.State != "paused" || sync.State.ErrorCode != "storage-capacity" || !sync.State.Gap || f.artifactCount(t) != before {
		t.Fatal("capacity silently evicted history or retained unreachable update", sync.State)
	}
}

func TestPolicySourceSyncAuthorityResetAndPublicationRace(t *testing.T) {
	f := newPolicySourceFixture(t)
	f.policy = policyApprove(t, f.s, f.token, f.policy)
	f.bind(t)
	f.poll(t)
	f.keep(t)
	ctx := context.Background()
	repo := filePolicyRepository{open: f.s.openPolicies}
	r := httptest.NewRequest("POST", "/api/policies/"+f.policy.Document.ID+"/publish", nil)
	r.Header.Set("Authorization", "Bearer "+f.token)
	service := f.s.localPolicyService(r, policyActor{Kind: "local", Subject: "local user"})
	// Source commits while publication's read-only Runtime check is outside the
	// ledger lock. Publication must see it in the actual commit transaction.
	service.validateVersion = func(context.Context, string, string) error {
		return repo.Update(ctx, func(l *policyLedger) (bool, error) {
			source := l.Sources[f.policy.Document.ID]
			item := source.Observations[0]
			item.ID = strings.Repeat("7", 64)
			item.SourceRevision = "8"
			source.Observations = append(source.Observations, item)
			return true, nil
		})
	}
	q := policyPublishBody(t, f.policy)
	command := policyCommand{Kind: "publish", PolicyID: f.policy.Document.ID, OperationID: q.OperationID, RequestDigest: strings.Repeat("8", 64), Actor: policyActor{Kind: "local", Subject: "local user"}, Publish: &q}
	_, err := service.execute(ctx, command)
	var failure *policyFailure
	if !errors.As(err, &failure) || failure.Code != "source-update-unresolved" {
		t.Fatal("publication raced past current source update", err)
	}
	before, _ := f.s.policySourceAuthorityBasis()
	if err := resetSignInStore(f.s.assistant); err != nil {
		t.Fatal(err)
	}
	after, _ := f.s.policySourceAuthorityBasis()
	if before == after {
		t.Fatal("CLI reset reused durable authority")
	}
	f.poll(t)
	if f.sync(t).State.State != "authorization-ended" {
		t.Fatal("reset preserved old authority")
	}
}

func TestPolicySourceSyncRealGatewayCanonicalObservation(t *testing.T) {
	program := os.Getenv("JPACK_POLICY_SOURCE_GATEWAY_CANON")
	if program == "" {
		t.Skip("set JPACK_POLICY_SOURCE_GATEWAY_CANON to a provenance-recorded Gateway canon binary")
	}
	// Exact producer fixture, without relabeling or rewriting any field. Generated
	// by Gateway's real processor test at 27df7367b6be5f380c6ae5989bde625bf9dde68a.
	fixturePath := os.Getenv("JPACK_POLICY_SOURCE_GATEWAY_OBSERVATION")
	if fixturePath == "" {
		fixturePath = "testdata/policy-source-observation.json"
	}
	raw, err := os.ReadFile(fixturePath)
	if err != nil {
		t.Fatal(err)
	}
	var wire policyGatewayObservation
	if json.Unmarshal(raw, &wire) != nil {
		t.Fatal("invalid producer fixture")
	}
	f := newPolicySourceFixture(t)
	f.s.policySourceCanon = func(ctx context.Context, raw []byte) ([]byte, error) { return researchCanon(ctx, program, raw) }
	binding := policySourceBinding{Version: 1, OperationID: strings.Repeat("a", 64), BindingID: wire.BindingID, Source: wire.Source, CreatedAt: wire.ObservedAt, State: "active", Coverage: "snapshot-only", Lifecycle: policySourceLifecycle{Kind: "not-exposed"}}
	prepared, err := f.s.verifyPolicyObservation(context.Background(), raw, binding)
	if err != nil {
		t.Fatal("real canonical producer/consumer contract refused", err)
	}
	for _, data := range [][]byte{prepared.Original, prepared.Document} {
		if _, err = f.s.artifactRepository().Put(context.Background(), data); err != nil {
			t.Fatal(err)
		}
	}
	if err = validateArchivedPolicyObservation(context.Background(), f.s.artifactRepository(), prepared.Metadata); err != nil {
		t.Fatal("offline archive verifier refused exact Gateway producer observation", err)
	}
	if prepared.Metadata.Content.BytesDigest != "sha256:"+digestOf(prepared.Original) || prepared.Metadata.Extraction.Content.BytesDigest != "sha256:"+digestOf(prepared.Document) {
		t.Fatal("cross-repository identities do not bind exact retained bytes")
	}
}

func TestPolicySourceSyncRecurringBackendWithoutBrowser(t *testing.T) {
	f := newPolicySourceFixture(t)
	f.bind(t)
	f.s.startPolicySources()
	defer func() { f.s.policySourceCancel(); <-f.s.policySourceDone; f.s.policySourceCancel = nil }()
	// No source API or browser polling drives this observation. Production's
	// scheduler reads the durable consent and polls independently.
	deadline := time.Now().Add(4 * time.Second)
	for time.Now().Before(deadline) {
		var count int
		_ = (filePolicyRepository{open: f.s.openPolicies}).View(context.Background(), func(l policyLedger) error { count = len(l.Sources[f.policy.Document.ID].Observations); return nil })
		if count == 1 {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("backend scheduler did not observe the explicitly authorized source")
}

func TestPolicySourceSyncCanceledUnobservedBindingKeepsPriorExactResolution(t *testing.T) {
	f := newPolicySourceFixture(t)
	f.bind(t)
	f.poll(t)
	f.keep(t)
	f.action(t, "revoke", nil)
	f.action(t, "bind", func(q *policySourceSyncRequest) {
		q.ResourceID = "file-B"
		q.SelectionContext = strings.Repeat("b", 64)
		q.Consent = "recurring-read-after-sign-out"
		q.FrequencyMinutes = 15
	})
	if !f.sync(t).Pending {
		t.Fatal("new binding inherited historical resolution")
	}
	f.action(t, "revoke", nil)
	if f.sync(t).Pending {
		t.Fatal("canceled source without a snapshot erased prior exact resolution")
	}
	edit := policyEditBody(t, f.policy)
	edit.Narrative += "\nNew candidate"
	f.policy = policyMutation(t, policyCall(f.s, f.token, "PUT", "/api/policies/"+f.policy.Document.ID, edit)).Policy
	if !f.sync(t).Pending {
		t.Fatal("old acknowledgment survived changed candidate")
	}
	f.keep(t)
	if f.sync(t).Pending {
		t.Fatal("historical retained update could not be reviewed after revocation")
	}
}

func TestPolicySourceSyncDistinctAcquisitionAndProcessingTimestamps(t *testing.T) {
	f := newPolicySourceFixture(t)
	f.bind(t)
	f.poll(t)
	var wire policyGatewayObservation
	_ = json.Unmarshal(f.observation, &wire)
	wire.ObservationID = strings.Repeat("4", 64)
	wire.ObservedAt = "2026-10-18T06:40:15Z"
	// Gateway's processing record and outer acquisition clock may cross a
	// second. Each is valid independently; their equality is not a contract.
	f.observation, _ = json.Marshal(wire)
	before := f.artifactCount(t)
	f.poll(t)
	sync := f.sync(t)
	if sync.State.ErrorCode != "" || sync.State.LastSuccess != wire.ObservedAt || len(sync.State.Observations) != 1 || f.artifactCount(t) != before {
		t.Fatal("valid distinct timestamps were rejected or created a spurious update", sync.State)
	}
}

func TestPolicySourceSyncProcessorConfigurationChangeCreatesUpdate(t *testing.T) {
	f := newPolicySourceFixture(t)
	f.bind(t)
	f.poll(t)
	before := f.sync(t).State.Observations[0]
	var wire policyGatewayObservation
	_ = json.Unmarshal(f.observation, &wire)
	var document map[string]any
	_ = json.Unmarshal(wire.Document, &document)
	processing := document["processing"].(map[string]any)
	processing["bounds"].(map[string]any)["timeoutMs"] = 24000
	provenance := document["provenance"].(map[string]any)
	provenance["adapter"].(map[string]any)["version"] = "fixture-2"
	configuration, _ := json.Marshal(map[string]any{"profile": "drive-document-processing-v1", "bounds": processing["bounds"], "requestOCR": "auto", "adapter": provenance["adapter"], "processor": provenance["processor"], "ocr": nil})
	configuration, _ = f.s.canonicalPolicySource(context.Background(), configuration)
	wire.Extraction.ConfigurationDigest = "sha256:" + digestOf(configuration)
	wire.Extraction.ProcessorVersion = "fixture-2"
	wire.ObservationID = strings.Repeat("5", 64)
	raw, _ := json.Marshal(document)
	wire.Document, _ = f.s.canonicalPolicySource(context.Background(), raw)
	wire.Extraction.Content.BytesDigest = "sha256:" + digestOf(wire.Document)
	wire.Extraction.Content.ByteLength = len(wire.Document)
	f.observation, _ = json.Marshal(wire)
	f.poll(t)
	sync := f.sync(t)
	if len(sync.State.Observations) != 2 || sync.State.Observations[1].Content != before.Content || sync.State.Observations[1].Extraction.ConfigurationDigest == before.Extraction.ConfigurationDigest {
		t.Fatal("changed processor/configuration was semantically deduplicated", sync.State)
	}
}

func TestPolicySourceSyncDisconnectFencesPreparedReadDespiteLostReplyAndLedgerFailure(t *testing.T) {
	for _, lost := range []bool{false, true} {
		t.Run(map[bool]string{false: "confirmed reply, failed annotation", true: "lost reply"}[lost], func(t *testing.T) {
			f := newPolicySourceFixture(t)
			f.bind(t)
			f.poll(t)
			f.observation = policyObservationFixture(t, f.s, f.binding, "A pending meaningful source update", "2026-10-18T06:40:17Z", 6)
			entered, release := make(chan struct{}), make(chan struct{})
			canon := f.s.policySourceCanon
			count := 0
			f.s.policySourceCanon = func(ctx context.Context, raw []byte) ([]byte, error) {
				out, err := canon(ctx, raw)
				count++
				if count == 3 {
					close(entered)
					<-release
				}
				return out, err
			}
			done := make(chan struct{})
			go func() { defer close(done); f.poll(t) }()
			<-entered
			// Use the actual disconnect route and companion pipe. This companion reads
			// its request and loses the reply; it does not access a real provider.
			bundle := t.TempDir()
			marker := filepath.Join(bundle, "disconnect-sent")
			catalog := `{"version":2,"sources":[],"providers":[{"id":"google-drive","auth":"oauth","registration":"google-desktop","selection":"source-search","queryRequired":false,"operations":["disconnect"]}]}`
			script := "#!/bin/sh\nif [ \"$1\" = '--catalog-v4' ]; then printf '%s' '" + catalog + "'; exit 0; fi\nread request\nprintf sent > '" + marker + "'\nexit 1\n"
			if !lost {
				script = "#!/bin/sh\nif [ \"$1\" = '--catalog-v4' ]; then printf '%s' '" + catalog + "'; exit 0; fi\nread request\nprintf sent > '" + marker + "'\nid=$(printf '%s' \"$request\" | sed -n 's/.*\"id\":\"\\([^\"]*\\)\".*/\\1/p')\nprintf '{\"id\":\"%s\",\"result\":{\"disconnected\":true}}\\n' \"$id\"\n"
			}
			files := map[string]string{}
			for _, name := range []string{"gateway", "adapter-document", "gateway-connections"} {
				raw := []byte("#!/bin/sh\nexit 0\n")
				if name == "gateway-connections" {
					raw = []byte(script)
				}
				if err := os.WriteFile(filepath.Join(bundle, name), raw, 0700); err != nil {
					t.Fatal(err)
				}
				files[name] = digestOf(raw)
			}
			manifest, _ := json.Marshal(map[string]any{"revision": "fixture.policy-source-disconnect", "fixture": true, "officialRelease": false, "files": files})
			t.Setenv("JPACK_DESK_GATEWAY_MANIFEST_SHA256", digestOf(manifest))
			if err := os.WriteFile(filepath.Join(bundle, "gateway-bundle.json"), manifest, 0600); err != nil {
				t.Fatal(err)
			}
			gatewayDone := make(chan struct{})
			f.s.localGateway = &localGateway{bundle: bundle, done: gatewayDone, pin: &localGatewayPin{}}
			t.Cleanup(func() { close(gatewayDone) })
			ledgerPath := filepath.Join(f.s.projectDir, policyDirectory, "ledger.json")
			ledger, err := os.ReadFile(ledgerPath)
			if err != nil {
				t.Fatal(err)
			}
			if err = os.WriteFile(ledgerPath, []byte(`{}`), 0600); err != nil {
				t.Fatal(err)
			}
			beforeAuthority, _ := f.s.policySourceAuthorityBasis()
			beforeArtifacts := f.artifactCount(t)
			w := policyCall(f.s, f.token, "POST", "/api/connections/disconnect", map[string]any{})
			expected := 200
			if lost {
				expected = 503
			}
			if w.Code != expected {
				t.Fatal("fixture did not lose disconnect reply", w.Code, w.Body.String())
			}
			if _, err = os.Stat(marker); err != nil {
				t.Fatal("disconnect was not dispatched", err)
			}
			afterAuthority, _ := f.s.policySourceAuthorityBasis()
			if beforeAuthority == afterAuthority {
				t.Fatal("disconnect was sent before source authority fencing")
			}
			if err = os.WriteFile(ledgerPath, ledger, 0600); err != nil {
				t.Fatal(err)
			}
			close(release)
			<-done
			if f.artifactCount(t) != beforeArtifacts || len(f.sync(t).State.Observations) != 1 {
				t.Fatal("old prepared bytes committed after uncertain disconnect")
			}
		})
	}
}
