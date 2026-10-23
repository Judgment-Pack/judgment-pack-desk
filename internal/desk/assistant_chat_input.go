package desk

// Saved chat is input data. This projection never restores approvals, execution
// allowance, tool definitions, source verification or saved artifact authority.
import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"math/big"
	"regexp"
	"strconv"
	"strings"
	"sync"

	"github.com/Judgment-Pack/judgment-pack-agent/go/adapter/turn"
	runcontext "github.com/Judgment-Pack/judgment-pack-agent/go/context"
	"github.com/Judgment-Pack/judgment-pack-agent/go/contract"
)

var assistantChatID = regexp.MustCompile(`^[a-zA-Z0-9_-]{1,100}$`)

type assistantChatInput struct {
	Policy          *assistantPolicyScope
	Request         runcontext.CurrentRequest
	Documents       []runcontext.Document
	Transcript      string
	State           string
	Review          *bool
	Critic          string
	CriticPurpose   string
	CriticProject   string
	CriticReasoning string
	Thinking        string
	Reasoning       *runcontext.Value
	References      []assistantDocumentReference
	Attachments     []assistantChatAttachmentGroup
}

type assistantChatAttachmentGroup struct {
	Bytes      int
	References []assistantDocumentReference
}

// This pointer belongs to one retained execution; reconstructed capabilities
// share the immutable custody snapshot, never a copied mutex or browser hook.
type assistantChatCustody struct {
	planMu      sync.Mutex
	mu          sync.Mutex
	ready       bool
	documents   []assistantVerifiedDocument
	critic      *assistantCriticSnapshot
	policyReady bool
}

func assistantChatReferenceKey(ref assistantDocumentReference) string {
	raw, _ := json.Marshal(ref)
	return string(raw)
}

func assistantChatVerifiedContext(held assistantVerifiedDocument) runcontext.Document {
	document := held.contextDocument()
	// Selection/consent copies never overwrite or union another selection of
	// the same attachment. Citations keep the original document page identity.
	document.ID += "-" + digestOf([]byte(assistantChatReferenceKey(held.reference)))[:16]
	return document
}

func (e *assistantRunExecution) prepareAssistantChatInput(ctx context.Context) (*assistantChatInput, error) {
	if err := e.Authorize(ctx); err != nil {
		return nil, err
	}
	chat, err := compileAssistantChatInput(e.source)
	if err != nil {
		return nil, err
	}
	if err := e.prepareAssistantPolicy(ctx, chat.Policy); err != nil {
		return nil, err
	}
	held, err := e.holdAssistantDocuments(ctx, chat.References)
	if err != nil {
		return nil, err
	}
	values := map[string]runcontext.Document{}
	for _, document := range held {
		value := assistantChatVerifiedContext(document)
		values[assistantChatReferenceKey(document.reference)] = value
		chat.Documents = append(chat.Documents, value)
	}
	for _, group := range chat.Attachments {
		size := group.Bytes
		for _, ref := range group.References {
			size += len(values[assistantChatReferenceKey(ref)].Value.JSON())
		}
		if size > 800000 {
			return nil, errAssistantRunInvalid
		}
	}
	if err := assistantRunAnswerHistory(e.record, chat); err != nil {
		return nil, err
	}
	return chat, nil
}

func (e *assistantRunExecution) holdAssistantDocuments(ctx context.Context, refs []assistantDocumentReference) ([]assistantVerifiedDocument, error) {
	if e.input == nil {
		return nil, errAssistantRunUnavailable
	}
	e.input.mu.Lock()
	defer e.input.mu.Unlock()
	if !e.input.ready {
		held := make([]assistantVerifiedDocument, 0, len(refs))
		for _, ref := range refs {
			document, err := e.service.server.readAssistantDocument(e.request.WithContext(ctx), e.source, ref)
			if err != nil {
				return nil, err
			}
			held = append(held, document)
		}
		e.input.documents, e.input.ready = held, true
	}
	return append([]assistantVerifiedDocument(nil), e.input.documents...), nil
}

