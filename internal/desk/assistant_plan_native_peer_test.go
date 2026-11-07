package desk

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"testing"

	runcontext "github.com/Judgment-Pack/judgment-pack-agent/go/context"
	"github.com/Judgment-Pack/judgment-pack-agent/go/contract"
)

// A handwritten native Work sequence. Only the peer is extended: it reads the
// actual acknowledged checkpoint after each bridge answer and never creates
// execution IDs, observations or receipts.
func assistantBindingNativeCompletionPeer(t *testing.T) string {
	t.Helper()
	peer := assistantExecutionCodexPeer
	replace := func(old, next string) {
		t.Helper()
		if strings.Count(peer, old) != 1 {
			t.Fatal("native Plan peer anchor changed", old)
		}
		peer = strings.Replace(peer, old, next, 1)
	}
	replace("request={}; index=0", "request={}; index=0; plan_stage=0; plan_run=''; plan_operation=''")
	replace("for line in sys.stdin:", assistantBindingNativePlanFunctions+"\nfor line in sys.stdin:")
	replace("  prompt=p['input'][0]['text']", "  log({'type':'accepted-before-request','checkpoint':runs[-1]['checkpoint']})\n  plan_run=runs[-1]['checkpoint']['id']\n  prompt=p['input'][0]['text']")
	replace("  else: tool(offered('get_schema'),{})", "  else:\n   cp=plan_checkpoint()\n   rows=[{'id':row['id'],'step':row['step'],'status':'in_progress' if i==0 else 'pending'} for i,row in enumerate(cp['plan'])]\n   plan_stage=1\n   plan_tool('update_plan',{'explanation':'Inspect the reviewed input','plan':rows})")
	replace("  if mode=='duplicate': tool(offered('get_schema'),{},rpc='rpc-two')\n  else: finish()", assistantBindingNativePlanAnswers)
	return peer
}

const assistantBindingNativePlanFunctions = `def plan_refuse(reason):
 log({'type':'native-plan-peer-refused','stage':plan_stage,'reason':reason})
 sys.exit(76)
def plan_checkpoint():
 with open(ledger) as f: current=json.load(f)['runs']
 found=[r['checkpoint'] for r in current if r.get('checkpoint',{}).get('id')==plan_run]
 if len(found)!=1: plan_refuse('checkpoint-identity')
 return found[0]
def plan_tool(name,args):
 wire=offered(name)
 if wire=='unoffered': plan_refuse('missing-tool:'+name)
 tool(wire,args,call='native-plan-'+str(plan_stage),rpc='native-plan-rpc-'+str(plan_stage))
def plan_read(cp):
 scope={'execution':cp['id'],'role':'author'}
 reads=[d for d in cp['durable']['dispatches'] if d['kind']=='host' and d['tool']=='list_decisions']
 if len(reads)!=1: plan_refuse('read-count')
 d=reads[0]
 if d['scope']!=scope or d['state']!='result' or d.get('failure') or not isinstance(d.get('result'),dict) or d['result'].get('isError',False): plan_refuse('read-not-successful')
 observations=[o for o in cp['durable']['observations'] if o['kind']=='operation' and o['dispatchId']==d['id'] and o['scope']==scope]
 if len(observations)!=1: plan_refuse('read-not-observed')
 return d
`

const assistantBindingNativePlanAnswers = `  if q.get('id')!='native-plan-rpc-'+str(plan_stage) or q.get('result',{}).get('success')!=True: plan_refuse('tool-answer')
  cp=plan_checkpoint()
  if plan_stage==1:
   if [r['status'] for r in cp['plan']]!=['in_progress','pending']: plan_refuse('active-state')
   plan_stage=2
   plan_tool('list_decisions',{})
  elif plan_stage==2:
   d=plan_read(cp)
   plan_operation=d['id']
   log({'type':'native-plan-evidence','operation':plan_operation,'scope':d['scope'],'revision':cp['planState']['revision']})
   plan_stage=3
   plan_tool('complete_plan_step',{'id':cp['plan'][0]['id'],'expectedRevision':cp['planState']['revision'],'outcome':'completed','evidence':{'operations':[plan_operation],'artifacts':[]}})
  elif plan_stage==3:
   d=plan_read(cp)
   if d['id']!=plan_operation or [r['status'] for r in cp['plan']]!=['completed','pending'] or cp['plan'][0].get('evidence',{}).get('operations')!=[plan_operation]: plan_refuse('completion-state')
   log({'type':'native-plan-completed','operation':plan_operation,'scope':d['scope'],'revision':cp['planState']['revision']})
   plan_stage=4
   finish()
  else: plan_refuse('unexpected-answer')`

