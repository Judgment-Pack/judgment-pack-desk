package desk

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"reflect"
	"regexp"
	"strings"
	"time"

	runcontext "github.com/Judgment-Pack/judgment-pack-agent/go/context"
)

var assistantSourceFileName = regexp.MustCompile(`^assistant-source-approvals-[a-f0-9]{64}\.json$`)

var assistantSourceRecordID = regexp.MustCompile(`^[a-f0-9]{32}$`)

type assistantSourceScope struct {
	Project      string `json:"project"`
	ChatID       string `json:"chatId"`
	SessionScope string `json:"sessionScope"`
	Principal    string `json:"principal"`
	PolicyDigest string `json:"policyDigest"`
}
type assistantSourceReview struct {
	Historical bool                    `json:"historical,omitempty"`
	Version    int                     `json:"version"`
	ID         string                  `json:"id"`
	Revision   uint64                  `json:"revision"`
	Scope      assistantSourceScope    `json:"scope"`
	Snapshot   assistantSourceSnapshot `json:"snapshot"`
	Consumed   bool                    `json:"consumed"`
}
type assistantSourceApproval struct {
	Historical     bool                    `json:"historical,omitempty"`
	Version        int                     `json:"version"`
	ID             string                  `json:"id"`
	Revision       uint64                  `json:"revision"`
	ReviewID       string                  `json:"reviewId"`
	ReviewRevision uint64                  `json:"reviewRevision"`
	Scope          assistantSourceScope    `json:"scope"`
	Snapshot       assistantSourceSnapshot `json:"snapshot"`
	Reviewer       string                  `json:"reviewer"`
	ApprovedAt     string                  `json:"approvedAt"`
	RevokedAt      string                  `json:"revokedAt,omitempty"`
}
type assistantSourceLedger struct {
	Version   int                       `json:"version"`
	Project   string                    `json:"project"`
	Reviews   []assistantSourceReview   `json:"reviews"`
	Approvals []assistantSourceApproval `json:"approvals"`
}

