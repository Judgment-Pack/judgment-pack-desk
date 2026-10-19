package desk

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"io"
	"os/exec"
	"strings"
	"testing"
)

func TestResearchSearchPreflightChecksReviewedRevision(t *testing.T) {
	choice := inputResearchSearch{Connection: "search-test", Revision: strings.Repeat("a", 64)}
	row := func(revision string) string { return `{"id":"search-test","revision":"` + revision + `"}` }
	for _, tt := range []struct {
		name, raw string
		want      error
		ok        bool
	}{
		{"current", `{"version":1,"connections":[` + row(choice.Revision) + `]}`, nil, true},
		{"changed", `{"version":1,"connections":[` + row(strings.Repeat("b", 64)) + `]}`, errResearchSearchChanged, false},
		{"missing", `{"version":1,"connections":[]}`, nil, false},
		{"unreadable", `broken`, errResearchSearchUnavailable, false},
		{"missing list", `{"version":1}`, errResearchSearchUnavailable, false},
		{"error", `{"error":"private provider text"}`, errResearchSearchUnavailable, false},
		{"invalid revision", `{"version":1,"connections":[` + row("bad") + `]}`, errResearchSearchUnavailable, false},
		{"duplicate", `{"version":1,"connections":[` + row(choice.Revision) + `,` + row(choice.Revision) + `]}`, errResearchSearchUnavailable, false},
	} {
		t.Run(tt.name, func(t *testing.T) {
			err := validateResearchSearchStatus([]byte(tt.raw), choice)
			if (err == nil) != tt.ok || tt.want != nil && !errors.Is(err, tt.want) {
				t.Fatalf("unexpected status: %v", err)
			}
			if err != nil && strings.Contains(err.Error(), "private provider text") {
				t.Fatal("provider body exposed")
			}
		})
	}
}

func TestResearchSearchPreflightSkipsSuppliedURLsAndExternalGateway(t *testing.T) {
	s, _, _ := assistantServer(t)
	recipe := researchRecipeFixture()
	if err := s.validateResearchSearchConnection(context.Background(), recipe); err != nil {
		t.Fatal(err)
	}
	recipe.Search = &inputResearchSearch{Connection: "search-test", Revision: strings.Repeat("a", 64)}
	recipe.Limits.Searches = 2
	writeDeskConfig(t, s, researchDeskFile("http://127.0.0.1:1"))
	if err := s.validateResearchSearchConnection(context.Background(), recipe); err != nil {
		t.Fatal(err)
	}
	if len(s.providerConnections) != 0 {
		t.Fatal("checked local settings for external gateway")
	}
}

func TestResearchSearchPreflightBlocksBeforeAnyAIOrSourceAttempt(t *testing.T) {
	s, _, _ := assistantServer(t)
	done := make(chan struct{})
	s.localGateway = &localGateway{bundle: t.TempDir(), done: done, pin: &localGatewayPin{URL: "http://127.0.0.1:1", Authority: "gateway:test", Signer: localSigner{Algorithm: "ed25519", Public: testSignerPublic}}}
	t.Cleanup(func() { close(done) })
	requests, input := io.Pipe()
	output, replies := io.Pipe()
	companion := &connectionCompanion{cmd: exec.Command("unused-test-companion"), input: input, output: bufio.NewReader(output)}
	s.providerConnections = map[string]*connectionCompanion{"web-search": companion}
	t.Cleanup(func() { companion.close(); requests.Close(); output.Close(); replies.Close() })
	seen := make(chan string, 1)
	go func() {
		var req struct {
			ID     string `json:"id"`
			Method string `json:"method"`
		}
		if json.NewDecoder(requests).Decode(&req) != nil {
			seen <- "invalid"
			return
		}
		seen <- req.Method
		json.NewEncoder(replies).Encode(map[string]any{"id": req.ID, "result": map[string]any{"version": 1, "connections": []any{map[string]string{"id": "search-test", "revision": strings.Repeat("b", 64)}}}})
	}()
	root, run := researchRunFixture(t)
	run.Recipe.Limits.Searches = 2
	run.Recipe.Search = &inputResearchSearch{Connection: "search-test", Revision: strings.Repeat("a", 64)}
	if err := writeInputResearch(root, run); err != nil {
		t.Fatal(err)
	}
	s.executeInputResearch(context.Background(), root, run)
	saved, err := readInputResearch(root)
	if err != nil {
		t.Fatal(err)
	}
	if saved.State != "blocked" || saved.Problem != errResearchSearchChanged.Error() || len(saved.Attempts) != 0 {
		t.Fatalf("research started with stale settings: state=%s attempts=%d problem=%s", saved.State, len(saved.Attempts), saved.Problem)
	}
	if method := <-seen; method != "status" {
		t.Fatal("preflight performed an operation", method)
	}
}