func TestAssistantPlanNativePeerConstruction(t *testing.T) {
	peer := assistantBindingNativeCompletionPeer(t)
	// The shared peer and native transport remain unchanged. Preserve its closed
	// dispatch witness and all bridge permission checks while adding three calls.
	for _, required := range []string{"if not ops or ops[-1]['state']!='dispatched': sys.exit(73)", "p['approvalPolicy']!='never'", "p['runtimeWorkspaceRoots']!=[]", "def plan_read(cp):", "plan_tool('update_plan'", "plan_tool('list_decisions',{})", "plan_tool('complete_plan_step'", "'read-not-observed'", "'read-not-successful'", "'tool-answer'", "'completion-state'"} {
		if !strings.Contains(peer, required) {
			t.Fatal("native Plan peer lost closed check", required)
		}
	}
	if output := os.Getenv("DESK_PLAN_NATIVE_PEER_OUTPUT"); output != "" {
		assistantRunMust(t, os.WriteFile(output, []byte(peer), 0600))
	}
}

func assistantBindingNativeCompletionCheck(t *testing.T, record assistantRunRecord, accepted chatPlan, trace []map[string]json.RawMessage) string {
	t.Helper()
	cp := record.Checkpoint
	if cp == nil || cp.Durable == nil || !assistantAcceptedPlanMatches(runcontext.MustParseJSON(string(assistantChatJSON(accepted))), cp) {
		t.Fatal("native final accepted lineage changed")
	}
	raw, err := json.Marshal(cp)
	assistantRunMust(t, err)
	value := runcontext.MustParseJSON(string(raw))
	lineage := value.Get("planState").Get("accepted")
	for i, row := range value.Get("plan").Items() {
		origin := row.Get("origin")
		if row.Get("step").Str() != accepted.Steps[i] || origin.Get("plan").Str() != accepted.ID || origin.Get("revision").Str() != lineage.Get("revision").Str() || origin.Get("index").Num() != float64(i) {
			t.Fatal("native task origin changed", row)
		}
	}
	operation := ""
	controls := map[string]int{"update_plan": 0, "list_decisions": 0, "complete_plan_step": 0}
	scope := contract.CheckpointDurableScope{Execution: cp.ID, Role: "author"}
	for _, d := range cp.Durable.Dispatches {
		if d.Kind != "host" {
			continue
		}
		if _, known := controls[d.Tool]; !known {
			t.Fatal("unexpected native host action", d.Tool)
		}
		controls[d.Tool]++
		if d.State != "result" || d.Scope != scope || d.Failure != nil {
			t.Fatal("native control acknowledgement failed", d.Tool)
		}
		result, err := runcontext.ParseJSON(d.Result)
		assistantRunMust(t, err)
		if result.Get("isError").Truth() {
			t.Fatal("native control result failed", d.Tool)
		}
		if d.Tool == "list_decisions" {
			operation = d.ID
		}
	}
	for name, count := range controls {
		if count != 1 {
			t.Fatal("native host action count", name, count)
		}
	}
	observations := 0
	for _, o := range cp.Durable.Observations {
		if o.Kind == "operation" && o.DispatchID == operation && o.Scope == scope {
			observations++
		}
	}
	if operation == "" || observations != 1 {
		t.Fatal("native successful same-scope read observation absent")
	}
	models, callbacks, reads := 0, 0, 0
	modelID := ""
	for _, op := range record.Operations {
		switch op.Kind {
		case "model":
			models++
			modelID = op.ID
			if op.State != "completed" {
				t.Fatal("native Work invocation not completed")
			}
		case "callback":
			callbacks++
			if op.State != "completed" {
				t.Fatal("native callback not completed")
			}
		case "host":
			if op.Name != "list_decisions" {
				t.Fatal("unexpected native physical host action", op.Name)
			}
			reads++
			var in assistantHostIntent
			assistantRunMust(t, readAssistantHostReceipt(op.Intent, &in))
			if !assistantNativeHostCall(record, op.Name, in) || !assistantCanonicalHostSucceeded(record, op, in) || in.DurableDispatchID != operation {
				t.Fatal("native exact callback/canonical read custody absent")
			}
			changed := in
			changed.Arguments = json.RawMessage(`{"unexpected":true}`)
			if assistantNativeHostCall(record, op.Name, changed) {
				t.Fatal("native altered arguments passed retained callback guard")
			}
		}
	}
	if models != 1 || callbacks != 3 || reads != 1 {
		t.Fatal("native one-turn physical sequence changed", models, callbacks, reads)
	}
	publicCalls := map[string]int{}
	for _, ev := range record.Events {
		if c := ev.Event.ToolCall; c != nil {
			publicCalls[c.Name]++
		}
	}
	if len(publicCalls) != 3 {
		t.Fatal("native public tool sequence changed", publicCalls)
	}
	for name := range controls {
		if publicCalls[name] != 1 {
			t.Fatal("native duplicate/missing public control", name, publicCalls[name])
		}
	}
	if err := assistantBindingNativeTraceCheck(trace, cp.ID, modelID, operation); err != nil {
		t.Fatal(err)
	}
	t.Logf("native completion: Work turns=1 callbacks=%d physical reads=%d operation=%s scope=%+v observations=%d", callbacks, reads, operation, scope, observations)
	return operation
}