func assistantSourceID() (string, error) {
	var raw [16]byte
	if _, err := rand.Read(raw[:]); err != nil {
		return "", err
	}
	return hex.EncodeToString(raw[:]), nil
}
func assistantSourceScopeValid(scope assistantSourceScope, project string) bool {
	return scope.Project == project && assistantRunDigest(project) && researchChatID.MatchString(scope.ChatID) && assistantRunDigest(scope.SessionScope) && assistantRunDigest(scope.Principal) && assistantRunDigest(scope.PolicyDigest)
}
func assistantSourceConfigurationDigest(snapshot assistantSourceSnapshot, project string) string {
	return digestOf(assistantChatJSON(struct {
		Version, Project, Adapter, List string
		Configuration                   assistantSourceConfiguration
	}{snapshot.AdapterVersion, project, snapshot.AdapterID, snapshot.ToolListDigest, snapshot.Configuration}))
}
func validateAssistantSourceSnapshot(snapshot assistantSourceSnapshot, project string) error {
	if snapshot.AdapterVersion != "1" || snapshot.Kind != "custom" || snapshot.Trust != "first-party" || snapshot.Status != "review-required" || len(snapshot.CompleteDeclarations) != 1 {
		return errAssistantRunInvalid
	}
	var expected *assistantSourceSnapshot
	for _, row := range assistantSourceBaseCatalog() {
		if row.AdapterID == snapshot.AdapterID {
			held := row
			expected = &held
		}
	}
	if expected == nil {
		return errAssistantRunInvalid
	}
	declaration := snapshot.CompleteDeclarations[0]
	if declaration.Name != expected.CompleteDeclarations[0].Name || !reflect.DeepEqual(declaration.Taxonomy, expected.CompleteDeclarations[0].Taxonomy) || declaration.EffectClass != expected.CompleteDeclarations[0].EffectClass || declaration.Approval != expected.CompleteDeclarations[0].Approval || declaration.Delegation != expected.CompleteDeclarations[0].Delegation || !assistantRunText(declaration.Description, assistantRunRecordLimit) {
		return errAssistantRunInvalid
	}
	schema, err := runcontext.ParseJSON(declaration.InputSchema)
	if err != nil || !schema.IsObject() || !uniqueJSON(declaration.InputSchema) {
		return errAssistantRunInvalid
	}
	taxonomy := &runcontext.ToolTaxonomy{Operation: declaration.Taxonomy.Operation, Effect: declaration.Taxonomy.Effect}
	if snapshot.ToolListDigest != runcontext.ToolListDigest([]runcontext.ListedTool{{Name: declaration.Name, Description: declaration.Description, InputSchema: declaration.InputSchema, Taxonomy: taxonomy}}) {
		return errAssistantRunInvalid
	}
	if !assistantRunDigest(snapshot.ConfigurationDigest) || snapshot.ConfigurationDigest != assistantSourceConfigurationDigest(snapshot, project) || snapshot.Identity == nil || snapshot.Identity.Kind != "connection" || snapshot.IdentityDigest != runcontext.IdentityDigest(*snapshot.Identity) {
		return errAssistantRunInvalid
	}
	label := "Desk research HTTP "
	if snapshot.AdapterID == "desk-research-workflow" {
		label = "Desk research workflow "
	}
	if snapshot.Identity.Label != label+snapshot.ConfigurationDigest {
		return errAssistantRunInvalid
	}
	c := snapshot.Configuration
	if c.CatalogDigest != "" || c.CatalogOwnerDigest != "" {
		if snapshot.AdapterID != "desk-website" || !assistantRunDigest(c.CatalogDigest) || !assistantRunDigest(c.CatalogOwnerDigest) || !assistantRunDigest(c.BundleDigest) || !assistantRunDigest(c.GatewayBuildDigest) {
			return errAssistantRunInvalid
		}
	}
	if !assistantRunDigest(c.DeskDigest) || !assistantRunDigest(c.OwnerDigest) {
		return errAssistantRunInvalid
	}
	for _, d := range []string{c.TargetDigest, c.AuthorityDigest, c.PublicKeyDigest, c.BundleDigest, c.VerifierDigest, c.GatewayBuildDigest, c.CatalogDigest, c.CatalogOwnerDigest} {
		if d != "" && !assistantRunDigest(d) {
			return errAssistantRunInvalid
		}
	}
	if snapshot.AdapterID == "desk-research-workflow" {
		if c.Target != "Desk private research workflow" || c.TargetDigest != "" || c.AuthorityDigest != "" || c.PublicKeyDigest != "" || c.Connection != "" || c.Revision != "" || c.Provider != "" || c.BundleDigest != "" || c.VerifierDigest != "" || c.GatewayBuildDigest != "" || c.CatalogDigest != "" || c.CatalogOwnerDigest != "" || c.PreferenceDigest != "" {
			return errAssistantRunInvalid
		}
	} else {
		if !assistantRunDigest(c.TargetDigest) || !assistantRunDigest(c.AuthorityDigest) || !assistantRunDigest(c.PublicKeyDigest) || !assistantRunText(c.Target, 4096) {
			return errAssistantRunInvalid
		}
		if snapshot.AdapterID == "desk-search" {
			if !researchSourceName.MatchString(c.Connection) || !researchConnectionRevision.MatchString(c.Revision) || !researchSourceName.MatchString(c.Provider) || c.PreferenceDigest != "absent" && !assistantRunDigest(c.PreferenceDigest) {
				return errAssistantRunInvalid
			}
		} else if c.Connection != "" || c.Revision != "" || c.Provider != "" || c.PreferenceDigest != "" {
			return errAssistantRunInvalid
		}
	}
	return nil
}
func decodeAssistantSourceLedger(raw []byte, project string) (assistantSourceLedger, error) {
	var ledger assistantSourceLedger
	if len(raw) > assistantRunRecordLimit || !validUTF8(raw) || !uniqueJSON(raw) || readAssistantDialectCustody(raw, reflect.TypeOf(ledger)) != nil || decodeDataJSON(raw, &ledger) != nil {
		return ledger, errAssistantRunInvalid
	}
	if err := validateAssistantSourceLedger(ledger, project); err != nil {
		return assistantSourceLedger{}, err
	}
	return ledger, nil
}
func validateAssistantSourceLedger(ledger assistantSourceLedger, project string) error {
	if ledger.Version != 1 || ledger.Project != project || !assistantRunDigest(project) || ledger.Reviews == nil || ledger.Approvals == nil || len(ledger.Reviews) > assistantRunCountLimit || len(ledger.Approvals) > assistantRunCountLimit {
		return errAssistantRunInvalid
	}
	reviews := map[string]assistantSourceReview{}
	for _, review := range ledger.Reviews {
		if review.Version != 1 || !assistantSourceRecordID.MatchString(review.ID) || review.Revision != 1 || !assistantSourceScopeValid(review.Scope, project) || validateAssistantSourceSnapshot(review.Snapshot, project) != nil {
			return errAssistantRunInvalid
		}
		if _, exists := reviews[review.ID]; exists {
			return errAssistantRunInvalid
		}
		reviews[review.ID] = review
	}
	ids := map[string]bool{}
	approvedReviews := map[string]bool{}
	active := map[string]bool{}
	for _, approval := range ledger.Approvals {
		if approval.Version != 1 || !assistantSourceRecordID.MatchString(approval.ID) || ids[approval.ID] || approval.Revision < 1 || approval.Revision > 2 || !assistantSourceScopeValid(approval.Scope, project) || approval.Reviewer != approval.Scope.Principal || validateAssistantSourceSnapshot(approval.Snapshot, project) != nil {
			return errAssistantRunInvalid
		}
		ids[approval.ID] = true
		review, ok := reviews[approval.ReviewID]
		if approvedReviews[approval.ReviewID] {
			return errAssistantRunInvalid
		}
		approvedReviews[approval.ReviewID] = true
		if !ok || !review.Consumed || review.Historical != approval.Historical || approval.ReviewRevision != review.Revision || review.Scope != approval.Scope || !assistantSourceSnapshotEqual(review.Snapshot, approval.Snapshot) {
			return errAssistantRunInvalid
		}
		at, err := time.Parse(time.RFC3339Nano, approval.ApprovedAt)
		if err != nil {
			return errAssistantRunInvalid
		}
		if approval.RevokedAt != "" {
			revoked, err := time.Parse(time.RFC3339Nano, approval.RevokedAt)
			if err != nil || revoked.Before(at) || approval.Revision != 2 {
				return errAssistantRunInvalid
			}
		} else {
			if approval.Revision != 1 {
				return errAssistantRunInvalid
			}
			key := string(assistantChatJSON(approval.Scope)) + "\n" + approval.Snapshot.AdapterID
			if !approval.Historical && active[key] {
				return errAssistantRunInvalid
			}
			if !approval.Historical {
				active[key] = true
			}
		}
	}
	return nil
}
func assistantSourceLedgerName(project string) string {
	return "assistant-source-approvals-" + project + ".json"
}
func readAssistantSourceLedger(root *os.Root, project string) (assistantSourceLedger, error) {
	raw, err := readPrivateData(root, assistantSourceLedgerName(project), assistantRunRecordLimit)
	if errors.Is(err, os.ErrNotExist) {
		return assistantSourceLedger{Version: 1, Project: project, Reviews: []assistantSourceReview{}, Approvals: []assistantSourceApproval{}}, nil
	}
	if err != nil {
		return assistantSourceLedger{}, err
	}
	return decodeAssistantSourceLedger(raw, project)
}

