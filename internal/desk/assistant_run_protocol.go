package desk

// The protocol accepts references only. Executable capabilities, canonical
// checkpoints, connection policy and source ownership are resolved by this host.
import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"reflect"

	"github.com/Judgment-Pack/judgment-pack-agent/go/contract"
)

var errAssistantRunUnavailable = errors.New("This assistant purpose is not available on the server yet.")

type assistantRunStart struct {
	AdmissionID string `json:"admissionId"`
	Purpose     string `json:"purpose"`
	OwnerID     string `json:"ownerId"`
	Revision    string `json:"revision"`
	RequestID   string `json:"requestId"`
}
type assistantRunAction struct {
	ID         string  `json:"id"`
	Kind       string  `json:"kind"`
	Revision   uint64  `json:"revision"`
	CallID     string  `json:"callId,omitempty"`
	ArgsDigest string  `json:"argsDigest,omitempty"`
	Approved   *bool   `json:"approved,omitempty"`
	Note       *string `json:"note,omitempty"`
}
type assistantRunActionReceipt struct {
	ID                 string `json:"id"`
	Digest             string `json:"digest"`
	Kind               string `json:"kind"`
	AuthorizingSession string `json:"authorizingSession,omitempty"`
	AuthorizingPolicy  string `json:"authorizingPolicy,omitempty"`
	// A receipt admits at most one delivery. An interrupted delivery is never
	// automatically repeated, even if its response was lost.
	Decision *contract.CheckpointApprovalDecision `json:"decision,omitempty"`
}
type assistantRunSource struct {
	Start      assistantRunStart
	Binding    assistantRunBinding
	Original   []byte
	Request    []byte
	Text       string
	Connection aiConnection
	Profile    assistantProfile
}

// Bound once by server implementation before serving. Prepare may only build
// context; it must not call a provider or execute a tool. Execute gets only the
// persisted host execution. No HTTP field selects an implementation.
type assistantRunExecutor interface {
	Prepare(context.Context, assistantRunSource) (contract.Checkpoint, error)
	Execute(context.Context, *assistantRunExecution) error
}

func (s *Server) assistantRunIdentity(r *http.Request) (assistantRunBinding, context.Context, error) {
	held, ok := s.sessionOf(r)
	if !ok || r.Context().Err() != nil {
		return assistantRunBinding{}, nil, errAssistantRunScope
	}
	if err := s.authorizeAssistantRunStorage(r); err != nil {
		return assistantRunBinding{}, nil, err
	}
	issuer := ""
	if held.issuer != nil {
		issuer = *held.issuer
	}
	policy := struct {
		Generation uint64
		Actor      string
	}{Actor: issuer + "\x00" + held.subject}
	if s.signIn != nil {
		s.signIn.mu.Lock()
		policy.Generation = s.signIn.generation
		s.signIn.mu.Unlock()
	}
	raw, _ := json.Marshal(policy)
	return assistantRunBinding{Principal: digestOf([]byte(issuer + "\x00" + held.subject)), Session: digestOf([]byte(s.sessions.handle(bearerOf(r)))), Policy: digestOf(raw)}, held.ctx, nil
}
func assistantRunIdentityMatches(a, b assistantRunBinding) bool {
	return a.Principal == b.Principal && a.Session == b.Session && a.Policy == b.Policy
}

