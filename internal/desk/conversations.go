package desk

// Conversations are private UI checkpoints, not runtime inputs or authority.
// They use a separate owner-only data root, with explicit legacy migration.
// The server chooses the name from its pinned project identity.
import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"reflect"
	"regexp"
	"strings"
)

const maxConversationBytes = 16 << 20

type conversationDocument struct {
	Version int               `json:"version"`
	Chats   []json.RawMessage `json:"chats"`
}
type conversationReply struct {
	ChatRevisions map[string]string `json:"chatRevisions,omitempty"`
	Project       string            `json:"project"`
	SHA256        string            `json:"sha256"`
	Content       json.RawMessage   `json:"content"`
	Snapshot      json.RawMessage   `json:"snapshot,omitempty"`
	SnapshotIssue string            `json:"snapshotIssue,omitempty"`
}

func (s *Server) conversationName() string {
	return "conversations-" + digestOf([]byte(s.conversationIdentity())) + ".json"
}
func validateConversations(data []byte) error {
	if !validUTF8(data) || !json.Valid(data) {
		return errors.New("chat history must be one UTF-8 JSON document")
	}
	var doc conversationDocument
	if err := json.Unmarshal(data, &doc); err != nil || doc.Version != 1 || doc.Chats == nil || len(doc.Chats) > 256 {
		return errors.New("chat history must have version 1 and at most 256 chats")
	}
	for _, chat := range doc.Chats {
		if err := validateConversationRunReports(chat); err != nil {
			return err
		}
		if err := validateChatPlan(chat); err != nil {
			return err
		}
		if err := validateConversationGraphSnapshots(chat); err != nil {
			return err
		}
	}
	_, err := policyWorkspaceEntries(data, false)
	return err
}

var draftPackFileName = regexp.MustCompile(`^draft-packs-[a-f0-9]{64}\.json$`)

var briefFileName = regexp.MustCompile(`^briefs-[a-f0-9]{64}\.json$`)

var packTestsFileName = regexp.MustCompile(`^pack-tests-[a-f0-9]{64}\.json$`)

func validatePackTests(data []byte) error {
	if !validUTF8(data) || !json.Valid(data) {
		return errors.New("tests must be one UTF-8 JSON document")
	}
	var doc struct {
		Version int                        `json:"version"`
		Suites  map[string]json.RawMessage `json:"suites"`
	}
	if err := json.Unmarshal(data, &doc); err != nil || doc.Version != 1 || doc.Suites == nil || len(doc.Suites) > 512 {
		return errors.New("test storage must have version 1 and at most 512 suites")
	}
	return nil
}
func (s *Server) handlePackTests(w http.ResponseWriter, r *http.Request) {
	s.handleWorkspaceRecord(w, r, false)
}

func draftPackName(conversation string) string {
	return strings.Replace(conversation, "conversations-", "draft-packs-", 1)
}
func validateDraftPacks(data []byte) error {
	if !validUTF8(data) || !json.Valid(data) {
		return errors.New("draft packs must be one UTF-8 JSON document")
	}
	var doc struct {
		Version int               `json:"version"`
		Drafts  []json.RawMessage `json:"drafts"`
		Deleted []string          `json:"deleted"`
	}
	if err := json.Unmarshal(data, &doc); err != nil || doc.Version != 1 || doc.Drafts == nil || len(doc.Drafts) > 256 || len(doc.Deleted) > 4096 {
		return errors.New("draft packs must have version 1 and at most 256 drafts")
	}
	_, err := policyWorkspaceEntries(data, true)
	return err
}
func (s *Server) readConversationFile(root *os.Root, name string) (conversationReply, error) {
	return s.readWorkspaceRecord(root, name, false)
}