// Writer injection is private test plumbing; HTTP always passes writePrivateData.
// Re-read the persisted bytes as well as relying on atomic helper read-back.
func writeAssistantSourceLedger(ctx context.Context, store *chatDataStore, ledger assistantSourceLedger, write func(*os.Root, string, []byte) error) error {
	raw, err := json.Marshal(ledger)
	if err != nil {
		return err
	}
	if len(raw) > assistantRunRecordLimit {
		return withCode(CodeTooLarge, errAssistantRunInvalid)
	}
	if _, err = decodeAssistantSourceLedger(raw, ledger.Project); err != nil {
		return err
	}
	if err = ctx.Err(); err != nil {
		return err
	}
	if err = checkPrivateDataPath(store); err != nil {
		return err
	}
	name := assistantSourceLedgerName(ledger.Project)
	if err = write(store.root, name, raw); err != nil {
		return err
	}
	saved, err := readPrivateData(store.root, name, assistantRunRecordLimit)
	if err != nil || !bytes.Equal(saved, raw) {
		return errAssistantRunConflict
	}
	if _, err = decodeAssistantSourceLedger(saved, ledger.Project); err != nil {
		return err
	}
	return ctx.Err()
}

// No model/profile resolver or run admission is involved in source review.
func (s *Server) assistantSourceScopeCurrent(r *http.Request, chatID string) (assistantSourceScope, assistantRunSource, error) {
	var scope assistantSourceScope
	identity, _, err := s.assistantRunIdentity(r)
	if err != nil {
		return scope, assistantRunSource{}, err
	}
	store, err := s.openChatData()
	if err != nil {
		return scope, assistantRunSource{}, err
	}
	defer store.root.Close()
	project, err := assistantRunNamespace(store.root, s.conversationIdentity())
	if err != nil {
		return scope, assistantRunSource{}, err
	}
	name, err := resolveConversationName(store.root, s.conversationIdentity())
	if err != nil {
		return scope, assistantRunSource{}, err
	}
	raw, err := readPrivateData(store.root, name, maxConversationBytes)
	if err != nil || validateConversations(raw) != nil {
		return scope, assistantRunSource{}, errAssistantRunScope
	}
	var document conversationDocument
	if json.Unmarshal(raw, &document) != nil {
		return scope, assistantRunSource{}, errAssistantRunInvalid
	}
	var selected json.RawMessage
	for _, chat := range document.Chats {
		var id struct {
			ID string `json:"id"`
		}
		if json.Unmarshal(chat, &id) != nil {
			return scope, assistantRunSource{}, errAssistantRunInvalid
		}
		if id.ID == chatID {
			if selected != nil {
				return scope, assistantRunSource{}, errAssistantRunInvalid
			}
			selected = chat
		}
	}
	if selected == nil {
		return scope, assistantRunSource{}, errAssistantRunScope
	}
	if err = s.authorizePolicyConversations(r, []byte(`{"version":1,"chats":[`+string(selected)+`]}`)); err != nil {
		return scope, assistantRunSource{}, err
	}
	// Domain separated from run/action session handles; no bearer is retained.
	sessionScope := digestOf([]byte(s.sessions.handle("desk-source-approval/v1\n" + bearerOf(r))))
	scope = assistantSourceScope{Project: project, ChatID: chatID, SessionScope: sessionScope, Principal: identity.Principal, PolicyDigest: identity.Policy}
	identity.Project = project
	return scope, assistantRunSource{Binding: identity, Original: append([]byte{}, selected...)}, nil
}

