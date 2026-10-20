package desk

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"
)

// The fixture is copied unchanged from contract/vectors/approval at the locked
// harness revision. Harness certification diffs it against that exact checkout.
// Only resource adapters are fixtures: all scope, expiry, grant matching,
// consumption and refusal behavior runs through assistantApprovalTransaction.
func TestAssistantApprovalHarnessGrantStates(t *testing.T) {
	raw, err := os.ReadFile("testdata/harness-approval/grant-states.json")
	if err != nil {
		t.Fatal(err)
	}
	var vector struct {
		Items []struct {
			Mode  string
			Grant *assistantActionGrant
		}
		Checks []struct {
			What           string
			State          int
			Now            time.Time
			Request        assistantApprovalRequest
			Resource       struct{ Path, Name, Revision string }
			Mode           string
			Admitted       bool
			After, Refusal string
		}
	}
	if err := json.Unmarshal(raw, &vector); err != nil {
		t.Fatal(err)
	}
	if len(vector.Items) != 6 || len(vector.Checks) != 14 {
		t.Fatalf("unexpected vector coverage: %d states, %d checks", len(vector.Items), len(vector.Checks))
	}
	seen := map[int]bool{}
	for _, check := range vector.Checks {
		t.Run(check.What, func(t *testing.T) {
			seen[check.State] = true
			f := newActionFixture(t)
			f.chat["id"] = "chat-1"
			f.save(t)
			request := httptest.NewRequest(http.MethodPost, "/api/assistant-approvals/chat-1/commit", nil)
			request.Header.Set("Authorization", "Bearer contract-session")
			scope := f.s.sessions.handle("assistant-action-approval\n" + bearerOf(request))
			key := scope + "\nchat-1"
			f.s.actionGrants = map[string]assistantActionGrant{}
			item := vector.Items[check.State]
			resource := check.Resource
			if item.Grant != nil {
				grant := *item.Grant
				// Shift the vector's fixed instant to the running clock. At expiry the
				// duration is zero, so admission must already refuse it, not wait a tick.
				grant.ExpiresAt = time.Now().Add(grant.ExpiresAt.Sub(check.Now))
				grant.Scope = scope
				f.s.actionGrants[key] = grant
				// Read the displayed mode before the resource changes in the commit.
				resource.Path, resource.Name, resource.Revision = grant.Path, grant.Name, grant.Revision
			}
			old := assistantActionAdapters
			assistantActionAdapters = map[string]assistantActionAdapter{}
			t.Cleanup(func() { assistantActionAdapters = old })
			commits := 0
			adapter := assistantActionAdapter{resolve: func(context.Context, *Server, assistantApprovalChat, json.RawMessage, string) (assistantActionResource, error) {
				return assistantActionResource{Path: resource.Path, Name: resource.Name, Revision: resource.Revision,
					Verify: func(string) error { return nil },
					Commit: func(string) (int, any) {
						commits++
						if got := f.s.actionGrants[key].State; got != "used" {
							t.Errorf("commit dispatched before consumption: %q", got)
						}
						return http.StatusOK, map[string]any{"saved": true}
					}}, nil
			}}
			for _, action := range []string{"pack-update", "policy-update", "portable-research", "dialect-publish"} {
				assistantActionAdapters[action] = adapter
			}
			display := request.Clone(request.Context())
			display.Method = http.MethodGet
			status, body := f.s.assistantApprovalTransaction(display, "chat-1", assistantApprovalRequest{})
			if status != 200 || body.(map[string]any)["mode"] != check.Mode {
				t.Fatalf("mode: %d %#v, want %q", status, body, check.Mode)
			}
			resource = check.Resource
			status, body = f.s.assistantApprovalTransaction(request, "chat-1", check.Request)
			if (commits == 1) != check.Admitted || commits > 1 {
				t.Fatalf("commit count %d, admitted %v: %d %#v", commits, check.Admitted, status, body)
			}
			if check.Admitted {
				if status != http.StatusOK {
					t.Fatalf("admitted status %d: %#v", status, body)
				}
			} else if status != http.StatusConflict || body.(map[string]string)["error"] != check.Refusal {
				t.Fatalf("refusal: %d %#v, want %q", status, body, check.Refusal)
			}
			if got := f.s.actionGrants[key].State; got != check.After {
				t.Fatalf("after: %q, want %q", got, check.After)
			}
		})
	}
	if len(seen) != len(vector.Items) {
		t.Fatalf("only %d of %d states exercised", len(seen), len(vector.Items))
	}
}