// prepareConversationDraftEnrollment enrolls the dialect drafts' storage
// where it is still needed and the saved conversation record, or the one being
// saved, asks for a draft. It reads the saved record in its own short shared
// transaction. Only the enrollment's own failure is answered here: a body that
// does not parse, or a record or inspection that fails, is left to the
// transaction, which reports it as it always has.
func (s *Server) prepareConversationDraftEnrollment(ctx context.Context, body []byte) error {
	if needed, err := s.dialectDraftEnrollmentNeeded(ctx); err != nil || !needed {
		return nil
	}
	if requests, err := conversationDialectDraftRequests(body); err == nil && len(requests) > 0 {
		return s.ensureDialectDraftEnrollment(ctx)
	}
	saved, err := func() ([]byte, error) {
		lock, err := s.privateDataLockContext(ctx, false)
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
		raw, err := readPrivateData(store.root, name, maxConversationBytes)
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}
		return raw, err
	}()
	if err != nil {
		return nil
	}
	if requests, err := conversationDialectDraftRequests(saved); err != nil || len(requests) == 0 {
		return nil
	}
	return s.ensureDialectDraftEnrollment(ctx)
}

func (s *Server) readWorkspaceRecord(root *os.Root, name string, drafts bool) (conversationReply, error) {
	reply := conversationReply{Project: s.projectDir, SHA256: "absent", Content: json.RawMessage(`{"version":1,"chats":[]}`)}
	if graphDraftFileName.MatchString(name) {
		reply.Content = json.RawMessage(`{"version":1,"drafts":[],"deleted":[]}`)
	} else if briefFileName.MatchString(name) {
		reply.Content = json.RawMessage(`{"version":1,"subjects":{}}`)
	} else if sourceReviewsFileName.MatchString(name) {
		reply.Content = json.RawMessage(`{"version":1,"reviews":[]}`)
	} else if packTestsFileName.MatchString(name) {
		reply.Content = json.RawMessage(`{"version":1,"suites":{}}`)
	} else if drafts {
		reply.Content = json.RawMessage(`{"version":1,"drafts":[],"deleted":[]}`)
	}
	info, err := root.Lstat(name)
	if errors.Is(err, os.ErrNotExist) {
		return reply, nil
	}
	if err != nil {
		return reply, err
	}
	if err = ownerOnlyFile(name, info.Mode()); err != nil {
		return reply, withCode(CodeForbidden, err)
	}
	if err = ownedByUs(name, info); err != nil {
		return reply, withCode(CodeForbidden, err)
	}
	file, err := root.OpenFile(name, os.O_RDONLY|openNoFollow|openNonBlocking, 0)
	if err != nil {
		return reply, err
	}
	defer file.Close()
	opened, err := file.Stat()
	if err != nil {
		return reply, err
	}
	if !os.SameFile(info, opened) {
		return reply, withCode(CodeForbidden, errors.New("chat history changed while being opened"))
	}
	if err = ownerOnlyFile(name, opened.Mode()); err != nil {
		return reply, withCode(CodeForbidden, err)
	}
	if err = ownedByUs(name, opened); err != nil {
		return reply, withCode(CodeForbidden, err)
	}
	data, err := readBounded(file, maxConversationBytes)
	if err != nil {
		return reply, withCode(CodeTooLarge, errors.New("chat history exceeds its 16 MiB limit"))
	}
	validate := validateConversations
	if graphDraftFileName.MatchString(name) {
		validate = validateGraphDrafts
	} else if briefFileName.MatchString(name) {
		validate = validateBriefs
	} else if sourceReviewsFileName.MatchString(name) {
		validate = validateSourceReviews
	} else if packTestsFileName.MatchString(name) {
		validate = validatePackTests
	} else if drafts {
		validate = validateDraftPacks
	}
	if err = validate(data); err != nil {
		return reply, withCode(CodeBadRequest, err)
	}
	reply.SHA256 = digestOf(data)
	reply.Content = data
	return reply, nil
}
func (s *Server) handleConversations(w http.ResponseWriter, r *http.Request) {
	s.handleWorkspaceRecord(w, r, false)
}
func (s *Server) handleDraftPacks(w http.ResponseWriter, r *http.Request) {
	s.handleWorkspaceRecord(w, r, true)
}
func (s *Server) handleWorkspaceRecord(w http.ResponseWriter, r *http.Request, drafts bool) {
	if !s.guard(w, r) || s.refuseUnusableStore(w) {
		return
	}
	graphs := r.URL.Path == "/api/draft-graphs"
	private := graphs || r.URL.Path == "/api/briefs" || r.URL.Path == "/api/pack-tests"
	if private && !s.guardPrivateWorkspace(w, r) {
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	if err := s.ensureDefaultHistoryEnrollment(r.Context()); err != nil {
		storageFailure(w, err)
		return
	}
	pressure := s.runtimeStoragePressureReporter()
	var persistenceErr error
	var persistenceUncertain bool
	// Registered before writer/domain lock defers: reporting uses memory only and
	// never acquires enrollment/owner mutexes while a writer lock is held.
	defer func() { pressure.report(persistenceErr, persistenceUncertain) }()
	var removedContextOwners []string
	var contextDeletionCommitted bool
	var contextBinding assistantRunBinding
	var contextBindingValid bool
	if r.URL.Path == "/api/conversations" && r.Method == http.MethodPut {
		contextBinding, contextBindingValid = s.contextDeletionBinding(r)
	}
	// Registered before workspace lock defers: retirement never takes the
	// assistant service lock while holding the private-data/write transaction.
	defer func() {
		if contextDeletionCommitted && contextBindingValid && len(removedContextOwners) > 0 {
			s.retireSavedContextOwners(contextBinding, removedContextOwners)
		}
	}()
	conversations := r.URL.Path == "/api/conversations" && !drafts
	var body []byte
	if r.Method != http.MethodGet {
		if r.Header.Get("If-Match") == "" && r.URL.Path != "/api/briefs" {
			writeJSONCoded(w, http.StatusBadRequest, CodeBadRequest, "If-Match is required; reload history before saving")
			return
		}
		var err error
		if body, err = readBounded(r.Body, maxConversationBytes); err != nil {
			writeJSONCoded(w, http.StatusRequestEntityTooLarge, CodeTooLarge, "Workspace records are limited to 16 MiB; nothing was written.")
			return
		}
	}
	// The dialect drafts' first use is enrolled before this transaction takes
	// .data.lock, whose domain the enrollment takes itself, as every
	// first-use path is.
	if conversations {
		if err := s.prepareConversationDraftEnrollment(r.Context(), body); err != nil {
			storageFailure(w, err)
			return
		}
	}
	s.writes.Lock()
	defer s.writes.Unlock()
	lock, err := s.privateDataLockContext(r.Context(), true)
	if err != nil {
		storageFailure(w, err)
		return
	}
	defer lock.Close()
	store, err := s.openChatData()
	if err != nil {
		storageFailure(w, err)
		return
	}
	defer store.root.Close()
	root := store.root
	recordName, err := resolveConversationName(root, s.conversationIdentity())
	if err != nil {
		storageFailure(w, err)
		return
	}
	briefs := r.URL.Path == "/api/briefs"
	reviews := r.URL.Path == "/api/source-reviews"
	tests := r.URL.Path == "/api/pack-tests"
	if graphs {
		recordName = graphDraftName(recordName)
	} else if briefs {
		recordName = strings.Replace(recordName, "conversations-", "briefs-", 1)
	} else if reviews {
		recordName = strings.Replace(recordName, "conversations-", "source-reviews-", 1)
	} else if tests {
		recordName = strings.Replace(recordName, "conversations-", "pack-tests-", 1)
	} else if drafts {
		recordName = draftPackName(recordName)
	}
	var recordReadErr error
	read := func() (conversationReply, error) {
		reply, err := s.readWorkspaceRecord(root, recordName, drafts)
		recordReadErr = err
		if err == nil && private {
			err = s.authorizePrivateWorkspace(r)
		}
		if err == nil && !graphs && !briefs && !reviews && !tests {
			err = s.authorizePolicyWorkspace(r, reply.Content, drafts)
		}
		if err == nil && briefs && r.URL.Query().Get("subject") != "" {
			reply.Snapshot, err = s.definitionBriefSnapshot(root, recordName, r.URL.Query().Get("subject"))
			if err != nil {
				reply.SnapshotIssue = err.Error()
				err = nil
			}
		}
		return reply, err
	}
	validate := validateConversations
	if graphs {
		validate = validateGraphDrafts
	} else if briefs {
		validate = validateBriefs
	} else if reviews {
		validate = validateSourceReviews
	} else if tests {
		validate = validatePackTests
	} else if drafts {
		validate = validateDraftPacks
	}
	fail := func(err error) {
		var failure *policyFailure
		if errors.As(err, &failure) {
			policyHTTPFailure(w, err)
			return
		}
		code := codeOf(err)
		writeJSONCoded(w, statusForRefusal(err), code, "Workspace data could not be saved or read: "+err.Error())
	}
	if r.Method == http.MethodGet {
		reply, err := read()
		if err != nil {
			fail(err)
			return
		}
		if !graphs && !briefs && !reviews && !tests && !drafts {
			if err = s.retainConversationDialectDrafts(r.Context(), reply.Content); err != nil {
				fail(err)
				return
			}
		}
		writeConversationReply(w, reply)
		return
	}
	expected := r.Header.Get("If-Match")
	data := body
	if briefs {
		current, e := read()
		if e != nil {
			fail(e)
			return
		}
		data, e = s.changeBrief(root, recordName, current.Content, data)
		if e != nil {
			writeJSONCoded(w, http.StatusConflict, CodeStale, e.Error())
			return
		}
		expected = current.SHA256
		if len(data) > maxConversationBytes {
			writeJSONCoded(w, 413, CodeTooLarge, "Brief storage is full; nothing was changed.")
			return
		}
	}
	if err = validate(data); err != nil {
		writeJSONCoded(w, http.StatusBadRequest, CodeBadRequest, err.Error())
		return
	}
	if !graphs && !briefs && !reviews && !tests {
		if err = s.authorizePolicyWorkspace(r, data, drafts); err != nil {
			fail(err)
			return
		}
	}
	matches := func() error {
		current, err := read()
		if err != nil {
			return err
		}
		if current.SHA256 != expected {
			return withCode(CodeStale, errors.New("history changed in another window; reload before saving"))
		}
		return nil
	}
	if err = matches(); err != nil {
		fail(err)
		return
	}
	if !graphs && !briefs && !reviews && !tests {
		previous, e := read()
		if e == nil {
			e = s.admitPolicyWorkspaceUpdate(r, previous.Content, data, drafts)
		}
		if e != nil {
			fail(e)
			return
		}
	}
	if graphs {
		previous, e := read()
		if e == nil {
			e = s.admitGraphDraftUpdate(r, previous.Content, data)
		}
		if e != nil {
			fail(e)
			return
		}
	}
	if !graphs && !briefs && !reviews && !tests && !drafts {
		previous, e := read()
		if e != nil {
			fail(e)
			return
		}
		data, e = preserveConversationRows(previous.Content, data)
		if e != nil {
			fail(e)
			return
		}
		if e = s.checkAssistantPlanAcceptance(r, previous.Content, data); e != nil {
			fail(e)
			return
		}
		if e = s.retainConversationGraphDrafts(r, root, recordName, previous.Content, data); e != nil {
			fail(e)
			return
		}
		if e = s.retainConversationDialectDrafts(r.Context(), previous.Content, data); e != nil {
			fail(e)
			return
		}
	}
	if hook := s.testBeforeStoragePersistence; hook != nil {
		if err = hook(storagePersistenceTarget{operation: storagePersistenceWorkspace, name: recordName}); err != nil {
			persistenceErr = err
			fail(err)
			return
		}
	}
	stage, name, err := newDataStage(root)
	if err != nil {
		persistenceErr = err
		fail(err)
		return
	}
	defer root.Remove(name)
	defer stage.Close()
	if _, err = stage.Write(data); err != nil {
		persistenceErr = err
		fail(err)
		return
	}
	if err = stage.Chmod(custodyFileMode); err != nil {
		persistenceErr = err
		fail(err)
		return
	}
	if err = stage.Sync(); err != nil {
		persistenceErr = err
		fail(err)
		return
	}
	if err = stage.Close(); err != nil {
		persistenceErr = err
		fail(err)
		return
	}
	// Same bounded compare-before-rename guarantee as desk.json; unrelated
	// processes can still race the final rename (see custody.go).
	if err = matches(); err != nil {
		fail(err)
		return
	}
	if private {
		if err = s.authorizePrivateWorkspace(r); err != nil {
			fail(err)
			return
		}
	}
	if !graphs && !briefs && !reviews && !tests && !drafts {
		previous, e := read()
		var planView inputPreparationRead
		if e == nil {
			planView, e = s.holdAssistantPlanAcceptance(r, previous.Content, data)
		}
		if e != nil {
			fail(e)
			return
		}
		removedContextOwners = removedSavedContextOwners(previous.Content, data)
		if planView != nil {
			defer planView.Close()
		}
		if planView != nil && testHookAssistantPlanAcceptanceCommit != nil {
			testHookAssistantPlanAcceptanceCommit()
		}
	}
	if err = root.Rename(name, recordName); err != nil {
		persistenceErr = err
		fail(err)
		return
	}
	contextDeletionCommitted = true
	if dir, dirErr := root.Open("."); dirErr == nil {
		syncErr := dir.Sync()
		closeErr := dir.Close()
		// Preserve the existing returned-error/success behavior of these
		// best-effort calls, while retaining a volatile durability warning.
		if syncErr != nil {
			persistenceErr, persistenceUncertain = syncErr, true
		} else if closeErr != nil {
			persistenceErr, persistenceUncertain = closeErr, true
		}
	} else {
		persistenceErr, persistenceUncertain = dirErr, true
	}
	reply, err := read()
	if err != nil {
		// Only failed record verification belongs to persistence. Authorization
		// can still refuse this read and must not manufacture storage pressure.
		if recordReadErr != nil {
			persistenceErr, persistenceUncertain = recordReadErr, true
		}
		fail(err)
		return
	}
	if reply.SHA256 != digestOf(data) {
		persistenceErr = withCode(CodeStale, fmt.Errorf("history changed after the write; reload before saving again"))
		persistenceUncertain = true
		fail(persistenceErr)
		return
	}
	writeConversationReply(w, reply)
}

// JSON is served as data, never embedded in HTML. Disabling HTML escaping keeps
// a bounded checkpoint from expanding sixfold in its response envelope.
func writeConversationReply(w http.ResponseWriter, reply conversationReply) {
	var doc conversationDocument
	if json.Unmarshal(reply.Content, &doc) == nil && doc.Chats != nil {
		reply.ChatRevisions = map[string]string{}
		for _, raw := range doc.Chats {
			var row struct {
				ID string `json:"id"`
			}
			if json.Unmarshal(raw, &row) == nil && researchChatID.MatchString(row.ID) {
				reply.ChatRevisions[row.ID] = digestOf(raw)
			}
		}
	}

	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	encoder := json.NewEncoder(w)
	encoder.SetEscapeHTML(false)
	_ = encoder.Encode(reply)
}

func (s *Server) conversationIdentity() string {
	if entry := s.homeEntry(); entry != nil {
		return entry.HistoryIdentity
	}
	if s.cfg.deskID != "" {
		return "desk:" + s.cfg.deskID
	}
	return s.projectDir
}

// An unrelated document CAS must not rewrite a run's pinned raw source merely
// because a browser serialized equivalent JSON with different spacing/escapes.
// Numbers retain their lexical representation: 1, 1.0 and rounded integers differ.
func preserveConversationRows(previous, incoming []byte) ([]byte, error) {
	if !uniqueJSON(previous) || !uniqueJSON(incoming) {
		return nil, errAssistantRunInvalid
	}
	oldDoc, oldRows, err := assistantChatRows(previous)
	if err != nil {
		return nil, err
	}
	_ = oldDoc
	newDoc, newRows, err := assistantChatRows(incoming)
	if err != nil {
		return nil, err
	}
	old := map[string]json.RawMessage{}
	for _, row := range oldRows {
		fields, e := assistantChatObject(row)
		if e != nil {
			return nil, e
		}
		var id string
		if json.Unmarshal(fields["id"], &id) != nil || !researchChatID.MatchString(id) || old[id] != nil {
			return nil, errAssistantRunInvalid
		}
		old[id] = row
	}
	seen := map[string]bool{}
	for i, row := range newRows {
		fields, e := assistantChatObject(row)
		if e != nil {
			return nil, e
		}
		var id string
		if json.Unmarshal(fields["id"], &id) != nil || !researchChatID.MatchString(id) || seen[id] {
			return nil, errAssistantRunInvalid
		}
		seen[id] = true
		before := old[id]
		if before == nil {
			continue
		}
		decode := func(raw []byte) (any, error) {
			d := json.NewDecoder(bytes.NewReader(raw))
			d.UseNumber()
			var v any
			e := d.Decode(&v)
			return v, e
		}
		a, e := decode(before)
		if e != nil {
			return nil, e
		}
		b, e := decode(row)
		if e != nil {
			return nil, e
		}
		if reflect.DeepEqual(a, b) {
			newRows[i] = before
		}
	}
	newDoc["chats"] = assistantChatEncodeArray(newRows)
	return assistantChatEncodeObject(newDoc), nil
}

// retentionConversationReferences is deliberately a closed supported subset.
// Opaque checkpoints/reports/extensions protect the entire joined selection;
// known attachment references are still recorded for truthful explanations.
func retentionConversationReferences(name string, raw []byte) storageRetentionSource {
	out := storageRetentionUnknown(name, "conversations", raw, "opaque-chat-fields")
	if validateConversations(raw) != nil || !uniqueJSON(raw) {
		out.Reasons = []string{"invalid-chat-source"}
		return out
	}
	var doc struct {
		Version int               `json:"version"`
		Chats   []json.RawMessage `json:"chats"`
	}
	if json.Unmarshal(raw, &doc) != nil {
		return out
	}
	complete := storageRetentionMembers(raw, "version", "chats")
	namespace := strings.TrimSuffix(strings.TrimPrefix(name, "conversations-"), ".json")
	for _, row := range doc.Chats {
		var chat struct {
			ID          string            `json:"id"`
			Title       string            `json:"title"`
			Name        string            `json:"name"`
			CreatedAt   string            `json:"createdAt"`
			UpdatedAt   string            `json:"updatedAt"`
			Documents   []json.RawMessage `json:"documents"`
			Attachments []json.RawMessage `json:"attachments"`
			Messages    []json.RawMessage `json:"messages"`
		}
		if json.Unmarshal(row, &chat) != nil {
			complete = false
			continue
		}
		if !storageRetentionMembers(row, "id", "title", "name", "createdAt", "updatedAt", "documents", "attachments", "messages") || len(chat.Messages) != 0 {
			complete = false
		}
		for _, key := range []string{"id", "title", "name", "createdAt", "updatedAt", "documents", "attachments", "messages"} {
			if storageRetentionNullMember(row, key) {
				complete = false
			}
		}
		for _, original := range append(chat.Documents, chat.Attachments...) {
			ref, supported := retentionDocumentReference(original)
			if !supported {
				complete = false
			}
			if assistantDocumentReferenceValid(ref) {
				out.add(storageRetentionReference{Kind: "attachment", ID: namespace + ":" + ref.ID, Revision: ref.Digest})
			}
		}
	}
	out.Complete = complete && len(out.Reasons) == 1
	if out.Complete {
		out.Reasons = []string{}
	}
	return out
}

// Original RawMessage members are checked before projecting a reference. A
// future nested value, alias or null cannot disappear into a negative proof.
func retentionDocumentReference(raw json.RawMessage) (ref assistantDocumentReference, complete bool) {
	if json.Unmarshal(raw, &ref) != nil {
		return ref, false
	}
	complete = uniqueJSON(raw) && storageRetentionMembers(raw, "id", "digest", "pages", "allowPartial", "needsReview") && assistantDocumentReferenceValid(ref)
	var fields map[string]json.RawMessage
	if json.Unmarshal(raw, &fields) != nil || fields == nil {
		return ref, false
	}
	for _, key := range []string{"id", "digest", "pages", "allowPartial"} {
		if _, ok := fields[key]; !ok {
			complete = false
		}
	}
	for _, key := range []string{"id", "digest", "pages", "allowPartial", "needsReview"} {
		if storageRetentionNullMember(raw, key) {
			complete = false
		}
	}
	return ref, complete
}