// Eligibility is only an exact still-unrevoked local approval match. E6 must
// independently re-observe the current owner before dispatch and evidence return.
func assistantSourceApprovalCurrent(ledger assistantSourceLedger, scope assistantSourceScope, snapshot assistantSourceSnapshot) *assistantSourceApproval {
	for i := len(ledger.Approvals) - 1; i >= 0; i-- {
		a := ledger.Approvals[i]
		if !a.Historical && a.Scope == scope && a.RevokedAt == "" && assistantSourceSnapshotEqual(a.Snapshot, snapshot) {
			return &a
		}
	}
	return nil
}

// Observe without creating reviews or approvals. Companion observation never
// holds the private lock; scope and local configuration are rechecked before
// selecting an exact persisted approval from the current authoritative store.
func (e *assistantRunExecution) assistantSourceApprovals(ctx context.Context) (map[string]assistantSourceApproval, error) {
	if err := e.Authorize(ctx); err != nil {
		return nil, err
	}
	s := e.service.server
	lock, err := s.privateDataLockContext(ctx, true)
	if err != nil {
		return nil, err
	}
	scope, source, err := s.assistantSourceScopeCurrent(e.request.WithContext(ctx), e.record.Binding.Owner.ID)
	lock.Close()
	if err != nil {
		return nil, err
	}
	if scope.Project != e.record.Binding.Project || scope.Principal != e.record.Binding.Principal || scope.PolicyDigest != e.record.Binding.Policy {
		return nil, errAssistantRunScope
	}
	catalog, err := s.assistantSourceCatalog(ctx, source, scope.Project)
	if err != nil {
		return nil, err
	}
	lock, err = s.privateDataLockContext(ctx, true)
	if err != nil {
		return nil, err
	}
	defer lock.Close()
	freshScope, fresh, err := s.assistantSourceScopeCurrent(e.request.WithContext(ctx), scope.ChatID)
	if err != nil || freshScope != scope {
		return nil, errAssistantRunScope
	}
	if err = s.assistantSourceCatalogLocalCurrent(fresh, catalog); err != nil {
		return nil, err
	}
	store, err := s.openChatData()
	if err != nil {
		return nil, err
	}
	defer store.root.Close()
	ledger, err := readAssistantSourceLedger(store.root, scope.Project)
	if err != nil {
		return nil, err
	}
	approved := map[string]assistantSourceApproval{}
	for _, snapshot := range catalog {
		if approval := assistantSourceApprovalCurrent(ledger, scope, snapshot); approval != nil {
			approved[snapshot.AdapterID] = *approval
		}
	}
	return approved, ctx.Err()
}

