package desk

import (
	"bytes"
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"regexp"
	"strings"
	"time"

	runcontext "github.com/Judgment-Pack/judgment-pack-agent/go/context"
	"github.com/Judgment-Pack/judgment-pack-agent/go/contract"
)

func assistantMemoryProposalPolicy(binding assistantRunBinding) runcontext.PolicyLayer {
	if binding.MemoryProposals != nil && *binding.MemoryProposals {
		return nil // Preserve the released table; no host-authored allowance grant.
	}
	return runcontext.PolicyLayer{"budget": map[string]any{"memoryProposalsPerRun": 0}}
}

type memoryReviewChange struct {
	ID             string  `json:"id"`
	RunID          string  `json:"runId"`
	RunRevision    uint64  `json:"runRevision"`
	ProposalID     string  `json:"proposalId"`
	ProposalDigest string  `json:"proposalDigest"`
	Decision       string  `json:"decision"`
	Text           *string `json:"text,omitempty"`
}

// A receipt and its effect live in the same atomic memory-document replacement.
// It retains an exact intent digest and result identity, never deleted prose.
type memoryReviewReceipt struct {
	ID             string `json:"id"`
	IntentDigest   string `json:"intentDigest"`
	Principal      string `json:"principal"`
	Session        string `json:"session"`
	Policy         string `json:"policy"`
	Project        string `json:"project"`
	RunID          string `json:"runId"`
	RunRevision    uint64 `json:"runRevision"`
	ProposalID     string `json:"proposalId"`
	ProposalDigest string `json:"proposalDigest"`
	Decision       string `json:"decision"`
	EntryID        string `json:"entryId,omitempty"`
	MemoryID       string `json:"memoryId,omitempty"`
	MemoryDigest   string `json:"memoryDigest,omitempty"`
	At             string `json:"at"`
}

type memoryProposalReview struct {
	RunID       string          `json:"runId"`
	RunRevision uint64          `json:"runRevision"`
	ChatID      string          `json:"chatId"`
	TurnID      string          `json:"turnId"`
	Memory      contract.Memory `json:"memory"`
	Reviewable  bool            `json:"reviewable"`
}

func (change *memoryReviewChange) UnmarshalJSON(raw []byte) error {
	fields, err := assistantTestControls(raw, "id", "runId", "runRevision", "proposalId", "proposalDigest", "decision", "text")
	if err != nil {
		return err
	}
	for _, name := range []string{"id", "runId", "runRevision", "proposalId", "proposalDigest", "decision"} {
		if len(fields[name]) == 0 || bytes.Equal(bytes.TrimSpace(fields[name]), []byte("null")) {
			return errAssistantRunInvalid
		}
	}
	for _, value := range fields {
		if bytes.Equal(bytes.TrimSpace(value), []byte("null")) {
			return errAssistantRunInvalid
		}
	}
	type plain memoryReviewChange
	return decodeDataJSON(raw, (*plain)(change))
}

func (receipt *memoryReviewReceipt) UnmarshalJSON(raw []byte) error {
	fields, err := assistantTestControls(raw, "id", "intentDigest", "principal", "session", "policy", "project", "runId", "runRevision", "proposalId", "proposalDigest", "decision", "entryId", "memoryId", "memoryDigest", "at")
	if err != nil {
		return err
	}
	for _, name := range []string{"id", "intentDigest", "principal", "session", "policy", "project", "runId", "runRevision", "proposalId", "proposalDigest", "decision", "at"} {
		if len(fields[name]) == 0 || bytes.Equal(bytes.TrimSpace(fields[name]), []byte("null")) {
			return errAssistantRunInvalid
		}
	}
	for _, value := range fields {
		if bytes.Equal(bytes.TrimSpace(value), []byte("null")) {
			return errAssistantRunInvalid
		}
	}
	type plain memoryReviewReceipt
	return decodeDataJSON(raw, (*plain)(receipt))
}

