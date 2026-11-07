package desk

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"

	runcontext "github.com/Judgment-Pack/judgment-pack-agent/go/context"
	"github.com/Judgment-Pack/judgment-pack-agent/go/contract"
)

// Handwritten retained sources, never captured from a writer observation.
const assistantRecordSource = `{"z":2,"a":{"last":1,"first":0},"Fact":false,"fact":null,"zero":0,"array":[false,null,0],"decimal":1.2}`
const assistantRecordCanonicalValue = `{"a":{"first":0,"last":1},"array":[false,null,0],"decimal":1.2,"Fact":false,"fact":null,"z":2,"zero":0}`

// Current event fixtures are handwritten; reading them does not opt into producers.
const assistantRecordRepeatEvent = `{"version":2,"type":"repeat_detected","execution":"execution-one","dispatchId":"dispatch-one","keyDigest":"repeat-one","count":2,"reminder":true,"stopped":false}`
const assistantRecordRoute = `{"route":"agent","skills":[],"tools":"all","operations":"all","planning":"offered","version":1,"decidedBy":"host","reason":"retained fixture","bindingDigest":"binding-one","catalogueDigest":"catalogue-one","widened":false,"refusals":[]}`
const assistantRecordRouteDecided = `{"version":2,"type":"route_decided","route":` + assistantRecordRoute + `}`
const assistantRecordRouteWidened = `{"version":2,"type":"route_widened","route":` + assistantRecordRoute + `}`
const assistantRecordOpaque = `{"Fact":false,"fact":null,"zero":0}`

func assistantRecordContext(version int64) contract.CheckpointContext {
	source := assistantRecordSource
	entry := contract.CheckpointRetainedEntry{ID: "fixture:item", Kind: "artifact", Label: "Exact source", Value: json.RawMessage(assistantRecordCanonicalValue), ValueJSON: &source, Bytes: int64(len(source)), Digest: digestOf([]byte(source))}
	if version == 1 {
		entry.Value = json.RawMessage(source)
		entry.ValueJSON = nil
	}
	return contract.CheckpointContext{Scope: "fixture", Entries: []contract.CheckpointRetainedEntry{entry}}
}
func assistantRecordHeader(t *testing.T, cp contract.Checkpoint) contract.Checkpoint {
	t.Helper()
	schema, until := runcontext.CheckpointSchema, "2030-01-01T00:00:00.000Z"
	cp.Schema = &schema
	cp.RetainUntil = &until
	cp.Digest = nil
	raw, err := json.Marshal(cp)
	assistantRunMust(t, err)
	value, err := runcontext.ParseJSON(raw)
	assistantRunMust(t, err)
	digest := runcontext.RecordDigest(value)
	cp.Digest = &digest
	return cp
}
func assistantRecordCheckpoint(t *testing.T, cp contract.Checkpoint, version int64) contract.Checkpoint {
	t.Helper()
	cp.Version = version
	if version == 1 {
		cp.PlanState = nil
		cp.Durable = nil
	}
	cp.Status = "stopped"
	cp.Context = assistantRecordContext(version)
	cp.Policy = map[string]contract.PolicyMember{"fixture": {Layer: "run", Value: json.RawMessage(assistantRecordOpaque)}}
	cp.Operations = []contract.CheckpointOperation{{ID: "read-one", Name: "read", Status: "complete", Args: json.RawMessage(assistantRecordOpaque)}}
	if version >= 2 {
		child := cp
		child.ID = "child-one"
		child.Durable = nil
		child = assistantRecordHeader(t, child)
		cp.Durable = &contract.CheckpointDurableBook{Dispatches: []contract.CheckpointDurableDispatch{}, Observations: []contract.CheckpointDurableObservation{}, Children: []contract.CheckpointDurableBookChildrenItem{{Scope: contract.CheckpointDurableScope{Execution: "child-one", Role: "author", Task: ptr("task-one")}, Checkpoint: child}}}
	}
	return assistantRecordHeader(t, cp)
}