func (e *assistantRunExecution) assistantSourceApprovalStillCurrent(ctx context.Context, held assistantSourceApproval) error {
	approved, err := e.assistantSourceApprovals(ctx)
	if err != nil {
		return err
	}
	current, ok := approved[held.Snapshot.AdapterID]
	if !ok || !reflect.DeepEqual(current, held) {
		return errAssistantRunScope
	}
	return nil
}

// A file is recognized only by its closed name and matching internal project.
func decodeAssistantSourceStorage(name string, raw []byte) (assistantSourceLedger, error) {
	if !assistantSourceFileName.MatchString(name) {
		return assistantSourceLedger{}, errAssistantRunInvalid
	}
	project := strings.TrimSuffix(strings.TrimPrefix(name, "assistant-source-approvals-"), ".json")
	return decodeAssistantSourceLedger(raw, project)
}

func (s *Server) authorizeAssistantSourceStorage(r *http.Request) error {
	if err := s.authorizePrivateWorkspace(r); err != nil {
		return err
	}
	return s.authorizePolicyHistory(r)
}

// Add only host historical status. Retain each original scope, snapshot and
// other member value byte-for-byte, including schema formatting. Imported
// pending reviews are historical too: importing can never grant approval.
func historicalAssistantSourceStorage(name string, raw []byte) ([]byte, error) {
	ledger, err := decodeAssistantSourceStorage(name, raw)
	if err != nil {
		return nil, err
	}
	all := true
	for _, review := range ledger.Reviews {
		all = all && review.Historical
	}
	for _, approval := range ledger.Approvals {
		all = all && approval.Historical
	}
	if all {
		return raw, nil
	}
	members, duplicate, err := topLevelMembers(raw)
	if err != nil || duplicate != "" {
		return nil, errAssistantRunInvalid
	}
	var out bytes.Buffer
	out.WriteByte('{')
	for i, member := range members {
		if i > 0 {
			out.WriteByte(',')
		}
		key, _ := json.Marshal(member.name)
		out.Write(key)
		out.WriteByte(':')
		if member.name != "reviews" && member.name != "approvals" {
			out.Write(member.raw)
			continue
		}
		var records []json.RawMessage
		if json.Unmarshal(member.raw, &records) != nil {
			return nil, errAssistantRunInvalid
		}
		out.WriteByte('[')
		for j, record := range records {
			if j > 0 {
				out.WriteByte(',')
			}
			fields, duplicate, err := topLevelMembers(record)
			if err != nil || duplicate != "" {
				return nil, errAssistantRunInvalid
			}
			out.WriteByte('{')
			count := 0
			for _, field := range fields {
				if field.name == "historical" {
					continue
				}
				if count > 0 {
					out.WriteByte(',')
				}
				count++
				key, _ := json.Marshal(field.name)
				out.Write(key)
				out.WriteByte(':')
				out.Write(field.raw)
			}
			if count > 0 {
				out.WriteByte(',')
			}
			out.WriteString(`"historical":true}`)
		}
		out.WriteByte(']')
	}
	out.WriteByte('}')
	updated := out.Bytes()
	if _, err := decodeAssistantSourceStorage(name, updated); err != nil {
		return nil, err
	}
	return updated, nil
}

// Transfer completion cannot precede an exact readback of historical status.
// E6 must reread persisted status/revision and exact current owner identity;
// an old cached approval is never sufficient for dispatch or evidence return.
func readbackAssistantSourceStorage(root *os.Root, name string, expected []byte) error {
	saved, err := readPrivateData(root, name, assistantRunRecordLimit)
	if err != nil {
		return err
	}
	if !bytes.Equal(saved, expected) {
		return errAssistantRunConflict
	}
	_, err = decodeAssistantSourceStorage(name, saved)
	return err
}

// Caller holds private storage ownership. Historicalize before a binding write;
// if the binding then fails, conservatively historical records remain safe.
func historicalizeAssistantSourceHistory(root *os.Root, conversation string) error {
	name := strings.Replace(conversation, "conversations-", "assistant-source-approvals-", 1)
	raw, err := readPrivateData(root, name, assistantRunRecordLimit)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	updated, err := historicalAssistantSourceStorage(name, raw)
	if err != nil {
		return err
	}
	if !bytes.Equal(updated, raw) {
		if err = writePrivateData(root, name, updated); err != nil {
			return err
		}
	}
	return readbackAssistantSourceStorage(root, name, updated)
}