var memoryReviewRunID = regexp.MustCompile(`^arun_[a-f0-9]{32}$`)

func validateMemoryReviews(receipts []memoryReviewReceipt) error {
	if len(receipts) > 512 {
		return errors.New("Project memory review history is full.")
	}
	ids, proposals := map[string]bool{}, map[string]bool{}
	for _, receipt := range receipts {
		key := receipt.RunID + "/" + receipt.ProposalID
		if !assistantRunText(receipt.ID, 128) || ids[receipt.ID] || proposals[key] || !memoryReviewRunID.MatchString(receipt.RunID) || receipt.RunRevision == 0 || !assistantRunText(receipt.ProposalID, 100) || !contains([]string{"accept", "decline", "edit"}, receipt.Decision) {
			return errAssistantRunInvalid
		}
		ids[receipt.ID], proposals[key] = true, true
		for _, digest := range []string{receipt.IntentDigest, receipt.Principal, receipt.Session, receipt.Policy, receipt.Project, strings.TrimPrefix(receipt.ProposalDigest, "sha256:")} {
			if !assistantRunDigest(digest) {
				return errAssistantRunInvalid
			}
		}
		if !strings.HasPrefix(receipt.ProposalDigest, "sha256:") {
			return errAssistantRunInvalid
		}
		if _, err := time.Parse(time.RFC3339Nano, receipt.At); err != nil {
			return errAssistantRunInvalid
		}
		if receipt.Decision == "decline" {
			if receipt.EntryID != "" || receipt.MemoryID != "" || receipt.MemoryDigest != "" {
				return errAssistantRunInvalid
			}
		} else if !assistantRunText(receipt.EntryID, 128) || !assistantRunText(receipt.MemoryID, 100) || !strings.HasPrefix(receipt.MemoryDigest, "sha256:") || !assistantRunDigest(strings.TrimPrefix(receipt.MemoryDigest, "sha256:")) {
			return errAssistantRunInvalid
		}
		if receipt.Decision == "accept" && (receipt.MemoryID != receipt.ProposalID || receipt.MemoryDigest != receipt.ProposalDigest) || receipt.Decision == "edit" && receipt.MemoryID == receipt.ProposalID {
			return errAssistantRunInvalid
		}
	}
	return nil
}

func memoryReviewIntent(change memoryReviewChange, revision string) string {
	raw, _ := json.Marshal(struct {
		Change   memoryReviewChange
		Revision string
	}{change, revision})
	return digestOf(raw)
}

// Caller owns the existing private-data lock. This read never recovers or writes
// the execution ledger, and model fields cannot nominate a file or principal.
func (s *Server) memoryProposalRuns(root *os.Root, memoryName string) (assistantRunLedger, error) {
	project := strings.TrimSuffix(strings.TrimPrefix(memoryName, "ai-memory-"), ".json")
	name := "assistant-runs-" + project + ".json"
	raw, err := readPrivateData(root, name, assistantRunRecordLimit)
	if errors.Is(err, os.ErrNotExist) {
		return assistantRunLedger{Version: 1, Project: project, Runs: []assistantRunRecord{}}, nil
	}
	if err != nil {
		return assistantRunLedger{}, err
	}
	return decodeAssistantRunStorage(name, raw)
}

// Independent creation reviewers cannot author private memory, even though the
// released session is a root Work session rather than CriticReading.
func assistantMemoryIndependentReview(action *assistantCreationAction) bool {
	return action != nil && (action.Kind == "establish-cases" || action.Kind == "propose-correction")
}

func memoryTopic(proposal contract.Memory, text string) string {
	return "canonical-" + proposal.Kind + "-" + memoryHash(proposal.Kind + "\x00" + text)[:32]
}