// One host-only physical-dispatch fence serves both relay and native Agent
// generation, after their waits. Source resolution remains shape-only, so no
// verification or recursive private lock is introduced inside its transaction.
func (e *assistantRunExecution) recheckAssistantInput(ctx context.Context) error {
	chat, err := e.prepareAssistantChatInput(ctx)
	if err != nil {
		e.cancel()
		return err
	}
	held, err := e.holdAssistantDocuments(ctx, chat.References)
	if err != nil {
		e.cancel()
		return err
	}
	for _, document := range held {
		if err := e.service.server.recheckAssistantDocument(e.request.WithContext(ctx), document); err != nil {
			e.cancel()
			return err
		}
	}
	if err := e.recheckAssistantCritic(ctx); err != nil {
		return err
	}
	return ctx.Err()
}

// Parse using the published value type, after refusing duplicate members,
// malformed Unicode and numbers its JS-compatible representation would change.
// Exact original candidate/source strings remain strings, never reparsed facts.
func assistantChatValue(raw []byte) (*runcontext.Value, error) {
	if len(raw) > maxConversationBytes || !validUTF8(raw) || !uniqueJSON(raw) || !json.Valid(raw) {
		return nil, errAssistantRunInvalid
	}
	for i := 0; i < len(raw); i++ {
		if raw[i] != '"' {
			continue
		}
		start := i
		for i++; i < len(raw); i++ {
			if raw[i] == '\\' {
				i++
			} else if raw[i] == '"' {
				break
			}
		}
		if !validJSONString(raw[start : i+1]) {
			return nil, errAssistantRunInvalid
		}
	}
	d := json.NewDecoder(bytes.NewReader(raw))
	d.UseNumber()
	depth := 0
	for {
		token, err := d.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, errAssistantRunInvalid
		}
		if delim, ok := token.(json.Delim); ok {
			if delim == '{' || delim == '[' {
				depth++
			} else {
				depth--
			}
			if depth > 64 {
				return nil, errAssistantRunInvalid
			}
		}
		if number, ok := token.(json.Number); ok {
			if len(number) > 1024 {
				return nil, errAssistantRunInvalid
			}
			if at := strings.IndexAny(string(number), "eE"); at >= 0 {
				exponent, err := strconv.Atoi(string(number)[at+1:])
				if err != nil || exponent < -1000 || exponent > 1000 {
					return nil, errAssistantRunInvalid
				}
			}
			parsed, err := runcontext.ParseJSON([]byte(number))
			if err != nil {
				return nil, errAssistantRunInvalid
			}
			before, a := new(big.Rat).SetString(string(number))
			after, b := new(big.Rat).SetString(parsed.Text())
			if !a || !b || before.Cmp(after) != 0 {
				return nil, errAssistantRunInvalid
			}
		}
	}
	return runcontext.ParseJSON(raw)
}

func assistantChatString(v *runcontext.Value, key, fallback string) (string, error) {
	if !v.Has(key) {
		return fallback, nil
	}
	if !v.Get(key).IsString() {
		return "", errAssistantRunInvalid
	}
	return v.Get(key).Str(), nil
}

// Only omission retains the legacy Work default. Explicit malformed controls
// never select execution authority, whether read by the resolver or compiler.
func assistantChatPhase(raw json.RawMessage) (string, error) {
	if len(raw) == 0 {
		return "work", nil
	}
	var phase string
	if json.Unmarshal(raw, &phase) != nil || !contains([]string{"work", "plan"}, phase) {
		return "", errAssistantRunInvalid
	}
	return phase, nil
}

