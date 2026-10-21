package desk

import (
	"bytes"
	"encoding/json"
	"net/http"
	"os"
	"strings"
)

// Materialization owns no Runtime/model callback. The immutable prepared output
// is already durable. Repeating this transaction only reconciles exact bytes.
func (s *Server) materializeAssistantTests(r *http.Request, id string) error {
	store := s.assistantRuns()
	_, err := store.transaction(r.Context(), func(ledger *assistantRunLedger) (bool, error) {
		run, err := assistantChatProjectionRun(ledger, id)
		if err != nil {
			return false, err
		}
		if err := s.authorizeAssistantRun(r, *run); err != nil {
			return false, err
		}
		data, err := s.openChatData()
		if err != nil {
			return false, err
		}
		defer data.root.Close()
		if err := checkPrivateDataPath(data); err != nil {
			return false, err
		}
		if err := store.checkScope(data.root); err != nil {
			return false, err
		}
		name, err := resolveConversationName(data.root, s.conversationIdentity())
		if err != nil {
			return false, err
		}
		chats, err := readPrivateData(data.root, name, maxConversationBytes)
		if err != nil {
			return false, err
		}
		_, rows, err := assistantChatRows(chats)
		if err != nil {
			return false, err
		}
		index, err := assistantChatSelected(rows, run.Binding.Owner.ID)
		if err != nil {
			return false, err
		}
		if !assistantChatProjectionMatches(run, rows[index]) || !assistantChatRequest(rows[index], run.Binding.RequestID, run.Binding.RequestDigest) {
			return false, errAssistantRunScope
		}
		if err := s.authorizePolicyConversations(r, chats); err != nil {
			return false, err
		}
		if err := validateAssistantDomainOutputs(*run); err != nil {
			return false, err
		}
		if err := validateAssistantTestProjectionSource(rows[index], *run); err != nil {
			return false, err
		}
		return false, s.materializeAssistantTestOutputsLocked(r, data.root, name, *run)
	})
	return err
}
func (s *Server) materializeAssistantTestOutputsLocked(r *http.Request, root *os.Root, conversation string, run assistantRunRecord) error {
	outputs := []assistantTestProposalOutput{}
	for _, out := range run.DomainOutputs {
		if out.Kind == "test-proposal" {
			outputs = append(outputs, *out.TestProposal)
		}
	}
	if len(outputs) == 0 {
		return nil
	}
	name := strings.Replace(conversation, "conversations-", "pack-tests-", 1)
	saved, err := s.readWorkspaceRecord(root, name, false)
	if err != nil {
		return err
	}
	store, err := assistantTestObject(saved.Content)
	if err != nil {
		return err
	}
	suites, err := assistantTestObject(store["suites"])
	if err != nil {
		return err
	}
	changed := false
	for _, output := range outputs {
		raw, present := suites[output.Read.Owner]
		if !present {
			raw = []byte(`{"revision":0,"cases":[],"runs":[],"recovered":[]}`)
		}
		suite, err := assistantTestSuite(raw)
		if err != nil {
			return err
		}
		proposals := []json.RawMessage{}
		if v := suite["proposals"]; v != nil {
			proposals, err = assistantChatArray(v)
			if err != nil {
				return err
			}
		}
		proposal, err := assistantTestObject(output.Proposal)
		if err != nil {
			return err
		}
		id, _ := assistantTestString(proposal["id"])
		existing := false
		seen := map[string]bool{}
		for _, p := range proposals {
			f, err := assistantTestObject(p)
			if err != nil {
				return err
			}
			held, err := assistantTestString(f["id"])
			if err != nil || seen[held] {
				return errAssistantRunConflict
			}
			seen[held] = true
			if held == id {
				if !bytes.Equal(p, output.Proposal) {
					return errAssistantRunConflict
				}
				existing = true
			}
		}
		if existing {
			continue
		}
		// Existing exact proposals are historical observations even if the pack
		// later changes/finalizes. A new append still requires current source
		// and lifecycle custody, with no Runtime/model replay under this lock.
		config, _, err := s.assistantDecisionDeclarations()
		if err != nil {
			return err
		}
		current, err := s.readAssistantTestTargetLocked(root, conversation, config, output.Read.Owner)
		if err != nil || !assistantTestSame(output.Read, current) || !assistantTestCoverageSame(output.Read, current) {
			return errAssistantRunConflict
		}
		before, err := assistantTestSuite(output.Read.Suite)
		if err != nil {
			return err
		}
		if present != output.Read.SuitePresent || !bytes.Equal(suite["cases"], before["cases"]) {
			return errAssistantRunConflict
		}
		proposals = append(proposals, json.RawMessage(output.Proposal))
		suite["proposals"] = assistantChatEncodeArray(proposals)
		var revision int64
		_ = json.Unmarshal(suite["revision"], &revision)
		if revision >= 9007199254740991 {
			return errAssistantRunInvalid
		}
		suite["revision"] = assistantChatJSON(revision + 1)
		suites[output.Read.Owner] = assistantChatEncodeObject(suite)
		changed = true
	}
	if !changed {
		return nil
	}
	store["suites"] = assistantChatEncodeObject(suites)
	next := assistantChatEncodeObject(store)
	if len(next) > maxConversationBytes || validatePackTests(next) != nil {
		return errAssistantRunInvalid
	}
	if err := s.authorizeAssistantRun(r, run); err != nil {
		return err
	}
	if err := writePrivateData(root, name, next); err != nil {
		return err
	}
	readback, err := readPrivateData(root, name, maxConversationBytes)
	if err != nil {
		return err
	}
	if !bytes.Equal(next, readback) {
		return errAssistantRunConflict
	}
	return nil
}