// Attribution is the latest actual saved user turn, including Plan. It is not
// inferred from remember prose, model tool arguments or a historical transcript.
func (s *Server) memoryReviewSource(r *http.Request, root *os.Root, run assistantRunRecord) (*memorySource, string, string, []memoryDependency, error) {
	identity, _, err := s.assistantRunIdentity(r)
	if err != nil || !assistantRunIdentityMatches(identity, run.Binding) || run.Historical || run.Binding.Owner.Kind != "chat" {
		return nil, "", "", nil, errAssistantRunScope
	}
	name, err := resolveConversationName(root, s.conversationIdentity())
	if err != nil {
		return nil, "", "", nil, err
	}
	raw, err := readPrivateData(root, name, maxConversationBytes)
	if err != nil {
		return nil, "", "", nil, err
	}
	_, rows, err := assistantChatRows(raw)
	if err != nil {
		return nil, "", "", nil, err
	}
	index, err := assistantChatSelected(rows, run.Binding.Owner.ID)
	if err != nil {
		return nil, "", "", nil, err
	}
	if s.authorizePolicyConversations(r, []byte(`{"version":1,"chats":[`+string(rows[index])+`]}`)) != nil {
		return nil, "", "", nil, errAssistantRunScope
	}
	var chat struct {
		Checkpoint struct {
			State struct {
				Turns []json.RawMessage `json:"turns"`
			} `json:"state"`
		} `json:"checkpoint"`
	}
	if json.Unmarshal(rows[index], &chat) != nil {
		return nil, "", "", nil, errAssistantRunInvalid
	}
	var latest json.RawMessage
	for _, rawTurn := range chat.Checkpoint.State.Turns {
		var turn struct{ Role, Kind string }
		if json.Unmarshal(rawTurn, &turn) != nil {
			return nil, "", "", nil, errAssistantRunInvalid
		}
		if turn.Role == "user" && turn.Kind != "note" {
			latest = rawTurn
		}
	}
	if digestOf(latest) != run.Binding.RequestDigest {
		return nil, "", "", nil, errAssistantRunScope
	}
	action, err := readAssistantCreationAction(latest)
	if err != nil || assistantMemoryIndependentReview(action) {
		return nil, "", "", nil, errAssistantRunScope
	}
	var turn struct{ ID, Text, Statement, At string }
	if json.Unmarshal(latest, &turn) != nil || turn.ID != run.Binding.RequestID {
		return nil, "", "", nil, errAssistantRunScope
	}
	when, err := time.Parse(time.RFC3339Nano, turn.At)
	if err != nil {
		return nil, "", "", nil, errAssistantRunInvalid
	}
	text := turn.Statement
	if text == "" {
		text = turn.Text
	}
	source := &memorySource{ChatID: run.Binding.Owner.ID, TurnID: turn.ID}
	digest := memoryHash(source.ChatID + "/" + source.TurnID + "/" + text)
	deps := []memoryDependency{}
	if retained := run.SavedPack; retained != nil {
		if retained.Read.Draft || retained.Basis.Path != retained.Read.Path || digestOf(retained.Read.Pack) != retained.Read.PackDigest {
			return nil, "", "", nil, errAssistantRunInvalid
		}
		deps = append(deps, memoryDependency{Path: retained.Read.Path, SHA256: retained.Read.PackDigest})
	}
	if err = s.validMemoryDependencies(deps); err != nil {
		return nil, "", "", nil, err
	}
	return source, when.UTC().Format(time.RFC3339Nano), digest, deps, nil
}

func memoryProposal(run assistantRunRecord, id string) (contract.Memory, error) {
	if run.Binding.MemoryProposals == nil || !*run.Binding.MemoryProposals || run.Checkpoint == nil || run.Checkpoint.Memories == nil {
		return contract.Memory{}, errAssistantRunConflict
	}
	for _, proposal := range run.Checkpoint.Memories.Proposed {
		if proposal.ID == id {
			if proposal.State != "proposed" || proposal.Scope != "project" || proposal.Source.Run != run.Checkpoint.ID || proposal.Source.ProposedBy != "model" {
				return contract.Memory{}, errAssistantRunInvalid
			}
			if _, err := runcontext.RecallMemory([]contract.Memory{proposal}, 0, ""); err != nil {
				return contract.Memory{}, errAssistantRunInvalid
			}
			return proposal, nil
		}
	}
	return contract.Memory{}, errAssistantRunConflict
}