func compileAssistantChatInput(source assistantRunSource) (*assistantChatInput, error) {
	if len(source.Binding.SourceDigests) != 1 || digestOf(source.Original) != source.Binding.SourceDigests[0] || digestOf(source.Request) != source.Binding.RequestDigest {
		return nil, errAssistantRunScope
	}
	chat, err := assistantChatValue(source.Original)
	if err != nil || !chat.IsObject() || !chat.Get("id").IsString() || chat.Get("id").Str() != source.Start.OwnerID {
		return nil, errAssistantRunInvalid
	}
	var interaction json.RawMessage
	if chat.Has("interaction") {
		interaction = chat.Get("interaction").JSON()
	}
	phase, err := assistantChatPhase(interaction)
	if err != nil {
		return nil, err
	}
	if phase != source.Binding.Phase {
		return nil, errAssistantRunScope
	}
	if _, err := assistantPlanSource(source.Original); err != nil {
		return nil, err
	}
	checkpoint := chat.Get("checkpoint")
	if !checkpoint.IsObject() || !checkpoint.Get("state").IsObject() {
		return nil, errAssistantRunInvalid
	}
	state := checkpoint.Get("state")
	for _, field := range []string{"candidates", "cases", "expectationIssues", "responses"} {
		if state.Has(field) && !state.Get(field).IsArray() {
			return nil, errAssistantRunInvalid
		}
	}
	if state.Has("candidates") {
		for _, candidate := range state.Get("candidates").Items() {
			if !candidate.IsObject() || !candidate.Get("text").IsString() {
				return nil, errAssistantRunInvalid
			}
		}
	}
	if state.Has("cases") {
		for _, row := range state.Get("cases").Items() {
			if !row.IsObject() || !row.Get("id").IsString() || !assistantRunText(row.Get("id").Str(), 256) || !row.Get("facts").IsObject() {
				return nil, errAssistantRunInvalid
			}
		}
	}
	turns := state.Get("turns")
	if !turns.IsArray() {
		return nil, errAssistantRunInvalid
	}
	ids, roles := map[string]int{}, map[string]string{}
	latest := -1
	for i, t := range turns.Items() {
		if !t.IsObject() || !t.Get("role").IsString() || !t.Get("kind").IsString() || !t.Get("text").IsString() || !t.Get("at").IsString() ||
			!contains([]string{"user", "assistant"}, t.Get("role").Str()) || !contains([]string{"brief", "message", "unknowns", "note", "question"}, t.Get("kind").Str()) {
			return nil, errAssistantRunInvalid
		}
		id := "legacy-turn-" + strconv.Itoa(i)
		if t.Has("id") {
			if !t.Get("id").IsString() || !assistantChatID.MatchString(t.Get("id").Str()) {
				return nil, errAssistantRunInvalid
			}
			id = t.Get("id").Str()
		}
		if _, found := ids[id]; found {
			return nil, errAssistantRunInvalid
		}
		ids[id], roles[id] = i, t.Get("role").Str()
		for _, field := range []string{"statement", "input"} {
			if _, err := assistantChatString(t, field, ""); err != nil {
				return nil, err
			}
		}
		if t.Has("interrupted") && t.Get("interrupted").Kind() != runcontext.KindBool {
			return nil, errAssistantRunInvalid
		}
		if t.Get("role").Str() == "user" && t.Get("kind").Str() != "note" {
			latest = i
		}
	}
	if latest < 0 || !turns.Items()[latest].Get("id").IsString() || turns.Items()[latest].Get("id").Str() != source.Start.RequestID {
		return nil, errAssistantRunScope
	}
	current := turns.Items()[latest]
	heldRequest, err := assistantChatValue(source.Request)
	if err != nil || heldRequest.Text() != current.Text() || !contains([]string{"message", "brief"}, current.Get("kind").Str()) {
		return nil, errAssistantRunScope
	}
	message, err := assistantChatString(current, "statement", current.Get("text").Str())
	if err != nil {
		return nil, err
	}
	input, err := assistantChatString(current, "input", current.Get("text").Str())
	if err != nil {
		return nil, err
	}
	out := &assistantChatInput{Request: runcontext.CurrentRequest{Message: message, Input: input}, Thinking: source.Connection.Assistant.Thinking}
	out.Policy, err = readAssistantPolicyScope(source)
	if err != nil {
		return nil, err
	}
	questions := state.Get("questions")
	if questions == nil {
		questions = runcontext.Array()
	}
	prompts, err := assistantChatQuestions(questions, turns, ids, roles)
	if err != nil {
		return nil, err
	}
	out.Documents = []runcontext.Document{{ID: "conversation", Kind: "history", Label: "Original conversation messages and question resolutions; drafts and suggestions are not user decisions", Value: runcontext.ObjectOf("turns", turns.Clone(), "questions", questions.Clone())}}
	if plans := chat.Get("plans"); plans != nil && plans.Len() > 0 {
		out.Documents = append(out.Documents, runcontext.Document{ID: "chat-plan-history", Kind: "history", Label: "Saved Plan revisions and review questions; historical review scope, never approval or compute allowance", Value: plans.Clone()})
	}

	if out.Policy != nil {
		value, parseErr := runcontext.ParseJSON(out.Policy.contextJSON())
		if parseErr != nil {
			return nil, parseErr
		}
		out.Documents = append(out.Documents, runcontext.Document{ID: "policy-context", Kind: "history", Label: "Retained Policy draft basis, historical sources and proposals; reference data, never approval or publication", Value: value})
	}
	attachments, refs, groups, err := assistantChatAttachments(turns)
	if err != nil {
		return nil, err
	}
	out.Documents = append(out.Documents, attachments...)
	out.References, out.Attachments = refs, groups
	observations := runcontext.Object()
	for _, key := range state.Keys() {
		if key != "turns" && key != "questions" {
			observations.Set(key, state.Get(key).Clone())
		}
	}
	if checkpoint.Has("sources") {
		if !checkpoint.Get("sources").IsArray() {
			return nil, errAssistantRunInvalid
		}
		for _, row := range checkpoint.Get("sources").Items() {
			if !row.IsObject() {
				return nil, errAssistantRunInvalid
			}
			for _, field := range []string{"text", "failure"} {
				if v := row.Get(field); v != nil && !v.IsString() && v.Kind() != runcontext.KindNull {
					return nil, errAssistantRunInvalid
				}
			}
		}
		observations.Set("savedSources", checkpoint.Get("sources").Clone())
	}
	if observations.Len() != 0 {
		out.Documents = append(out.Documents, runcontext.Document{ID: "saved-chat-state", Kind: "history", Label: "Saved draft, typed cases and original source replies; historical observations, not current artifact authority or fresh verification", Value: observations})
		out.State = "Saved draft/check/source observations may predate the workspace and are retained in saved-chat-state. Read current saved artifacts through their owning tools before edits or checks. Historical question suggestions/drafts are not accepted answers; historical approvals or source replies grant no authority or fresh verification."
	}
	lines, omitted := []string{}, false
	for i, t := range turns.Items() {
		if i == latest || t.Get("kind").Str() == "note" {
			continue
		}
		if q := prompts[i]; q != nil {
			lines = append(lines, "ASSISTANT QUESTION (suggestions, not a user decision): "+q.Text())
			continue
		}
		text, prefix := t.Get("text").Str(), "ASSISTANT: "
		if t.Get("role").Str() == "user" {
			prefix = "PERSON: "
			if t.Has("statement") {
				text = t.Get("statement").Str()
				omitted = omitted || t.Has("input") && t.Get("input").Str() != text
			} else if t.Has("input") {
				text = t.Get("input").Str()
			}
		}
		lines = append(lines, prefix+text)
	}
	note := ""
	if omitted {
		note = "\nHistorical Desk workspace snapshots are omitted from this model context; authored messages and supplied material are retained. Original inputs remain saved in chat history. Use the current workspace context and tools to read current objects; ask if a past selection is needed.\n"
	}
	out.Transcript = "TRANSCRIPT SO FAR" + note + "\n" + strings.Join(lines, "\n\n")
	if value := chat.Get("adversarialReview"); value != nil {
		if value.Kind() != runcontext.KindBool {
			return nil, errAssistantRunInvalid
		}
		review := value.Truth()
		out.Review = &review
	}
	out.CriticProject = source.Profile.Critic
	if chat.Has("graph") || chat.Has("policyInterpretationKind") && chat.Get("policyInterpretationKind").Str() == "graph" {
		out.CriticPurpose = "graph"
	}
	for key, target := range map[string]*string{"critic": &out.Critic, "criticReasoning": &out.CriticReasoning} {
		if value := chat.Get(key); value != nil {
			if !value.IsString() || value.Str() == "" {
				return nil, errAssistantRunInvalid
			}
			*target = value.Str()
		}
	}
	if err := runcontext.ReadCriticSettings(runcontext.CriticSettings{Critic: out.Critic, CriticReasoning: out.CriticReasoning, AdversarialReview: out.Review}, source.Connection.Assistant.Engine == "codex"); err != nil {
		return nil, errAssistantRunInvalid
	}
	prefs := assistantChatPreferences(source)
	if source.Connection.Assistant.Engine != "codex" && prefs != nil && !prefs.Inherit && prefs.Thinking != "" {
		out.Thinking = prefs.Thinking
	}
	if value := chat.Get("apiThinking"); value != nil {
		if !value.IsString() || !contains(AssistantThinkingTiers, value.Str()) {
			return nil, errAssistantRunInvalid
		}
		out.Thinking = value.Str()
	}
	if value := chat.Get("reasoning"); value != nil {
		if !value.IsObject() || len(value.Keys()) != 2 || !value.Get("model").IsString() || !assistantRunText(value.Get("model").Str(), 128) || !value.Has("effort") {
			return nil, errAssistantRunInvalid
		}
		effort := value.Get("effort")
		if effort.Kind() != runcontext.KindNull && (!effort.IsString() || !contains([]string{"none", "minimal", "low", "medium", "high", "xhigh", "max"}, effort.Str())) {
			return nil, errAssistantRunInvalid
		}
		out.Reasoning = value.Clone()
	}
	return out, nil
}