func TestAssistantRecordAdoptionStoreBackupAndProjection(t *testing.T) {
	for _, version := range []int64{1, 2, 3} {
		t.Run(fmt.Sprint(version), func(t *testing.T) {
			_, ts, store, binding, record, lease, writer := assistantRunHTTPFixture(t)
			before, err := store.Read(t.Context(), binding, record.ID)
			assistantRunMust(t, err)
			cp := assistantRecordCheckpoint(t, *before.Checkpoint, version)
			assistantRunMust(t, writer.Checkpoint(t.Context(), cp))
			for i, raw := range []string{`{"type":"message","text":"Historical"}`, `{"version":1,"type":"policy_resolved","members":{"fixture":{"layer":"run","value":` + assistantRecordOpaque + `}}}`, assistantRecordRepeatEvent, assistantRecordRouteDecided, assistantRecordRouteWidened} {
				var event contract.RunEvent
				assistantRunMust(t, json.Unmarshal([]byte(raw), &event))
				assistantRunMust(t, writer.Event(t.Context(), fmt.Sprintf("compat-%d", i), event))
			}
			assistantRunMust(t, writer.State(t.Context(), "stopped"))
			retained, err := store.Read(t.Context(), binding, record.ID)
			assistantRunMust(t, err)
			if !reflect.DeepEqual(retained.Checkpoint, &cp) {
				t.Fatal("Desk writer/read changed retained checkpoint")
			}
			view := assistantRunPublic(retained)
			if !reflect.DeepEqual(view.Checkpoint, &cp) {
				t.Fatal("public export lost checkpoint custody")
			}
			assistantRunMust(t, lease.Close())
			// This is the actual authenticated private backup owner, not a synthesized archive.
			backup := backupBytes(t, ts)
			archive, err := zip.NewReader(bytes.NewReader(backup), int64(len(backup)))
			assistantRunMust(t, err)
			found := false
			for _, file := range archive.File {
				if file.Name != store.name {
					continue
				}
				found = true
				reader, err := file.Open()
				assistantRunMust(t, err)
				raw, err := io.ReadAll(reader)
				assistantRunMust(t, err)
				assistantRunMust(t, reader.Close())
				exported, err := decodeAssistantRunStorage(store.name, raw)
				assistantRunMust(t, err)
				held := exported.Runs[0]
				if !reflect.DeepEqual(held.Checkpoint, &cp) || held.Checkpoint.Version != version || *held.Checkpoint.RetainUntil != "2030-01-01T00:00:00.000Z" {
					t.Fatal("untouched backup read upgraded version or extended lifecycle")
				}
				if len(held.Events) != len(retained.Events) || !reflect.DeepEqual(held.Events, retained.Events) {
					t.Fatal("backup changed historical/current event records")
				}
				for _, event := range held.Events[2:] {
					raw, err := json.Marshal(event.Event)
					assistantRunMust(t, err)
					if !bytes.Contains(raw, []byte(`"version":2`)) {
						t.Fatal("current event lost its supplied version")
					}
				}
				historical, _ := json.Marshal(held.Events[0].Event)
				current, _ := json.Marshal(held.Events[1].Event)
				if bytes.Contains(historical, []byte(`"version"`)) || !bytes.Contains(current, []byte(`"version":1`)) || !bytes.Contains(current, []byte(assistantRecordOpaque)) {
					t.Fatal("event/policy-value export rewrote original record", string(current))
				}
			}
			if !found {
				t.Fatal("private backup omitted retained runs")
			}
		})
	}
	// The actual saved-chat projection owner must carry the current companion and child.
	for _, version := range []int64{2, 3} {
		f := newChatProjectionFixture(t)
		run := f.admit(t, "chat-one", "message-one", "retained-adoption", "failed", projectionMessage("Retained"))
		_, err := f.store.transaction(t.Context(), func(l *assistantRunLedger) (bool, error) {
			l.Runs[0].Checkpoint = ptr(assistantRecordCheckpoint(t, *run.Checkpoint, version))
			l.Runs[0].Revision++
			return true, nil
		})
		assistantRunMust(t, err)
		assistantRunMust(t, f.s.prepareAssistantChatProjection(f.request(), run.ID))
		assistantRunMust(t, f.s.writeAssistantChatProjection(f.request(), run.ID))
		assistantRunMust(t, f.s.acknowledgeAssistantChatProjection(f.request(), run.ID))
		_, rows, err := assistantChatRows(f.read(t))
		assistantRunMust(t, err)
		chat, err := assistantTestObject(rows[0])
		assistantRunMust(t, err)
		saved, err := assistantTestObject(chat["checkpoint"])
		assistantRunMust(t, err)
		state, err := assistantTestObject(saved["state"])
		assistantRunMust(t, err)
		var projected contract.Checkpoint
		assistantRunMust(t, json.Unmarshal(state["execution"], &projected))
		if projected.Version != version || !reflect.DeepEqual(projected.Context, assistantRecordContext(version)) || projected.Durable == nil || !reflect.DeepEqual(projected.Durable.Children[0].Checkpoint.Context, assistantRecordContext(version)) {
			t.Fatal("projection lost exact retained companion or nested child")
		}
	}
}

