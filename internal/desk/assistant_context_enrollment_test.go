package desk

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"reflect"
	"strings"
	"testing"

	"github.com/Judgment-Pack/judgment-pack-agent/go/adapter"
	native "github.com/Judgment-Pack/judgment-pack-agent/go/adapter/agent"
	"github.com/Judgment-Pack/judgment-pack-agent/go/adapter/eino"
	runcontext "github.com/Judgment-Pack/judgment-pack-agent/go/context"
	"github.com/Judgment-Pack/judgment-pack-agent/go/contract"
	"github.com/Judgment-Pack/judgment-pack-agent/go/gate"
	"github.com/Judgment-Pack/judgment-pack-agent/go/relay"
)

func assistantEnrollmentHost() runcontext.HostTool {
	return runcontext.HostTool{Name: "inspect", Description: "Read exact fixture bytes", InputSchema: json.RawMessage(`{"type":"object"}`), Taxonomy: &runcontext.ToolTaxonomy{Operation: "read", Effect: "read"}, Effect: "read", Delegation: "read", Execute: func(context.Context, json.RawMessage) (runcontext.ToolResult, error) {
		return runcontext.ResultOf([]runcontext.Content{runcontext.TextContent("exact fixture bytes")}, nil), nil
	}}
}

func assistantEnrollmentRun(t *testing.T, engine string, g *gate.Gate, input *runcontext.Input, inspect func([]adapter.AgentTool)) (*adapter.Run, adapter.Adapter) {
	t.Helper()
	run := &adapter.Run{Prompt: "Inspect supplied facts", Wire: "openai-compatible", ModelName: "fixture", Thinking: "off", Critic: "off", AllowConversation: true, Interactive: true, Context: input, Gate: g.Calls(), HostTools: []runcontext.HostTool{assistantEnrollmentHost()}, Emit: func(contract.RunEvent) {}}
	if engine == "native" {
		run.Agent = &adapter.Agent{Model: "fixture", Run: func(ctx context.Context, request adapter.AgentRequest, cb adapter.AgentCallbacks) error {
			inspect(request.Tools)
			return cb.Event(ctx, adapter.AgentEvent{Type: "message", Text: "Done.", Phase: "final"})
		}}
		return run, native.New()
	}
	run.Model = func(_ context.Context, _ string, request relay.Request) (*http.Response, error) {
		var body struct {
			Tools []struct {
				Function struct {
					Name, Description string
					Parameters        json.RawMessage
				} `json:"function"`
			} `json:"tools"`
		}
		assistantRunMust(t, json.Unmarshal(request.Body, &body))
		tools := []adapter.AgentTool{}
		for _, row := range body.Tools {
			tools = append(tools, adapter.AgentTool{Name: row.Function.Name, Description: row.Function.Description, InputSchema: row.Function.Parameters})
		}
		inspect(tools)
		return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": []string{"text/event-stream"}}, Body: io.NopCloser(strings.NewReader("data: {\"id\":\"fixture\",\"object\":\"chat.completion.chunk\",\"choices\":[{\"index\":0,\"delta\":{\"content\":\"Done.\"},\"finish_reason\":null}]}\n\ndata: {\"id\":\"fixture\",\"object\":\"chat.completion.chunk\",\"choices\":[{\"index\":0,\"delta\":{},\"finish_reason\":\"stop\"}]}\n\ndata: [DONE]\n\n"))}, nil
	}
	return run, eino.New()
}

