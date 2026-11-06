package desk

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestAssistantTestHostCriticConsumedInputs(t *testing.T) {
	for _, mode := range []string{"deleted", "skipped-row", "full-envelope", "absent-empty", "legacy-v1", "legacy-v1-and-v2"} {
		t.Run(mode, func(t *testing.T) {
			s, token, e, _ := assistantTestHostFixture(t, "http://127.0.0.1:1", "work", false, false, true)
			if mode == "skipped-row" {
				assistantTestHostReplaceSuite(t, s, token, "one", func(suite map[string]json.RawMessage) {
					cases, _ := assistantChatArray(suite["cases"])
					c, _ := assistantTestObject(cases[0])
					row, _ := assistantTestObject(c["row"])
					delete(row, "expectedDisposition")
					c["row"] = assistantChatEncodeObject(row)
					cases[0] = assistantChatEncodeObject(c)
					suite["cases"] = assistantChatEncodeArray(cases)
				})
			}
			if mode == "absent-empty" {
				saved := policyWorkspaceRead(t, s, token, "/api/pack-tests")
				fields, _ := assistantTestObject(saved.Content)
				suites, _ := assistantTestObject(fields["suites"])
				delete(suites, "one")
				fields["suites"] = assistantChatEncodeObject(suites)
				policyWorkspaceSave(t, s, token, "/api/pack-tests", json.RawMessage(assistantChatEncodeObject(fields)))
			}
			x := assistantTestHostOpen(t, s, e)
			if mode == "legacy-v1" || mode == "legacy-v1-and-v2" {
				before := assistantRunRaw(t, e.writer.lease.store)
				members := `"deskCriticInputV1":"` + e.input.critic.fingerprint + `",`
				if mode == "legacy-v1-and-v2" {
					members += `"deskCriticInputV2":"` + e.input.critic.fingerprint + `",`
				}
				legacy := bytes.Replace(before, []byte(`"checkpoint":{`), []byte(`"checkpoint":{`+members), 1)
				if bytes.Equal(legacy, before) {
					t.Fatal("legacy fixture not applied")
				}
				root, err := s.openChatData()
				assistantRunMust(t, err)
				defer root.root.Close()
				path := filepath.Join(root.location.Path, e.writer.lease.store.name)
				assistantRunMust(t, os.WriteFile(path, legacy, 0600))
				continued := *e
				continued.input = &assistantChatCustody{}
				continued.record = e.record
				next, err := continued.openAssistantExecution(t.Context(), assistantExecutionPrompt{Name: "served"})
				if next != nil {
					next.Close()
				}
				if err == nil {
					t.Fatal("legacy whole-suite custody was rebound as V2")
				}
				after, err := os.ReadFile(path)
				assistantRunMust(t, err)
				if !bytes.Equal(legacy, after) {
					t.Fatal("legacy refusal changed original run/checkpoint")
				}
				return
			}
			_ = x
			if mode == "absent-empty" {
				saved := policyWorkspaceRead(t, s, token, "/api/pack-tests")
				fields, _ := assistantTestObject(saved.Content)
				suites, _ := assistantTestObject(fields["suites"])
				suites["one"] = []byte(`{"revision":0,"cases":[],"runs":[],"recovered":[]}`)
				fields["suites"] = assistantChatEncodeObject(suites)
				policyWorkspaceSave(t, s, token, "/api/pack-tests", json.RawMessage(assistantChatEncodeObject(fields)))
			} else {
				assistantTestHostReplaceSuite(t, s, token, "one", func(suite map[string]json.RawMessage) {
					if mode == "deleted" {
						suite["deleted"] = []byte(`[]`)
						return
					}
					cases, _ := assistantChatArray(suite["cases"])
					c, _ := assistantTestObject(cases[0])
					if mode == "full-envelope" {
						c["name"] = assistantChatJSON("Changed envelope")
					} else {
						row, _ := assistantTestObject(c["row"])
						row["facts"] = []byte(`{"amount":"changed skipped row"}`)
						c["row"] = assistantChatEncodeObject(row)
					}
					cases[0] = assistantChatEncodeObject(c)
					suite["cases"] = assistantChatEncodeArray(cases)
				})
			}
			if e.recheckAssistantCritic(t.Context()) == nil {
				t.Fatal("changed consumed input accepted", mode)
			}
		})
	}
}