func TestAssistantRecordAdoptionStorageRefusals(t *testing.T) {
	store, binding, cp, record, _, writer := assistantRunAdmitted(t)
	cp = assistantRecordCheckpoint(t, cp, 2)
	assistantRunMust(t, writer.Checkpoint(t.Context(), cp))
	assistantRunMust(t, writer.Event(t.Context(), "stored-historical", projectionMessage("Kept")))
	original := assistantRunRaw(t, store)
	rawCP, err := json.Marshal(cp)
	assistantRunMust(t, err)
	for _, bad := range []string{
		strings.Replace(string(rawCP), `"version":2`, `"version":4`, 1),
		strings.Replace(string(rawCP), `"version":2`, `"version":"2"`, 1),
		strings.Replace(string(rawCP), `"version":2`, `"version":2,"version":2`, 1),
		strings.Replace(string(rawCP), `"version":2,`, "", 1),
		strings.Replace(string(rawCP), `"version":2`, `"version":2,"authority":true`, 1),
		strings.Replace(string(rawCP), `"version":2`, `"version":1`, 1),
	} {
		// Substitute original raw checkpoint bytes before the actual storage decoder.
		modified := bytes.Replace(original, rawCP, []byte(bad), 1)
		if bytes.Equal(modified, original) {
			t.Fatal("refusal fixture did not replace checkpoint")
		}
		if _, err := decodeAssistantRunStorage(store.name, modified); err == nil {
			t.Fatal("malformed/future structural checkpoint admitted")
		}
	}
	// These are real stored events; future/unknown versions must refuse on reload.
	for _, raw := range []string{`{"type":"message","text":"Kept","version":3}`, `{"type":"message","text":"Kept","version":2}`, `{"type":"message","text":"Kept","version":1,"version":1}`, `{"type":"message","text":"Kept","authority":true}`, `{"type":"future_event","version":2}`, strings.Replace(assistantRecordRepeatEvent, `"version":2`, `"version":1`, 1)} {
		modified := bytes.Replace(original, []byte(`{"type":"message","text":"Kept"}`), []byte(raw), 1)
		if bytes.Equal(modified, original) {
			t.Fatal("event refusal fixture did not replace events")
		}
		if _, err := decodeAssistantRunStorage(store.name, modified); err == nil {
			t.Fatal("future/unknown event admitted on reload")
		}
	}
	held, err := store.Read(t.Context(), binding, record.ID)
	assistantRunMust(t, err)
	if !reflect.DeepEqual(held.Checkpoint, &cp) {
		t.Fatal("rejected storage input changed source ledger")
	}
}

func TestAssistantRecordAdoptionRestoreRefusesForgedContext(t *testing.T) {
	for _, mode := range []string{"valid", "semantic", "digest", "bytes", "source"} {
		t.Run(mode, func(t *testing.T) {
			var requests atomic.Int32
			provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				requests.Add(1)
				assistantBriefSSE(w, "Must not dispatch.", "stop")
			}))
			defer provider.Close()
			s, _, e := assistantExecutionFixture(t, provider.URL, "work")
			assistantExecutionRuntimeFixture(t, s, e)
			cp := *e.record.Checkpoint
			cp.Version = 2
			cp.Context = assistantRecordContext(2)
			switch mode {
			case "semantic":
				cp.Context.Entries[0].Value = json.RawMessage(`{"changed":true}`)
			case "digest":
				cp.Context.Entries[0].Digest = strings.Repeat("0", 64)
			case "bytes":
				cp.Context.Entries[0].Bytes++
			case "source":
				bad := `{"different":true}`
				cp.Context.Entries[0].ValueJSON = &bad
			}
			cp = assistantRecordHeader(t, cp)
			assistantRunMust(t, e.Checkpoint(t.Context(), cp))
			e.record = assistantExecutionRecord(t, e)
			x, err := e.openAssistantExecution(context.Background(), assistantExecutionPrompt{Name: "served"})
			assistantRunMust(t, err)
			defer x.Close()
			assistantRunMust(t, x.Adapter.Run(t.Context(), x.Run))
			// Failed initialization is journaled, rather than returned as a Go
			// adapter error. Require its exact cause before any provider request.
			refused := false
			diagnostics := []map[string]any{}
			for _, event := range assistantExecutionRecord(t, e).Events {
				failure := event.Event.Error
				if failure != nil {
					diagnostics = append(diagnostics, map[string]any{"message": failure.Message, "failure": failure.Failure})
				}
				if failure != nil && failure.Failure != nil && failure.Failure.Code == "invalid-tool-input" && failure.Failure.Source == "tool" && failure.Failure.Detail != nil && *failure.Failure.Detail == "Saved context no longer matches its recorded digest. Start with current objects." {
					refused = true
				}
			}
			if mode == "valid" {
				if refused || len(diagnostics) != 0 || requests.Load() != 1 {
					t.Fatalf("matched valid context did not reach exactly one finite request: requests=%d failures=%s", requests.Load(), assistantChatJSON(diagnostics))
				}
				return
			}
			if !refused {
				t.Fatalf("actual Desk adapter did not journal the retained-context refusal; provider requests=%d, typed fixture failures=%s", requests.Load(), assistantChatJSON(diagnostics))
			}
			if requests.Load() != 0 {
				t.Fatal("corrupt retained context reached provider")
			}
		})
	}
}