func TestAssistantContextEnrollmentActualAdapters(t *testing.T) {
	for _, engine := range []string{"eino", "native"} {
		for _, mode := range []string{"ack", "plan", "unknown", "mismatched-mode", "missing", "duplicate", "mutated"} {
			t.Run(engine+"/"+mode, func(t *testing.T) {
				g := gate.New(gate.Options{})
				g.SetOffer(contract.ToolOffer{})
				input := &runcontext.Input{CurrentRequest: &runcontext.CurrentRequest{Message: "Inspect", Input: "exact supplied facts"}}
				if mode == "plan" {
					input.Phase = "plan"
				}
				if mode == "plan" || mode == "mismatched-mode" {
					g.Enter(gate.PlanPhase(g.Offer()))
				}
				b := &assistantGateBindings{}
				b.add(g)
				providers := 0
				run, inner := assistantEnrollmentRun(t, engine, g, input, func(tools []adapter.AgentTool) {
					providers++
					if len(g.Offer().HostTools) == 0 {
						t.Fatal("provider preceded enrollment")
					}
					for _, declaration := range g.Offer().HostTools {
						found := false
						for _, tool := range tools {
							var want, actual any
							_ = json.Unmarshal(declaration.InputSchema, &want)
							_ = json.Unmarshal(tool.InputSchema, &actual)
							if (tool.Description == declaration.Description || tool.Description == declaration.Name+": "+declaration.Description) && reflect.DeepEqual(want, actual) {
								found = true
							}
						}
						if !found {
							t.Fatal("provider lacks enrolled declaration", declaration.Name)
						}
					}
					for _, name := range []string{"inspect", "update_plan", "complete_plan_step", "request_budget_increase"} {
						if g.Calls().AdmitHost(name) != nil {
							t.Fatal("enrolled action refused", name)
						}
					}
					for _, name := range []string{"unknown", "UPDATE_PLAN", "update_plan_extra", "propose_graph"} {
						if g.Calls().AdmitHost(name) == nil {
							t.Fatal("unoffered action admitted", name)
						}
					}
				})
				assistantRunMust(t, b.bind(run)) // Also available before the inner adapter.
				if mode == "unknown" {
					other := gate.New(gate.Options{})
					run.Gate = other.Calls()
				}
				wrapped := b.adapter(adapter.Func(func(ctx context.Context, current *adapter.Run) error {
					acknowledge := current.EnrollContext
					current.EnrollContext = func(ctx context.Context, offer adapter.ContextOffer) error {
						switch mode {
						case "missing":
							return nil
						case "mutated":
							offer.HostTools[0].Description = "mutated declaration"
						}
						if err := acknowledge(ctx, offer); err != nil {
							return err
						}
						if mode == "duplicate" {
							return acknowledge(ctx, offer)
						}
						return nil
					}
					return inner.Run(ctx, current)
				}))
				err := wrapped.Run(t.Context(), run)
				if mode == "ack" || mode == "plan" {
					assistantRunMust(t, err)
					if providers != 1 {
						t.Fatalf("providers=%d", providers)
					}
				} else if providers != 0 {
					t.Fatal("invalid enrollment dispatched provider", providers)
				}
				if mode == "unknown" && err == nil {
					t.Fatal("unknown handle accepted")
				}
			})
		}
	}
}

func TestAssistantContextEnrollmentCopiedChild(t *testing.T) {
	for _, engine := range []string{"eino", "native"} {
		for _, parentMode := range []string{"work", "plan"} {
			t.Run(engine+"/"+parentMode, func(t *testing.T) {
				parent := gate.New(gate.Options{})
				parent.SetOffer(contract.ToolOffer{})
				if parentMode == "plan" {
					parent.Enter(gate.PlanPhase(parent.Offer()))
				}
				b := &assistantGateBindings{}
				b.add(parent)
				parentProviders := 0
				root, inner := assistantEnrollmentRun(t, engine, parent, &runcontext.Input{Phase: parentMode, CurrentRequest: &runcontext.CurrentRequest{Message: "Inspect", Input: "facts"}}, func([]adapter.AgentTool) { parentProviders++ })
				assistantRunMust(t, b.bind(root))
				assistantRunMust(t, b.adapter(inner).Run(t.Context(), root))
				if parentProviders != 1 {
					t.Fatal("parent did not execute")
				}
				before, _ := json.Marshal(parent.Offer())
				phase := parent.Phase()
				calls, tools := b.child(parent, contract.DelegatedTaskProfile{ID: "reader", Capabilities: []string{"read"}}, gate.Options{Turn: parent})
				childGate := b.gates[calls]
				if len(childGate.Offer().HostTools) != 0 || len(tools) != 0 {
					t.Fatal("child inherited initial host declarations or ungranted Runtime tools")
				}
				childProviders := 0
				template, childInner := assistantEnrollmentRun(t, engine, childGate, &runcontext.Input{Phase: parentMode, Objective: "Inspect the explicit facts"}, func([]adapter.AgentTool) {
					childProviders++
					if childGate.Calls().AdmitHost("inspect") != nil {
						t.Fatal("child read refused")
					}
					for _, name := range []string{"unknown", "propose_graph", "propose_pack", "propose_plan", "delegate"} {
						if childGate.Calls().AdmitHost(name) == nil {
							t.Fatal("child authority widened", name)
						}
					}
				})
				// Reproduce delegate's actual Run copy with a changed gate and input. The
				// inherited root callback must be replaced at the inner adapter boundary.
				child := *root
				child.Gate, child.Context, child.Model, child.Agent = template.Gate, template.Context, template.Model, template.Agent
				child.HostTools = template.HostTools
				assistantRunMust(t, b.adapter(childInner).Run(t.Context(), &child))
				after, _ := json.Marshal(parent.Offer())
				if childProviders != 1 || string(before) != string(after) || !reflect.DeepEqual(phase, parent.Phase()) {
					t.Fatal("child failed or changed parent authority")
				}
				if childGate.Phase().Kind != gate.PhaseChild {
					t.Fatal("child phase widened")
				}
			})
		}
	}
}
