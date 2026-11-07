package desk

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	runcontext "github.com/Judgment-Pack/judgment-pack-agent/go/context"
	"github.com/Judgment-Pack/judgment-pack-agent/go/relay"
)

// Supply only local fixture programs, observe their real complete catalog and
// persist each explicit source review/approval through the authenticated API.
func assistantSourceEnrollmentFixture(t *testing.T, phase string) *assistantSearchFixture {
	t.Helper()
	f := newAssistantWebsiteFixture(t, "api", "research", phase, "http://127.0.0.1:1")
	// Keep the original admitted execution and its custody profile. This setup
	// supplies only the launched fixture build and explicit companion status.
	build, err := launchedGatewayBuild(f.s.localGateway.bundle)
	assistantRunMust(t, err)
	f.s.localGateway.mu.Lock()
	f.s.localGateway.pin.build = &build
	f.s.localGateway.mu.Unlock()
	_, err = f.s.assistantSearchConfiguration(t.Context(), f.e.source)
	assistantRunMust(t, err)
	manifest, err := assistantSourceManifest(f.s.localGateway.bundle)
	assistantRunMust(t, err)
	f.s.localGateway.mu.Lock()
	f.s.localGateway.launchBundleDigest = digestOf(manifest)
	f.s.localGateway.mu.Unlock()
	setup := policyCall(f.s, f.token, "POST", "/api/connections/catalog", map[string]any{})
	if setup.Code != http.StatusOK {
		t.Fatal("explicit catalog setup", setup.Code, setup.Body.String())
	}
	view := assistantSourceTestView(t, f.s, f.token)
	for _, row := range view.Sources {
		if row.Status != "review-required" {
			t.Fatal("fixture source did not become reviewable", row.Snapshot.AdapterID, row.Status)
		}
		assistantSourceTestApprove(t, f.s, f.token, row)
	}
	if f.acquisitions.Load() != 0 || f.seals.Load() != 0 {
		t.Fatal("source approval acquired material")
	}
	return f
}

func TestAssistantSourceEnrollmentLocalContinuity(t *testing.T) {
	for _, phase := range []string{"work", "plan"} {
		t.Run(phase, func(t *testing.T) {
			s, token, e := assistantExecutionFixture(t, "http://127.0.0.1:1", phase)
			x := assistantGraphSetup(t, s, e)
			for _, tool := range x.Run.HostTools {
				if _, external := assistantExternalOwnerTaxonomies()[tool.Name]; external {
					t.Fatal("source without current consent entered executable offer", tool.Name)
				}
				if problem := runcontext.HostTaxonomyProblem(tool); problem != "" {
					t.Fatal(tool.Name, problem)
				}
			}
			result := assistantGraphInvoke(t, e, x, phase+"-local-read", "read_decision", map[string]string{"type": "graph", "id": "flow"})
			if result.Err() || !strings.Contains(result.Value().Text(), "1.20") {
				t.Fatal("unconfigured sources blocked local read", result.Value().Text())
			}
			calls := 0
			x.Run.Model = func(context.Context, string, relay.Request) (*http.Response, error) {
				calls++
				recorder := httptest.NewRecorder()
				assistantPolicySSE(recorder, "", nil, "Done.")
				return recorder.Result(), nil
			}
			assistantRunMust(t, x.Adapter.Run(t.Context(), x.Run))
			if calls != 1 {
				t.Fatal("local adapter did not dispatch", calls)
			}
			store, err := s.openChatData()
			assistantRunMust(t, err)
			ledger, err := readAssistantSourceLedger(store.root, e.record.Binding.Project)
			assistantRunMust(t, err)
			assistantRunMust(t, store.root.Close())
			if len(ledger.Reviews) != 0 || len(ledger.Approvals) != 0 || s.localGateway != nil {
				t.Fatal("execution minted source review/approval or started Gateway")
			}
			view := assistantSourceTestView(t, s, token)
			for _, row := range view.Sources {
				want := "setup-required"
				if row.Snapshot.AdapterID == "desk-research-workflow" {
					want = "review-required"
				}
				if row.Status != want {
					t.Fatal("source status unavailable", row.Snapshot.AdapterID, row.Status)
				}
			}
		})
	}
}

func TestAssistantSourceEnrollmentCurrentness(t *testing.T) {
	for _, mode := range []string{"revocation", "search-revision"} {
		t.Run(mode, func(t *testing.T) {
			f := assistantSourceEnrollmentFixture(t, "work")
			x := assistantGraphSetup(t, f.s, f.e)
			approved, err := f.e.assistantSourceApprovals(t.Context())
			assistantRunMust(t, err)
			if len(approved) != 4 {
				t.Fatal("incomplete approved production enrollment", len(approved))
			}
			for name, id := range assistantExternalOwnerIDs() {
				tool := assistantGraphFind(t, x.Run.HostTools, name)
				if tool.Source == nil || tool.Source.ID != id || tool.Source.CurrentIdentity == nil {
					t.Fatal("missing real source", name)
				}
				_, err := tool.Source.CurrentIdentity(t.Context())
				assistantRunMust(t, err)
				if mode == "search-revision" && name != assistantSearchTool {
					continue
				}
				if mode == "revocation" {
					a := approved[id]
					response := policyCall(f.s, f.token, "POST", "/api/assistant-source-approvals/chat-one/revoke", assistantSourceRevokeRequest{ApprovalID: a.ID, ApprovalRevision: a.Revision})
					if response.Code != http.StatusOK {
						t.Fatal("revoke", response.Code, response.Body.String())
					}
				} else {
					assistantRunMust(t, os.WriteFile(f.statusPath, assistantChatJSON(map[string]any{"version": 1, "connections": []any{map[string]any{"id": "demo", "revision": strings.Repeat("b", 64), "provider": "tavily"}}}), 0600))
				}
				if _, err := tool.Source.CurrentIdentity(t.Context()); err == nil {
					t.Fatal("retained source survived currentness change", name)
				}
				registry, err := f.e.assistantConversationTools(x.runtime, nil)
				assistantRunMust(t, err)
				for _, current := range registry.tools {
					if current.Name == name {
						t.Fatal("changed/revoked source re-enrolled", name)
					}
				}
				view := assistantSourceTestView(t, f.s, f.token)
				row := assistantSourceTestRow(t, view, id)
				want := "revoked"
				if mode == "search-revision" {
					want = "changed"
				}
				if row.Status != want {
					t.Fatal("current source status lost", id, row.Status)
				}
			}
			if f.acquisitions.Load() != 0 || f.seals.Load() != 0 {
				t.Fatal("currentness checks acquired material")
			}
		})
	}
}

// Test policy instrumentation may wrap an already enrolled production owner,
// but only when its complete actual declaration matches the persisted approval.
func assistantResearchAlreadyEnrolled(t *testing.T, tool runcontext.HostTool, approval assistantSourceApproval) bool {
	t.Helper()
	if tool.Source == nil {
		return false
	}
	binding, err := assistantResearchApprovalBinding(approval)
	assistantRunMust(t, err)
	raw, err := json.Marshal(tool.Source.Record(tool.Name))
	assistantRunMust(t, err)
	value, err := runcontext.ParseJSON(raw)
	assistantRunMust(t, err)
	if tool.Name != assistantResearchTool || !assistantExternalSourceDeclaration(tool) || runcontext.CanonicalJSON(value) != string(binding.SourceRecord) || tool.Source.CurrentIdentity == nil || tool.Approval != "required" || tool.Approve == nil || tool.Source.VerifyReceipt != nil {
		t.Fatal("test instrumentation did not retain exact production source")
	}
	return true
}
