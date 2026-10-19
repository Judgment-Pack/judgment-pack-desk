package desk

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestPolicyExecutionLocalIngressAndUnavailableJobsRefusal(t *testing.T) {
	f, _ := admissionPrepareFixture(t, "committed")
	if _, err := enableAdmissionFixture(t, f, nil); err != nil {
		t.Fatal(err)
	}
	s, err := f.configured(t)
	if err != nil {
		t.Fatal(err)
	}
	// The retained demand remains authoritative even with Jobs absent.
	if s.jobs != nil {
		t.Fatal("fixture unexpectedly has Jobs")
	}
	c := &conn{out: make(chan []byte, 4), done: make(chan struct{})}
	operational := []byte(`{"jsonrpc":"2.0","id":17,"method":"tools/call","params":{"name":"experimental_evaluate","arguments":{"pack_id":"locked"}}}`)
	if s.admitRuntimeFrame(c, operational) {
		t.Fatal("unavailable Jobs downgraded direct evaluator")
	}
	reply := <-c.out
	if !bytes.Contains(reply, []byte("policy-execution-required")) {
		t.Fatal(string(reply))
	}
	for _, raw := range []string{
		`{"jsonrpc":"2.0","id":18,"method":"tools/call","params":{"name":"get_schema","arguments":{}}}`,
		`{"jsonrpc":"2.0","id":19,"method":"tools/call","params":{"name":"experimental_evaluate","arguments":{"rehearsal":true}}}`,
	} {
		if !s.admitRuntimeFrame(c, []byte(raw)) {
			t.Fatal("nonoperational utility refused", raw)
		}
	}
	if !f.s.admitRuntimeFrame(c, operational) {
		t.Fatal("unconfigured relay changed")
	}
	token := policyTestSession(t, s)
	for _, path := range []string{"/api/input-preparations", "/api/input-preparations/prep_synthetic/evaluations", "/api/input-preparations/prep_synthetic/research"} {
		w := policyCall(s, token, "POST", path, map[string]any{})
		if w.Code != 409 || !strings.Contains(w.Body.String(), "unsupported-profile") {
			t.Fatal("workflow reached acquisition", path, w.Code, w.Body.String())
		}
	}
	if _, err = s.inputRunner(context.Background(), "jobs", map[string]any{}, ""); err == nil {
		t.Fatal("private workflow bypassed profile")
	}
}
func TestPolicyExecutionRuntimeFrameClosedClassification(t *testing.T) {
	for _, raw := range []string{
		`{"jsonrpc":"2.0","method":"tools/call","params":{"name":"future_operational_tool","arguments":{"rehearsal":true}}}`,
		`{"jsonrpc":"2.0","method":"tools/call","params":{"name":"experimental_evaluate","arguments":{"rehearsal":"true"}}}`,
		`{"jsonrpc":"2.0","method":"tools/call","params":{"name":"experimental_evaluate","arguments":{"rehearsal":true,"Rehearsal":false}}}`,
		`{"jsonrpc":"2.0","method":"tools/call","params":{"name":"experimental_evaluate","arguments":{"rehearsal":true,"rehearsal":false}}}`,
		`{"jsonrpc":"2.0","Method":"tools/call","id":1,"result":{},"params":{"name":"experimental_evaluate"}}`,
		`[{"jsonrpc":"2.0","method":"tools/call","params":{"name":"experimental_evaluate"}}]`,
	} {
		if manualRuntimeFrameAllowed([]byte(raw)) {
			t.Fatal("ambiguous/operational frame accepted", raw)
		}
	}
}
func TestPolicyExecutionHTTPRequiresLocalSessionAndOriginalHeaders(t *testing.T) {
	f, _ := admissionPrepareFixture(t, "committed")
	if _, err := enableAdmissionFixture(t, f, nil); err != nil {
		t.Fatal(err)
	}
	s, err := f.configured(t)
	if err != nil {
		t.Fatal(err)
	}
	wspace := executorRegistry(t, f).Workspaces[0].Execution.RunnerWorkspace
	calls := 0
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		w.Header().Set("Content-Type", "application/json")
		io.WriteString(w, `{}`)
	}))
	defer upstream.Close()
	// Synthetic live owner endpoint: these tests distinguish host authentication
	// and no-forwarding. Full binding/Runtime evidence is in the real fixture.
	s.jobs = &jobsCompanion{workspace: wspace, token: "owner-token", url: upstream.URL, done: make(chan struct{}), stop: make(chan struct{}), admission: &policyExecutionService{}}
	defer func() { s.jobs = nil }()
	local := policyTestSession(t, s)
	issuer := "https://issuer.invalid"
	oidc, err := s.sessions.createExpiring("oidc-admin", &issuer, "", "")
	if err != nil {
		t.Fatal(err)
	}
	job := "job_" + strings.Repeat("a", 32)
	call := func(token string, headers http.Header) *httptest.ResponseRecorder {
		r := httptest.NewRequest("POST", "/api/operations/jobs/"+job+"/runs", strings.NewReader(`{"facts":false}`))
		r.Header = headers.Clone()
		r.Header.Set("Authorization", "Bearer "+token)
		out := httptest.NewRecorder()
		s.ServeHTTP(out, r)
		return out
	}
	for _, token := range []string{testToken, oidc} {
		if w := call(token, http.Header{"Idempotency-Key": []string{"original"}}); w.Code != 401 {
			t.Fatal("nonlocal credential admitted", w.Code, w.Body.String())
		}
	}
	for _, header := range []http.Header{
		{}, {"Idempotency-Key": []string{"one", "two"}},
		{"Idempotency-Key": []string{"one"}, "idempotency-key": []string{"two"}},
		{"Idempotency-Key": []string{"one"}, "x-jpack-manual-intent": []string{"forged"}},
	} {
		if w := call(local, header); w.Code < 400 {
			t.Fatal("invalid original headers accepted", w.Code)
		}
	}
	if calls != 0 {
		t.Fatal("refused request reached Runner", calls)
	}
	r := httptest.NewRequest("GET", "/", nil)
	r.Header.Set("Authorization", "Bearer "+local)
	core, err := s.localExecutionCore(r)
	if err != nil {
		t.Fatal(err)
	}
	s.sessions.revoke(local)
	if core.authorized(context.Background()) == nil {
		t.Fatal("rotated/revoked local session retained authorization")
	}
}