// Closed registry: other journal owners require a concrete resolver and tests.
func (s *Server) resolveAssistantRun(r *http.Request, start assistantRunStart, identity assistantRunBinding, project string) (assistantRunSource, error) {
	if start.Purpose != "chat" {
		return assistantRunSource{}, errAssistantRunUnavailable
	}
	if !assistantRunText(start.AdmissionID, 128) || !researchChatID.MatchString(start.OwnerID) || !assistantRunText(start.RequestID, 128) || !assistantRunDigest(start.Revision) {
		return assistantRunSource{}, errAssistantRunInvalid
	}
	store, err := s.openChatData()
	if err != nil {
		return assistantRunSource{}, err
	}
	defer store.root.Close()
	name, err := resolveConversationName(store.root, s.conversationIdentity())
	if err != nil {
		return assistantRunSource{}, err
	}
	raw, err := readPrivateData(store.root, name, maxConversationBytes)
	if err != nil {
		return assistantRunSource{}, err
	}
	if !uniqueJSON(raw) || validateConversations(raw) != nil {
		return assistantRunSource{}, errAssistantRunInvalid
	}
	var doc conversationDocument
	if json.Unmarshal(raw, &doc) != nil {
		return assistantRunSource{}, errAssistantRunInvalid
	}
	var selected json.RawMessage
	for _, candidate := range doc.Chats {
		var id struct {
			ID string `json:"id"`
		}
		if json.Unmarshal(candidate, &id) != nil {
			return assistantRunSource{}, errAssistantRunInvalid
		}
		if id.ID == start.OwnerID {
			if selected != nil {
				return assistantRunSource{}, errAssistantRunInvalid
			}
			selected = candidate
		}
	}
	if selected == nil || digestOf(selected) != start.Revision {
		return assistantRunSource{}, errAssistantRunConflict
	}
	if err = s.authorizePolicyConversations(r, []byte(`{"version":1,"chats":[`+string(selected)+`]}`)); err != nil {
		return assistantRunSource{}, err
	}
	// Read declared fields by their exact wire names, not encoding/json aliases.
	var chat map[string]json.RawMessage
	_ = json.Unmarshal(selected, &chat)
	var phase, connectionID, model string
	_ = json.Unmarshal(chat["interaction"], &phase)
	_ = json.Unmarshal(chat["aiConnection"], &connectionID)
	_ = json.Unmarshal(chat["model"], &model)
	if phase == "" {
		phase = "work"
	}
	if !contains([]string{"work", "plan"}, phase) {
		return assistantRunSource{}, errAssistantRunInvalid
	}
	var checkpoint map[string]json.RawMessage
	var state map[string]json.RawMessage
	var turns []json.RawMessage
	if json.Unmarshal(chat["checkpoint"], &checkpoint) != nil || json.Unmarshal(checkpoint["state"], &state) != nil || json.Unmarshal(state["turns"], &turns) != nil {
		return assistantRunSource{}, errAssistantRunInvalid
	}
	var request json.RawMessage
	text := ""
	for _, turn := range turns {
		var fields map[string]json.RawMessage
		if json.Unmarshal(turn, &fields) != nil {
			return assistantRunSource{}, errAssistantRunInvalid
		}
		var role, id, kind, value string
		_ = json.Unmarshal(fields["role"], &role)
		_ = json.Unmarshal(fields["id"], &id)
		_ = json.Unmarshal(fields["kind"], &kind)
		_ = json.Unmarshal(fields["text"], &value)
		if role == "user" {
			request = nil
			text = ""
			if kind == "message" && id == start.RequestID && value != "" {
				request = turn
				text = value
			}
		}
	}
	if request == nil {
		return assistantRunSource{}, errAssistantRunScope
	}
	profile, err := s.readAssistantProfile()
	if err != nil {
		return assistantRunSource{}, err
	}
	registry, err := s.readAIRegistry()
	if err != nil {
		return assistantRunSource{}, err
	}
	var connection *aiConnection
	for _, candidate := range registry.Connections {
		if candidate.ID == connectionID {
			held := candidate
			connection = &held
			break
		}
	}
	if connection == nil || !connection.Enabled || !contains(connectionModels(connection, profile), model) || profile.Version == 2 && profile.Inherit != nil && !*profile.Inherit && !contains(profile.Connections, connectionID) {
		return assistantRunSource{}, errAssistantRunScope
	}
	profileRaw, _ := json.Marshal(profile)
	binding := identity
	binding.Project = project
	binding.Owner = assistantRunOwner{Kind: "chat", ID: start.OwnerID, Revision: start.Revision}
	binding.RequestID = start.RequestID
	binding.RequestDigest = digestOf(request)
	binding.SourceDigests = []string{digestOf(selected)}
	binding.ConnectionID = connectionID
	binding.ConnectionRevision = connection.Revision
	binding.ProfileRevision = digestOf(profileRaw)
	binding.Model = model
	binding.Phase = phase
	return assistantRunSource{Start: start, Binding: binding, Original: bytes.Clone(selected), Request: bytes.Clone(request), Text: text, Connection: *connection, Profile: *profile}, nil
}

