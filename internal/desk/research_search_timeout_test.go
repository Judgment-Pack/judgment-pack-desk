package desk

import (
	"testing"
	"time"
)

func TestSearchRelayBudgetOnlyExtendsManagedSearch(t *testing.T) {
	for _, tt := range []struct {
		local       bool
		route, body string
		extended    bool
	}{
		{true, "acquire", `{"source":"web-search"}`, true},
		{false, "acquire", `{"source":"web-search"}`, false},
		{true, "seal", `{"source":"web-search"}`, false},
		{true, "acquire", `{"source":"gmail"}`, false},
		{true, "acquire", `broken`, false},
	} {
		total, idle := researchRequestTiming(tt.local, tt.route, []byte(tt.body))
		if tt.extended {
			if total != 140*time.Second || idle != 135*time.Second {
				t.Fatal("search envelope cuts off configured timeout")
			}
		} else if total != researchDeadline || idle != researchIdle {
			t.Fatal("unrelated request extended")
		}
	}
}