func memoryProposalAllowed(d memoryDocument, proposal contract.Memory, sourceDigest, at, text string) bool {
	if memoryBlocked(d, memoryTopic(proposal, text), text, sourceDigest) {
		return false
	}
	if d.GenerationAfter != "" {
		cutoff, err := time.Parse(time.RFC3339Nano, d.GenerationAfter)
		if err != nil {
			return false
		}
		when, err := time.Parse(time.RFC3339Nano, at)
		if err != nil || !when.After(cutoff) {
			return false
		}
	}
	return true
}

func (s *Server) projectMemoryProposals(r *http.Request, root *os.Root, reply *memoryReply) error {
	ledger, err := s.memoryProposalRuns(root, reply.Name)
	if err != nil {
		return err
	}
	if len(ledger.Runs) == 0 {
		return nil
	}
	identity, _, err := s.assistantRunIdentity(r)
	if err != nil {
		return err
	}
	done := map[string]bool{}
	for _, receipt := range reply.Content.Reviews {
		done[receipt.RunID+"/"+receipt.ProposalID] = true
	}
	reply.Proposals = nil
	for _, run := range ledger.Runs {
		if !assistantRunIdentityMatches(identity, run.Binding) || run.Historical || run.Binding.Owner.Kind != "chat" || run.Binding.MemoryProposals == nil || !*run.Binding.MemoryProposals || run.Checkpoint == nil || run.Checkpoint.Memories == nil {
			continue
		}
		_, at, sourceDigest, _, currentErr := s.memoryReviewSource(r, root, run)
		if currentErr != nil && reply.Content.GenerationAfter != "" {
			continue
		}
		for _, raw := range run.Checkpoint.Memories.Proposed {
			proposal, err := memoryProposal(run, raw.ID)
			if err != nil {
				return err
			}
			if done[run.ID+"/"+proposal.ID] || currentErr == nil && !memoryProposalAllowed(reply.Content, proposal, sourceDigest, at, proposal.Text) {
				continue
			}
			reply.Proposals = append(reply.Proposals, memoryProposalReview{RunID: run.ID, RunRevision: run.Revision, ChatID: run.Binding.Owner.ID, TurnID: run.Binding.RequestID, Memory: proposal, Reviewable: currentErr == nil && (assistantRunTerminal(run.State) || assistantRunPaused(run.State))})
		}
	}
	return nil
}

