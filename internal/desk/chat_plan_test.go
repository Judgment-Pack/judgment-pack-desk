package desk

import (
	"encoding/json"
	"os"
	"strings"
	"testing"
)

func TestChatPlanAdmissionAndStorage(t *testing.T) {
	s, _, _ := assistantServer(t)
	seedMemoryMessage(t, s, "remember: concise outreach")
	update := func(phase string) {
		t.Helper()
		store, e := s.openChatData()
		if e != nil {
			t.Fatal(e)
		}
		defer store.root.Close()
		b, e := readPrivateData(store.root, s.conversationName(), maxConversationBytes)
		if e != nil {
			t.Fatal(e)
		}
		var d map[string]any
		json.Unmarshal(b, &d)
		d["chats"].([]any)[0].(map[string]any)["interaction"] = phase
		b, _ = json.Marshal(d)
		if e = writePrivateData(store.root, s.conversationName(), b); e != nil {
			t.Fatal(e)
		}
	}
	update("plan")
	if e := s.checkChatExecution("chat-one"); e == nil || !strings.Contains(e.Error(), "Plan mode") {
		t.Fatal(e)
	}
	if e := s.withMemory(func(root *os.Root, r *memoryReply) error {
		return s.applyMemory(root, r, memoryChange{Action: "suggest", Key: "style", Text: "concise outreach", Source: &memorySource{ChatID: "chat-one", TurnID: "turn-one"}})
	}); e == nil {
		t.Fatal("Plan saved a memory")
	}
	update("work")
	if e := s.checkChatExecution("chat-one"); e != nil {
		t.Fatal(e)
	}
	for _, raw := range []string{`{"version":1,"chats":[{"interaction":"auto-execute"}]}`, `{"version":1,"chats":[{"plans":[{"id":"x","revision":1,"status":"started"}]}]}`} {
		if validateConversations([]byte(raw)) == nil {
			t.Fatal("invalid plan accepted")
		}
	}
	if e := validateConversations([]byte(`{"version":1,"chats":[{"id":"old-chat"}]}`)); e != nil {
		t.Fatal("legacy chat rejected", e)
	}
}

func TestChatPlanCannotConsumeAutoAllowance(t *testing.T) {
	s, rr, run, _ := approvalFixture(t)
	authorizeFixture(t, s, run)
	store, e := s.openChatData()
	if e != nil {
		t.Fatal(e)
	}
	defer store.root.Close()
	if e = writePrivateData(store.root, s.conversationName(), []byte(`{"version":1,"chats":[{"id":"chat-1","interaction":"plan"}]}`)); e != nil {
		t.Fatal(e)
	}
	response := requestApprovalRun(s, run, "chat-1", "turn-1")
	if response.Code != 409 || !strings.Contains(response.Body.String(), "Plan mode") {
		t.Fatal(response.Code, response.Body.String())
	}
	unchanged, e := readInputResearch(rr)
	if e != nil || unchanged.State != "review" || unchanged.Approval.UsedAt != "" {
		t.Fatal("Plan consumed Auto", e, unchanged.State)
	}
	if e = writePrivateData(store.root, s.conversationName(), []byte(`{"version":1,"chats":[{"id":"chat-1","interaction":"work"}]}`)); e != nil {
		t.Fatal(e)
	}
	response = requestApprovalRun(s, run, "chat-1", "turn-1")
	if response.Code != 200 || !strings.Contains(response.Body.String(), `"status":"started"`) {
		t.Fatal(response.Code, response.Body.String())
	}
}