// Local model operation IDs may repeat across proposal and Work records. Bind
// Work to its actual pre-request checkpoint marker and the following generation.
// The caller validates every full checkpoint and its accepted lineage first;
// this helper projects only marker identity/phase to associate trace rows.
// Ignore transport chatter, but require the complete handwritten semantic order.
func assistantBindingNativeTraceCheck(trace []map[string]json.RawMessage, execution, model, operation string) error {
	stage := 0
	scope := contract.CheckpointDurableScope{Execution: execution, Role: "author"}
	for _, row := range trace {
		switch string(row["type"]) {
		case `"generation"`:
			if stage == 0 {
				stage = 1
				continue
			}
			if stage != 2 {
				return fmt.Errorf("native unexpected generation at stage %d", stage)
			}
			var id string
			if err := json.Unmarshal(row["operation"], &id); err != nil {
				return err
			}
			if model == "" || id != model {
				return fmt.Errorf("native Work model identity changed")
			}
			stage = 3
		case `"accepted-before-request"`:
			if stage != 1 {
				return fmt.Errorf("native unexpected Work seed at stage %d", stage)
			}
			var cp struct {
				ID    string `json:"id"`
				Phase string `json:"phase"`
			}
			if err := json.Unmarshal(row["checkpoint"], &cp); err != nil {
				return fmt.Errorf("native malformed Work seed: %w", err)
			}
			if execution == "" || cp.ID != execution || cp.Phase != "work" {
				return fmt.Errorf("native Work seed execution changed")
			}
			stage = 2
		case `"native-plan-peer-refused"`:
			return fmt.Errorf("native peer refused: %s", row["reason"])
		case `"native-plan-evidence"`, `"native-plan-completed"`:
			expected := 3
			if string(row["type"]) == `"native-plan-completed"` {
				expected = 4
			}
			if stage != expected {
				return fmt.Errorf("native unexpected evidence/completion at stage %d", stage)
			}
			var id string
			var gotScope contract.CheckpointDurableScope
			if err := json.Unmarshal(row["operation"], &id); err != nil {
				return err
			}
			if err := json.Unmarshal(row["scope"], &gotScope); err != nil {
				return fmt.Errorf("native malformed read scope: %w", err)
			}
			if operation == "" || id != operation || gotScope != scope {
				return fmt.Errorf("native peer did not reference actual acknowledged read")
			}
			stage++
		}
	}
	if stage != 5 {
		return fmt.Errorf("native incomplete Plan/Work trace at stage %d", stage)
	}
	return nil
}

