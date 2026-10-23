package desk

import (
	"context"
	"encoding/json"
	runcontext "github.com/Judgment-Pack/judgment-pack-agent/go/context"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"
)

func TestAssistantCriticControlVectors(t *testing.T) {
	raw, err := os.ReadFile("../../web/src/chat/fixtures/critic-controls.json")
	assistantRunMust(t, err)
	var rows []struct {
		Name                  string
		Chat                  json.RawMessage
		Project, Mode, Engine string
	}
	assistantRunMust(t, json.Unmarshal(raw, &rows))
	for _, row := range rows {
		t.Run(row.Name, func(t *testing.T) {
			saved := assistantChatSaved
			if string(row.Chat) != "{}" {
				saved = strings.Replace(saved, `"interaction":"work"`, `"interaction":"work",`+string(row.Chat[1:len(row.Chat)-1]), 1)
			}
			source := assistantChatSource(t, saved)
			if row.Engine != "" {
				source.Connection.Assistant.Engine = row.Engine
			}
			source.Profile.Critic = row.Project
			input, err := compileAssistantChatInput(source)
			assistantRunMust(t, err)
			var exact struct {
				Critic, CriticReasoning string
				AdversarialReview       *bool
			}
			assistantRunMust(t, json.Unmarshal(row.Chat, &exact))
			if input.Critic != exact.Critic || input.CriticReasoning != exact.CriticReasoning || input.CriticProject != row.Project {
				t.Fatal("exact critic inputs lost", input)
			}
			if (input.Review == nil) != (exact.AdversarialReview == nil) || input.Review != nil && *input.Review != *exact.AdversarialReview {
				t.Fatal("legacy boolean presence lost")
			}
			mode, _ := runcontext.CriticMode(runcontext.CriticSettings{Critic: input.Critic, CriticProject: input.CriticProject, AdversarialReview: input.Review}, runcontext.CriticTable{})
			if mode != row.Mode {
				t.Fatal("canonical mode", mode, row.Mode)
			}
		})
	}
}

// The real Desk factory and Agent adapter decide from held saved-suite facts.
func TestAssistantCriticAutoActualAPI(t *testing.T) {
	for _, proposal := range []bool{false, true} {
		t.Run(map[bool]string{false: "conversation", true: "saved-test-proposal"}[proposal], func(t *testing.T) {
			u := newUpstream(t, func(w http.ResponseWriter, r *http.Request) {
				var body struct{ Model string }
				_ = json.NewDecoder(r.Body).Decode(&body)
				text := "Explanation only."
				if proposal {
					text = assistantCriticProposal
				}
				if body.Model == "child-model" {
					text = "Independent critic findings."
				}
				assistantCriticSSE(w, text)
			})
			s, token, old := assistantExecutionFixture(t, u.server.URL, "work")
			old.cancel()
			<-old.done
			assistantCriticAPISetup(t, s)
			e := assistantCriticResave(t, s, token, old, "auto", "work", true)
			x := assistantCriticOpen(t, s, e, "served")
			if x.Run.Critic != "auto" || len(x.Run.Cases) == 0 || !x.Run.ExistingPack {
				t.Fatal("Desk did not bind auto facts")
			}
			ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
			defer cancel()
			assistantRunMust(t, x.Adapter.Run(ctx, x.Run))
			record := assistantExecutionRecord(t, e)
			want := 1
			if proposal {
				want = 2
			}
			if len(u.arrivals()) != want {
				t.Fatalf("model requests %d want %d", len(u.arrivals()), want)
			}
			critic := record.Checkpoint.Critic
			if critic == nil || critic.Ran == nil || *critic.Ran != proposal || critic.Reason == nil {
				t.Fatal("canonical decision missing", critic)
			}
			reason := "no-proposal"
			if proposal {
				reason = "cases"
			}
			if *critic.Reason != reason {
				t.Fatal("wrong canonical reason", *critic.Reason)
			}
			found := false
			for _, held := range record.Events {
				if held.Event.CriticDecided != nil {
					found = true
				}
			}
			if !found {
				t.Fatal("canonical task event missing")
			}
		})
	}
}
