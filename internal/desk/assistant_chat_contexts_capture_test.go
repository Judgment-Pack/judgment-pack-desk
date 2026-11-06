package desk

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/Judgment-Pack/judgment-pack-agent/go/contract"
)

var errContextCapturePrepared = errors.New("capture restart probe reached preparation")

type assistantContextCaptureExecutor struct {
	source   assistantRunSource
	prepared int
	executed int
}

func (x *assistantContextCaptureExecutor) Prepare(_ context.Context, source assistantRunSource) (contract.Checkpoint, error) {
	x.source = source
	x.prepared++
	return contract.Checkpoint{}, errContextCapturePrepared
}
func (x *assistantContextCaptureExecutor) Execute(context.Context, *assistantRunExecution) error {
	x.executed++
	return errContextCapturePrepared
}

// Probe only the existing immutable capture and admission-to-Prepare boundary.
// It deliberately stops before admission, Runtime setup or provider execution.
func TestAssistantContextCaptureRestart(t *testing.T) {
	for _, changed := range []bool{false, true} {
		name := "unchanged-source"
		if changed {
			name = "changed-source-refused"
		}
		t.Run(name, func(t *testing.T) {
			s, token := assistantAuthoringFixture(t, "https://example.invalid")
			executor := &assistantContextCaptureExecutor{}
			s.assistantRunExecutor = executor
			request := assistantAuthoringRequestFixture("capture-restart", "author-pack")
			original := assistantAuthoringCaptureFixture(t, s, token, request)
			before := assistantAuthoringLedger(t, s)
			if len(before.AuthoringCaptures) != 1 || len(before.Runs) != 0 {
				t.Fatal("capture allocated work")
			}
			captured := before.AuthoringCaptures[0]
			previousDocument := assistantContextDocumentFixture(t, s, token, "previous-document")
			previousPane := assistantContextPaneFixture(t, s, token, previousDocument.Document, "previous-pane", "author-pack")
			old := s.assistantRunService()
			old.close()
			s.assistantRunServiceMu.Lock()
			s.assistantRunHost = nil
			s.assistantRunServiceMu.Unlock()
			if s.assistantRunService() == old {
				t.Fatal("service was not replaced")
			}
			if changed {
				writeProjectFile(t, s.projectDir, "one.pack.json", reviewPack+"\n")
			}
			if !changed {
				response := assistantBriefCall(s, token, "GET", "/api/assistant-authoring/captures/capture-restart", nil)
				if response.Code != 200 {
					t.Fatal("capture recovery", response.Code)
				}
				var found assistantAuthoringReply
				assistantRunMust(t, json.Unmarshal(response.Body.Bytes(), &found))
				if found.Start == nil || *found.Start != *original.Start || found.Run != nil {
					t.Fatal("recovery changed original reference")
				}
				repeated := assistantAuthoringCaptureFixture(t, s, token, request)
				if *repeated.Start != *original.Start {
					t.Fatal("retry replaced original capture")
				}
			}
			// The recovered capture does not resurrect its previous ephemeral scope.
			if w := policyCall(s, token, "POST", "/api/assistant-contexts/panes", assistantContextPaneRequest{previousPane.Document, "previous-pane", "author-pack"}); w.Code != 404 {
				t.Fatal("old document resurrected", w.Code)
			}
			currentDocument := assistantContextDocumentFixture(t, s, token, "current-document")
			currentPane := assistantContextPaneFixture(t, s, token, currentDocument.Document, "current-pane", "author-pack")
			start := *original.Start
			start.Context = &assistantRunContextSelector{Document: currentPane.Document, Pane: currentPane.Pane}
			response := assistantBriefCall(s, token, "POST", "/api/assistant-runs", start)
			if response.Code < 400 {
				t.Fatal("probe must stop before admission")
			}
			if changed {
				if executor.prepared != 0 {
					t.Fatal("changed source reached preparation")
				}
			} else {
				if executor.prepared != 1 {
					t.Fatal("unchanged capture could not reach preparation after service replacement", response.Code)
				}
				if !bytes.Equal(executor.source.Request, captured.Request) || !bytes.Equal(executor.source.Original, captured.Snapshot) || !sameAssistantAuthoringReference(executor.source.Start, *original.Start) || executor.source.Start.Context == nil || *executor.source.Start.Context != *start.Context {
					t.Fatal("recovery changed immutable source")
				}
			}
			after := assistantAuthoringLedger(t, s)
			if executor.executed != 0 || len(after.Runs) != 0 || len(after.AuthoringCaptures) != 1 {
				t.Fatal("probe created execution or duplicate capture")
			}
			if !bytes.Equal(after.AuthoringCaptures[0].Request, captured.Request) || !bytes.Equal(after.AuthoringCaptures[0].Snapshot, captured.Snapshot) || after.AuthoringCaptures[0].Revision != captured.Revision {
				t.Fatal("capture custody changed")
			}
		})
	}
}