func (s *Server) reviewMemory(r *http.Request, root *os.Root, reply *memoryReply, change memoryReviewChange) (bool, error) {
	if !assistantRunText(change.ID, 128) || !assistantRunText(change.RunID, 37) || change.RunRevision == 0 || !assistantRunText(change.ProposalID, 100) || !contains([]string{"accept", "decline", "edit"}, change.Decision) || ((change.Decision == "edit") != (change.Text != nil)) {
		return false, errAssistantRunInvalid
	}
	identity, _, err := s.assistantRunIdentity(r)
	if err != nil {
		return false, err
	}
	project := strings.TrimSuffix(strings.TrimPrefix(reply.Name, "ai-memory-"), ".json")
	intent := memoryReviewIntent(change, r.Header.Get("If-Match"))
	for _, receipt := range reply.Content.Reviews {
		if receipt.ID == change.ID {
			if receipt.IntentDigest != intent || receipt.Principal != identity.Principal || receipt.Session != identity.Session || receipt.Policy != identity.Policy || receipt.Project != project {
				return false, errAssistantRunConflict
			}
			return false, nil // Known completed receipt only; never recreate its entry.
		}
		if receipt.RunID == change.RunID && receipt.ProposalID == change.ProposalID {
			return false, errAssistantRunConflict
		}
	}
	if reply.SHA256 != r.Header.Get("If-Match") {
		return false, withCode(CodeStale, errAssistantRunConflict)
	}
	ledger, err := s.memoryProposalRuns(root, reply.Name)
	if err != nil {
		return false, err
	}
	var run *assistantRunRecord
	for i := range ledger.Runs {
		if ledger.Runs[i].ID == change.RunID {
			run = &ledger.Runs[i]
		}
	}
	if run == nil || run.Revision != change.RunRevision || !(assistantRunTerminal(run.State) || assistantRunPaused(run.State)) {
		return false, errAssistantRunConflict
	}
	proposal, err := memoryProposal(*run, change.ProposalID)
	if err != nil || proposal.Digest != change.ProposalDigest {
		return false, errAssistantRunConflict
	}
	source, at, sourceDigest, deps, err := s.memoryReviewSource(r, root, *run)
	if err != nil {
		return false, err
	}
	text := proposal.Text
	if change.Text != nil {
		text = *change.Text
	}
	if !memoryProposalAllowed(reply.Content, proposal, sourceDigest, at, text) || !memoryProposalAllowed(reply.Content, proposal, sourceDigest, at, proposal.Text) {
		return false, errors.New("This proposal was forgotten or excluded by Clear. Nothing was accepted.")
	}
	if len(reply.Content.Reviews) >= 512 {
		return false, errors.New("Project memory review history is full.")
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	receipt := memoryReviewReceipt{ID: change.ID, IntentDigest: intent, Principal: identity.Principal, Session: identity.Session, Policy: identity.Policy, Project: project, RunID: run.ID, RunRevision: run.Revision, ProposalID: proposal.ID, ProposalDigest: proposal.Digest, Decision: change.Decision, At: now}
	if change.Decision != "decline" {
		if strings.TrimSpace(text) == "" || len(text) > 2000 || strings.ContainsRune(text, 0) || memorySensitive(text) {
			return false, errors.New("Use memory text of at most 2,000 bytes without credentials or secrets.")
		}
		if len(reply.Content.Entries) >= 256 {
			return false, errors.New("Project memory is full. Forget unused entries before accepting more.")
		}
		canonical := &memoryCanonical{ID: proposal.ID, Kind: proposal.Kind, Source: proposal.Source, Digest: proposal.Digest, ExpiresAt: proposal.ExpiresAt}
		created := proposal.CreatedAt
		if change.Decision == "edit" {
			identity := runcontext.ContextDigest(project + "/" + receipt.Principal + "/" + change.ID + "/" + proposal.ID + "/" + text)
			canonical = &memoryCanonical{ID: "desk-memory-person:" + identity, Kind: proposal.Kind, Source: contract.MemorySource{Run: "desk-memory-review:" + identity, ProposedBy: "person"}, Digest: "sha256:" + runcontext.ContextDigest(text), ExpiresAt: proposal.ExpiresAt}
			created = now
		}
		entry := memoryEntry{ID: randomStartupID(), Key: memoryTopic(proposal, text), Text: text, State: "active", CreatedAt: created, UpdatedAt: now, Source: source, SourceDigest: sourceDigest, Dependencies: deps, Canonical: canonical}
		if memoryExpired(entry) {
			return false, errors.New("This memory proposal has expired.")
		}
		for i := range reply.Content.Entries {
			if reply.Content.Entries[i].Key == entry.Key {
				reply.Content.Entries[i].State = "superseded"
			}
		}
		reply.Content.Entries = append(reply.Content.Entries, entry)
		receipt.EntryID, receipt.MemoryID, receipt.MemoryDigest = entry.ID, canonical.ID, canonical.Digest
	}
	reply.Content.Reviews = append(reply.Content.Reviews, receipt)
	current, _, err := s.assistantRunIdentity(r)
	if err != nil || !assistantRunIdentityMatches(identity, current) {
		return false, errAssistantRunScope
	}
	return true, nil
}