func assistantChatPreferences(source assistantRunSource) *modelPreferences {
	if source.Profile.Version == 2 {
		return source.Profile.Models[source.Connection.ID]
	}
	if source.Connection.ID == "legacy-codex" {
		return source.Profile.Codex
	}
	if source.Connection.ID == "legacy-api" {
		return source.Profile.API
	}
	return nil
}

func (e *assistantRunExecution) assistantChatMemory(ctx context.Context) (*bool, error) {
	s := e.service.server
	lock, err := s.privateDataLockContext(ctx, true)
	if err != nil {
		return nil, err
	}
	defer lock.Close()
	store, err := s.openChatData()
	if err != nil {
		return nil, err
	}
	defer store.root.Close()
	name, err := resolveConversationName(store.root, s.conversationIdentity())
	if err != nil {
		return nil, err
	}
	memory, err := readMemory(store.root, strings.Replace(name, "conversations-", "ai-memory-", 1))
	if err != nil {
		return nil, err
	}
	return &memory.Content.Settings.Use, nil
}

func assistantChatQuestions(questions, turns *runcontext.Value, ids map[string]int, roles map[string]string) (map[int]*runcontext.Value, error) {
	if !questions.IsArray() || questions.Len() > 128 {
		return nil, errAssistantRunInvalid
	}
	prompts, seen, unresolved := map[int]*runcontext.Value{}, map[string]bool{}, 0
	for _, value := range questions.Items() {
		var saved contract.QuestionSaved
		if !value.IsObject() || json.Unmarshal(value.JSON(), &saved) != nil || !assistantChatID.MatchString(saved.ID) || seen[saved.ID] {
			return nil, errAssistantRunInvalid
		}
		i, found := ids[saved.ID]
		if !found || roles[saved.ID] != "assistant" || turns.Items()[i].Get("kind").Str() != "question" {
			return nil, errAssistantRunInvalid
		}
		if !value.Get("prompt").IsObject() || !value.Get("draft").IsObject() || !value.Get("draft").Get("selected").IsArray() || !value.Get("draft").Get("text").IsString() {
			return nil, errAssistantRunInvalid
		}
		if _, err := turn.ReadUserQuestion(value.Get("prompt").JSON()); err != nil || saved.Draft.Selected == nil || runcontext.U16Length(saved.Draft.Text) > 4000 {
			return nil, errAssistantRunInvalid
		}
		selected := map[string]bool{}
		for _, id := range saved.Draft.Selected {
			found := false
			for _, option := range saved.Prompt.Options {
				found = found || option.ID == id
			}
			if !found || selected[id] {
				return nil, errAssistantRunInvalid
			}
			selected[id] = true
		}
		if saved.Prompt.Selection == "single" && len(selected) > 1 {
			return nil, errAssistantRunInvalid
		}
		if saved.Resolution == nil {
			if value.Has("resolution") {
				return nil, errAssistantRunInvalid
			}
			unresolved++
		} else if !contains([]string{"answered", "help", "superseded"}, saved.Resolution.Kind) || saved.Resolution.At == "" || roles[saved.Resolution.TurnID] != "user" {
			return nil, errAssistantRunInvalid
		}
		if saved.Resolution != nil && value.Get("resolution").Has("continued") && value.Get("resolution").Get("continued").Kind() != runcontext.KindBool {
			return nil, errAssistantRunInvalid
		}
		seen[saved.ID], prompts[i] = true, value.Get("prompt")
	}
	for id, i := range ids {
		if turns.Items()[i].Get("kind").Str() == "question" && !seen[id] {
			return nil, errAssistantRunInvalid
		}
	}
	if unresolved > 1 {
		return nil, errAssistantRunInvalid
	}
	return prompts, nil
}