func TestAssistantPlanNativeTraceScope(t *testing.T) {
	// Handwritten rows deliberately reuse a local model ID across two runs.
	rows := []string{
		`{"type":"generation","operation":"local-model"}`,
		`{"type":"accepted-before-request","checkpoint":{"id":"work-run","phase":"work"}}`,
		`{"type":"generation","operation":"local-model"}`,
		`{"type":"native-plan-evidence","operation":"read","scope":{"execution":"work-run","role":"author"}}`,
		`{"type":"native-plan-completed","operation":"read","scope":{"execution":"work-run","role":"author"}}`,
	}
	cases := []struct {
		name    string
		rows    []string
		refusal string
	}{
		{"same-local-ID-separate-runs", rows, ""},
		{"wrong-run", []string{rows[0], strings.Replace(rows[1], "work-run", "other-run", 1), rows[2], rows[3], rows[4]}, "native Work seed execution changed"},
		{"malformed-marker", []string{rows[0], `{"type":"accepted-before-request","checkpoint":{"id":7,"phase":"work"}}`, rows[2], rows[3], rows[4]}, "native malformed Work seed:"},
		{"missing-marker-identity", []string{rows[0], `{"type":"accepted-before-request","checkpoint":{"phase":"work"}}`, rows[2], rows[3], rows[4]}, "native Work seed execution changed"},
		{"null-marker", []string{rows[0], `{"type":"accepted-before-request","checkpoint":null}`, rows[2], rows[3], rows[4]}, "native Work seed execution changed"},
		{"wrong-phase", []string{rows[0], strings.Replace(rows[1], `"phase":"work"`, `"phase":"plan"`, 1), rows[2], rows[3], rows[4]}, "native Work seed execution changed"},
		{"marker-before-proposal", []string{rows[1], rows[0], rows[2], rows[3], rows[4]}, "native unexpected Work seed at stage 0"},
		{"marker-after-Work", []string{rows[0], rows[2], rows[1], rows[3], rows[4]}, "native unexpected generation at stage 1"},
		{"duplicate-marker", []string{rows[0], rows[1], rows[1], rows[2], rows[3], rows[4]}, "native unexpected Work seed at stage 2"},
		{"missing-marker", []string{rows[0], rows[2], rows[3], rows[4]}, "native unexpected generation at stage 1"},
		{"missing-proposal", rows[1:], "native unexpected Work seed at stage 0"},
		{"missing-Work", []string{rows[0], rows[1], rows[3], rows[4]}, "native unexpected evidence/completion at stage 2"},
		{"duplicate-Work", []string{rows[0], rows[1], rows[2], rows[2], rows[3], rows[4]}, "native unexpected generation at stage 3"},
		{"wrong-model", []string{rows[0], rows[1], strings.Replace(rows[2], "local-model", "other-model", 1), rows[3], rows[4]}, "native Work model identity changed"},
		{"wrong-evidence-scope", []string{rows[0], rows[1], rows[2], strings.Replace(rows[3], "work-run", "other-run", 1), rows[4]}, "native peer did not reference actual acknowledged read"},
		{"unknown-scope-member", []string{rows[0], rows[1], rows[2], strings.Replace(rows[3], `"role":"author"`, `"role":"author","unknown":true`, 1), rows[4]}, "native malformed read scope:"},
		{"wrong-scope-role", []string{rows[0], rows[1], rows[2], strings.Replace(rows[3], `"role":"author"`, `"role":"critic"`, 1), rows[4]}, "native peer did not reference actual acknowledged read"},
		{"unexpected-task-scope", []string{rows[0], rows[1], rows[2], strings.Replace(rows[3], `"role":"author"`, `"role":"author","task":"other-task"`, 1), rows[4]}, "native peer did not reference actual acknowledged read"},
		{"wrong-operation", []string{rows[0], rows[1], rows[2], rows[3], strings.Replace(rows[4], `"read"`, `"other-read"`, 1)}, "native peer did not reference actual acknowledged read"},
		{"reordered-completion", []string{rows[0], rows[1], rows[2], rows[4], rows[3]}, "native unexpected evidence/completion at stage 3"},
		{"missing-completion", rows[:4], "native incomplete Plan/Work trace at stage 4"},
		{"duplicate-evidence", []string{rows[0], rows[1], rows[2], rows[3], rows[3], rows[4]}, "native unexpected evidence/completion at stage 4"},
		{"peer-refused", append(append([]string{}, rows...), `{"type":"native-plan-peer-refused","reason":"unexpected-answer"}`), "native peer refused:"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var trace []map[string]json.RawMessage
			for _, raw := range tc.rows {
				var row map[string]json.RawMessage
				assistantRunMust(t, json.Unmarshal([]byte(raw), &row))
				trace = append(trace, row)
			}
			err := assistantBindingNativeTraceCheck(trace, "work-run", "local-model", "read")
			if tc.refusal == "" {
				if err != nil {
					t.Fatalf("valid separate-run trace refused: %v", err)
				}
			} else if err == nil || !strings.Contains(err.Error(), tc.refusal) {
				t.Fatalf("trace refusal=%v want reason %q", err, tc.refusal)
			}
		})
	}
}