func (s *Server) readAssistantRunSource(r *http.Request, start assistantRunStart, identity assistantRunBinding, project string) (assistantRunSource, error) {
	lock, err := s.privateDataLockContext(r.Context(), true)
	if err != nil {
		return assistantRunSource{}, err
	}
	defer lock.Close()
	return s.resolveAssistantRun(r, start, identity, project)
}
func (s *Server) authorizeAssistantRun(r *http.Request, run assistantRunRecord) error {
	identity, _, err := s.assistantRunIdentity(r)
	if err != nil {
		return err
	}
	if !assistantRunIdentityMatches(identity, run.Binding) {
		return errAssistantRunScope
	}
	return nil
}
func assistantRunActionDigest(action assistantRunAction) string {
	raw, _ := json.Marshal(action)
	return digestOf(raw)
}
func assistantRunArgsDigest(args map[string]json.RawMessage) string {
	raw, _ := json.Marshal(args)
	return digestOf(raw)
}

func validateAssistantRunActionReceipts(run assistantRunRecord) error {
	ids := map[string]bool{}
	if len(run.Actions) > assistantRunOperationLimit {
		return errAssistantRunInvalid
	}
	for _, receipt := range run.Actions {
		if !assistantRunText(receipt.ID, 128) || !assistantRunDigest(receipt.Digest) || ids[receipt.ID] || !contains([]string{"cancel", "approval", "abandon"}, receipt.Kind) {
			return errAssistantRunInvalid
		}
		ids[receipt.ID] = true
		if receipt.Kind == "abandon" {
			if !assistantRunDigest(receipt.AuthorizingSession) || !assistantRunDigest(receipt.AuthorizingPolicy) || run.State != "stopped" {
				return errAssistantRunInvalid
			}
		} else if receipt.AuthorizingSession != "" || receipt.AuthorizingPolicy != "" {
			return errAssistantRunInvalid
		}
		if receipt.Kind == "approval" {
			if receipt.Decision == nil || !assistantRunText(receipt.Decision.CallID, 256) || receipt.Decision.Note != nil && len(*receipt.Decision.Note) > 8000 {
				return errAssistantRunInvalid
			}
		} else if receipt.Decision != nil {
			return errAssistantRunInvalid
		}
	}
	return nil
}
func (s assistantRunStore) lookup(ctx context.Context, identity assistantRunBinding, id, admission string, historicalRead bool) (assistantRunRecord, error) {
	var out assistantRunRecord
	_, err := s.transaction(ctx, func(ledger *assistantRunLedger) (bool, error) {
		for _, run := range ledger.Runs {
			if id != "" && run.ID == id || admission != "" && run.AdmissionID == admission && run.Binding.Principal == identity.Principal {
				if !assistantRunIdentityMatches(identity, run.Binding) && !(historicalRead && identity.Principal == run.Binding.Principal && run.Binding.Project == s.project && (assistantRunTerminal(run.State) || assistantRunPaused(run.State))) {
					return false, errAssistantRunScope
				}
				out = run
				return false, nil
			}
		}
		return false, errAssistantRunNotFound
	})
	return out, err
}

var errAssistantRunNotFound = errors.New("Assistant run is unavailable in this signed-in window.")

func assistantRunFreshSourceEqual(a, b assistantRunSource) bool {
	return reflect.DeepEqual(a.Binding, b.Binding) && bytes.Equal(a.Original, b.Original) && bytes.Equal(a.Request, b.Request)
}
func assistantRunCursor(run assistantRunRecord) string {
	return fmt.Sprintf("%d:%d", run.Revision, len(run.Events))
}

// Pauses are inspectable by a current authorized owner, but retain their original
// session/Policy binding. Only explicit abandonment may release that reservation.
func assistantRunPaused(state string) bool {
	return state == "awaiting-input" || state == "paused-budget" || state == "awaiting-client"
}