func assistantChatAttachments(turns *runcontext.Value) ([]runcontext.Document, []assistantDocumentReference, []assistantChatAttachmentGroup, error) {
	documents, held := []runcontext.Document{}, map[string]string{}
	references, groups := []assistantDocumentReference{}, []assistantChatAttachmentGroup{}
	repeated := map[string]bool{}
	for _, t := range turns.Items() {
		files := t.Get("attachments")
		if files == nil {
			continue
		}
		if t.Get("role").Str() != "user" || !files.IsArray() || files.Len() > 4 {
			return nil, nil, nil, errAssistantRunInvalid
		}
		seen, ledgerBytes := map[string]bool{}, 0
		group := assistantChatAttachmentGroup{}
		for _, file := range files.Items() {
			if !file.IsObject() || !file.Get("id").IsString() || !assistantChatID.MatchString(file.Get("id").Str()) || !file.Get("name").IsString() || !file.Get("text").IsString() || runcontext.U16Length(file.Get("text").Str()) > 200000 {
				return nil, nil, nil, errAssistantRunInvalid
			}
			id, name := file.Get("id").Str(), file.Get("name").Str()
			if seen[id] || name == "" || len(name) > 255 || strings.ContainsAny(name, "\x00\r\n\x7f") {
				return nil, nil, nil, errAssistantRunInvalid
			}
			for _, char := range name {
				if char < 32 {
					return nil, nil, nil, errAssistantRunInvalid
				}
			}
			seen[id] = true
			if file.Has("document") {
				value := file.Get("document")
				if !value.IsObject() || !value.Get("id").IsString() || !value.Get("digest").IsString() || !value.Get("pages").IsArray() || !value.Has("allowPartial") || value.Get("allowPartial").Kind() != runcontext.KindBool || file.Get("text").Str() != "" {
					return nil, nil, nil, errAssistantRunInvalid
				}
				for _, key := range value.Keys() {
					if !contains([]string{"id", "digest", "pages", "allowPartial", "needsReview"}, key) {
						return nil, nil, nil, errAssistantRunInvalid
					}
				}
				if value.Has("needsReview") && value.Get("needsReview").Kind() != runcontext.KindBool {
					return nil, nil, nil, errAssistantRunInvalid
				}
				var ref assistantDocumentReference
				if json.Unmarshal(value.JSON(), &ref) != nil || ref.ID != id || !assistantDocumentReferenceValid(ref) {
					return nil, nil, nil, errAssistantRunInvalid
				}
				key := assistantChatReferenceKey(ref)
				if !repeated[key] {
					references = append(references, ref)
					repeated[key] = true
				}
				group.References = append(group.References, ref)
				continue
			}
			if file.Has("link") {
				return nil, nil, nil, errAssistantRunInvalid
			}
			value := runcontext.ObjectOf("pages", runcontext.Array(runcontext.String(file.Get("text").Str())))
			ledgerBytes += len(value.JSON())
			if ledgerBytes > 800000 {
				return nil, nil, nil, errAssistantRunInvalid
			}
			if before, exists := held[id]; exists {
				if before != name+"\x00"+value.Text() {
					return nil, nil, nil, errAssistantRunInvalid
				}
				continue
			}
			held[id] = name + "\x00" + value.Text()
			documents = append(documents, runcontext.Document{ID: "attachment-" + id, Kind: "artifact", Label: "Attached file: " + name, Value: value})
		}
		group.Bytes = ledgerBytes
		groups = append(groups, group)
	}
	return documents, references, groups, nil
}