func TestAssistantRecordAdoptionCompanionAndChildBound(t *testing.T) {
	for _, nested := range []bool{false, true} {
		t.Run(fmt.Sprint(nested), func(t *testing.T) {
			_, _, cp, _, _, writer := assistantRunAdmitted(t)
			cp.Version = 2
			cp.Context = assistantRecordContext(2)
			text := `"` + strings.Repeat("x", 1100000) + `"`
			entry := &cp.Context.Entries[0]
			entry.Value = json.RawMessage(text)
			entry.ValueJSON = &text
			entry.Bytes = int64(len(text))
			entry.Digest = digestOf([]byte(text))
			if nested {
				child := cp
				child.ID = "child-one"
				cp.Durable = &contract.CheckpointDurableBook{Dispatches: []contract.CheckpointDurableDispatch{}, Observations: []contract.CheckpointDurableObservation{}, Children: []contract.CheckpointDurableBookChildrenItem{{Scope: contract.CheckpointDurableScope{Execution: "child-one", Role: "author", Task: ptr("task-one")}, Checkpoint: child}}}
			}
			err := writer.Checkpoint(t.Context(), cp)
			if nested && err == nil {
				t.Fatal("duplicated child companion exceeded 4 MiB but was admitted")
			}
			if !nested {
				assistantRunMust(t, err)
			}
		})
	}
}

// Exercise the actual authenticated continuation action and its canonical
// allowance mutation. The existing finite protocol executor does no model work.
func TestAssistantRecordAdoptionExplicitContinuationUpgradesLegacy(t *testing.T) {
	s, token, paused, _ := assistantContinuationFixture(t, "extend-budget", func(run *assistantRunRecord) {
		cp := *run.Checkpoint
		cp.Version = 1
		cp.PlanState = nil
		cp.Durable = nil
		cp.Context = assistantRecordContext(1)
		cp = assistantRecordHeader(t, cp)
		run.Checkpoint = &cp
	})
	if paused.Checkpoint.Version != 1 || *paused.Checkpoint.RetainUntil != "2030-01-01T00:00:00.000Z" {
		t.Fatal("paused historical read upgraded version or lifecycle")
	}
	action := assistantContinuationAction(paused, "extend-budget")
	assistantProtocolView(t, policyCall(s, token, "POST", "/api/assistant-runs/"+paused.ID+"/actions", action), 200)
	complete := assistantProtocolWait(t, s, token, paused.ID, "complete")
	if complete.Checkpoint.Version != 2 || *complete.Checkpoint.RetainUntil != *paused.Checkpoint.RetainUntil || !reflect.DeepEqual(complete.Checkpoint.Context, paused.Checkpoint.Context) {
		t.Fatal("explicit continuation lost legacy retained bytes or extended lifecycle")
	}
	if complete.Checkpoint.Context.Entries[0].ValueJSON != nil {
		t.Fatal("continuation manufactured a companion for legacy evidence")
	}
	if complete.Checkpoint.Limit != paused.Checkpoint.Limit+10 || len(complete.Checkpoint.Grants) != 1 {
		t.Fatal("actual explicit continuation did not retain its canonical grant")
	}
}
