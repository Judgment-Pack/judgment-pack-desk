package desk

// Standalone authoring requests own real host reservations. Editor bytes and
// prompt arguments are untrusted request data; saved declarations and bytes are
// captured independently through Desk's rooted readers. Neither grants a write.
import (
	"bytes"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"reflect"
	"regexp"

	"github.com/Judgment-Pack/judgment-pack-desk/internal/jsonscalar"
)

var assistantAuthoringKey = regexp.MustCompile(`^[A-Za-z0-9_-]{1,100}$`)
var assistantAuthoringID = regexp.MustCompile(`^author_[a-f0-9]{32}$`)
var assistantAuthoringAdmission = regexp.MustCompile(`^author-admit-[a-f0-9]{32}$`)

func assistantAuthoringPurpose(p string) bool {
	return contains([]string{"author-pack", "describe-pack", "author-graph"}, p)
}

type assistantAuthoringRequest struct {
	RequestID   string            `json:"requestId"`
	Purpose     string            `json:"purpose"`
	Prompt      string            `json:"prompt"`
	Arguments   map[string]string `json:"arguments"`
	Draft       *string           `json:"draft,omitempty"`
	Path        string            `json:"path,omitempty"`
	GraphID     string            `json:"graphId,omitempty"`
	GraphDigest string            `json:"graphDigest,omitempty"`
	Packs       []string          `json:"packs,omitempty"`
	Model       string            `json:"model"`
	Thinking    string            `json:"thinking"`
	Effort      string            `json:"effort,omitempty"`
	Phase       string            `json:"phase"`
}
type assistantAuthoringScope struct {
	Principal          string `json:"principal"`
	Session            string `json:"session"`
	Policy             string `json:"policy"`
	Project            string `json:"project"`
	ConnectionID       string `json:"connectionId"`
	ConnectionRevision string `json:"connectionRevision"`
	ProfileRevision    string `json:"profileRevision"`
}
type assistantAuthoringCapture struct {
	ID          string                  `json:"id"`
	AdmissionID string                  `json:"admissionId"`
	RequestID   string                  `json:"requestId"`
	Scope       assistantAuthoringScope `json:"scope"`
	Cancelled   bool                    `json:"cancelled,omitempty"`
	Request     []byte                  `json:"request,omitempty"`
	Snapshot    []byte                  `json:"snapshot,omitempty"`
	Revision    string                  `json:"revision,omitempty"`
}
type assistantAuthoringDocument struct {
	ID          string `json:"id"`
	Path        string `json:"path"`
	Declaration string `json:"declaration"`
	Bytes       []byte `json:"bytes"`
}
type assistantAuthoringSnapshot struct {
	Pack  *assistantTestRead           `json:"pack,omitempty"`
	Graph *assistantAuthoringDocument  `json:"graph,omitempty"`
	Packs []assistantAuthoringDocument `json:"packs,omitempty"`
}
type assistantAuthoringReply struct {
	Start     *assistantRunStart `json:"start,omitempty"`
	Run       *assistantRunView  `json:"run,omitempty"`
	Cancelled bool               `json:"cancelled,omitempty"`
}

func validateAssistantAuthoringRequest(a assistantAuthoringRequest) error {
	if !assistantAuthoringKey.MatchString(a.RequestID) || !assistantAuthoringPurpose(a.Purpose) || !assistantRunText(a.Model, 128) || !contains([]string{"off", "on", "ultra"}, a.Thinking) || !contains([]string{"", "none", "minimal", "low", "medium", "high", "xhigh"}, a.Effort) || !contains([]string{"work", "plan"}, a.Phase) || len(a.Packs) > 32 {
		return errAssistantRunInvalid
	}
	if a.Path != "" {
		if p, e := wireRelativePath(a.Path); e != nil || p != a.Path {
			return errAssistantRunInvalid
		}
	}
	if a.Draft != nil && (len(*a.Draft) > 2_000_000 || !validUTF8([]byte(*a.Draft))) {
		return errAssistantRunInvalid
	}
	argument := "policy"
	switch a.Purpose {
	case "author-pack":
		if a.Prompt == "fix_pack" {
			argument = "diagnostics"
		} else if a.Prompt != "author_pack" {
			return errAssistantRunInvalid
		}
		if a.GraphID != "" || a.GraphDigest != "" || len(a.Packs) != 0 {
			return errAssistantRunInvalid
		}
	case "describe-pack":
		if a.Prompt != "author_pack" || a.Path != "" || a.Draft != nil || a.GraphID != "" || a.GraphDigest != "" || len(a.Packs) != 0 {
			return errAssistantRunInvalid
		}
	case "author-graph":
		argument = "relationship"
		if (a.GraphID != "" && !assistantRunDigest(a.GraphDigest)) || (a.GraphID == "" && a.GraphDigest != "") {
			return errAssistantRunInvalid
		}
		if a.Prompt != "author_graph" || a.Path != "" || a.Draft != nil || len(a.Packs) == 0 || a.GraphID != "" && !assistantDecisionID(a.GraphID) {
			return errAssistantRunInvalid
		}
	}
	if len(a.Arguments) != 1 {
		return errAssistantRunInvalid
	}
	if value, ok := a.Arguments[argument]; !ok || len(value) > 2_000_000 || !validUTF8([]byte(value)) {
		return errAssistantRunInvalid
	}
	seen := map[string]bool{}
	for _, id := range a.Packs {
		if !assistantDecisionID(id) || seen[id] {
			return errAssistantRunInvalid
		}
		seen[id] = true
	}
	return nil
}
func readAssistantAuthoringRequest(raw []byte) (assistantAuthoringRequest, error) {
	var a assistantAuthoringRequest
	if len(raw) > assistantRunRecordLimit || !validUTF8(raw) || jsonscalar.ValidateEscapes(raw) != nil || !uniqueJSON(raw) || readAssistantDialectCustody(raw, reflect.TypeOf(a)) != nil || assistantHostArguments(raw, &a) != nil {
		return a, errAssistantRunInvalid
	}
	return a, validateAssistantAuthoringRequest(a)
}
func assistantAuthoringScopeOf(identity assistantRunBinding, project string) assistantAuthoringScope {
	return assistantAuthoringScope{Principal: identity.Principal, Session: identity.Session, Policy: identity.Policy, Project: project}
}
func assistantAuthoringIdentity(scope assistantAuthoringScope, identity assistantRunBinding, project string) bool {
	return scope.Principal == identity.Principal && scope.Session == identity.Session && scope.Policy == identity.Policy && scope.Project == project
}
func (s *Server) assistantAuthoringSelection(r *http.Request, scope *assistantAuthoringScope, model string) (*aiConnection, *assistantProfile, error) {
	connection, err := s.selectedAIConnection(r, true)
	if err != nil || connection == nil {
		return nil, nil, errAssistantRunScope
	}
	profile, err := s.readAssistantProfile()
	if err != nil || !contains(connectionModels(connection, profile), model) {
		return nil, nil, errAssistantRunScope
	}
	raw, _ := json.Marshal(profile)
	scope.ConnectionID, scope.ConnectionRevision, scope.ProfileRevision = connection.ID, connection.Revision, digestOf(raw)
	return connection, profile, nil
}

// Called under the existing private-data transaction. No provider/Runtime work.
func (s *Server) assistantAuthoringSnapshot(root *os.Root, a assistantAuthoringRequest) ([]byte, error) {
	snapshot := assistantAuthoringSnapshot{}
	if a.Purpose == "describe-pack" {
		return json.Marshal(snapshot)
	}
	config, _, err := s.assistantDecisionDeclarations()
	if err != nil {
		return nil, err
	}
	read := func(entries map[string]assistantDecisionDeclaration, id string) (assistantAuthoringDocument, error) {
		entry, e := assistantDecisionEntry(entries, id)
		if e != nil {
			return assistantAuthoringDocument{}, e
		}
		file := s.readProjectFile(entry.Path)
		if file.err != nil || !file.present || !validUTF8(file.data) || len(file.data) > 2_000_000 {
			return assistantAuthoringDocument{}, errAssistantRunScope
		}
		return assistantAuthoringDocument{ID: id, Path: entry.Path, Declaration: assistantDeclarationDigest(entry), Bytes: bytes.Clone(file.data)}, nil
	}
	if a.Purpose == "author-pack" && a.Path != "" {
		var owner string
		for id, entry := range config.Packs {
			if entry.Path == a.Path {
				if owner != "" {
					return nil, errAssistantRunConflict
				}
				owner = id
			}
		}
		// A file outside the saved declarations stays an untrusted unsaved draft.
		if owner != "" {
			name, e := resolveConversationName(root, s.conversationIdentity())
			if e != nil {
				return nil, e
			}
			target, e := s.readAssistantTestTargetLocked(root, name, config, owner)
			if e != nil || target.Draft || target.Path != a.Path {
				return nil, errAssistantRunScope
			}
			snapshot.Pack = &target
		}
	}
	if a.Purpose == "author-graph" {
		for _, id := range a.Packs {
			doc, e := read(config.Packs, id)
			if e != nil {
				return nil, e
			}
			snapshot.Packs = append(snapshot.Packs, doc)
		}
		if a.GraphID != "" {
			doc, e := read(config.Graphs, a.GraphID)
			if e != nil {
				return nil, e
			}
			if digestOf(doc.Bytes) != a.GraphDigest {
				return nil, errAssistantRunConflict
			}
			snapshot.Graph = &doc
		}
	}
	return json.Marshal(snapshot)
}
func validateAssistantAuthoringCaptures(ledger assistantRunLedger) error {
	if len(ledger.AuthoringCaptures)+len(ledger.Runs) > assistantRunCountLimit {
		return errAssistantRunInvalid
	}
	ids, keys, admissions := map[string]bool{}, map[string]bool{}, map[string]bool{}
	for _, c := range ledger.AuthoringCaptures {
		key := c.Scope.Principal + ":" + c.RequestID
		if !assistantAuthoringID.MatchString(c.ID) || !assistantAuthoringAdmission.MatchString(c.AdmissionID) || !assistantAuthoringKey.MatchString(c.RequestID) || ids[c.ID] || keys[key] || admissions[c.AdmissionID] {
			return errAssistantRunInvalid
		}
		ids[c.ID], keys[key], admissions[c.AdmissionID] = true, true, true
		for _, d := range []string{c.Scope.Principal, c.Scope.Session, c.Scope.Policy, c.Scope.Project} {
			if !assistantRunDigest(d) {
				return errAssistantRunInvalid
			}
		}
		if c.Scope.Project != ledger.Project {
			return errAssistantRunScope
		}
		a, err := readAssistantAuthoringRequest(c.Request)
		if err != nil || a.RequestID != c.RequestID || !assistantRunDigest(c.Scope.ConnectionRevision) || !assistantRunDigest(c.Scope.ProfileRevision) || !aiIDPattern.MatchString(c.Scope.ConnectionID) || !assistantRunDigest(c.Revision) || c.Revision != assistantAuthoringRevision(c) {
			return errAssistantRunInvalid
		}
		var snapshot assistantAuthoringSnapshot
		if readAssistantDialectCustody(c.Snapshot, reflect.TypeOf(snapshot)) != nil || json.Unmarshal(c.Snapshot, &snapshot) != nil {
			return errAssistantRunInvalid
		}
		// Snapshot shape and exact saved bytes remain independent from draft bytes.
		if a.Purpose == "describe-pack" && (snapshot.Pack != nil || snapshot.Graph != nil || len(snapshot.Packs) != 0) || a.Purpose == "author-pack" && (snapshot.Graph != nil || len(snapshot.Packs) != 0) || a.Purpose == "author-graph" && (snapshot.Pack != nil || len(snapshot.Packs) != len(a.Packs) || (snapshot.Graph == nil) != (a.GraphID == "")) {
			return errAssistantRunInvalid
		}
		if snapshot.Pack != nil {
			p := snapshot.Pack
			if !assistantDecisionID(p.Owner) || p.Draft || p.Path != a.Path || !assistantRunDigest(p.Declaration) || digestOf(p.Pack) != p.PackDigest {
				return errAssistantRunInvalid
			}
			if _, e := assistantTestSuite(p.Suite); e != nil {
				return e
			}
		}
		for i, p := range snapshot.Packs {
			if p.ID != a.Packs[i] || !assistantRunDigest(p.Declaration) || !validUTF8(p.Bytes) {
				return errAssistantRunInvalid
			}
		}
		if snapshot.Graph != nil && (snapshot.Graph.ID != a.GraphID || !assistantRunDigest(snapshot.Graph.Declaration) || !validUTF8(snapshot.Graph.Bytes) || digestOf(snapshot.Graph.Bytes) != a.GraphDigest) {
			return errAssistantRunInvalid
		}
	}
	for _, run := range ledger.Runs {
		if assistantAuthoringPurpose(run.Binding.Owner.Kind) {
			found := false
			for _, c := range ledger.AuthoringCaptures {
				if c.ID == run.Binding.Owner.ID {
					a, err := readAssistantAuthoringRequest(c.Request)
					if err != nil || a.Purpose != run.Binding.Owner.Kind || !assistantAuthoringIdentity(c.Scope, run.Binding, ledger.Project) || c.Scope.ConnectionID != run.Binding.ConnectionID || c.Scope.ConnectionRevision != run.Binding.ConnectionRevision || c.Scope.ProfileRevision != run.Binding.ProfileRevision || a.Model != run.Binding.Model || a.Phase != run.Binding.Phase || len(run.Binding.SourceDigests) != 1 || run.Binding.SourceDigests[0] != digestOf(c.Snapshot) || c.AdmissionID != run.AdmissionID || c.Revision != run.Binding.Owner.Revision || c.RequestID != run.Binding.RequestID || digestOf(c.Request) != run.Binding.RequestDigest {
						return errAssistantRunInvalid
					}
					found = true
				}
			}
			if !found {
				return errAssistantRunInvalid
			}
		}
	}
	return nil
}
func assistantAuthoringRevision(c assistantAuthoringCapture) string {
	raw, _ := json.Marshal(struct {
		Scope             assistantAuthoringScope
		Request, Snapshot []byte
	}{c.Scope, c.Request, c.Snapshot})
	return digestOf(raw)
}
func assistantAuthoringStart(c assistantAuthoringCapture) *assistantRunStart {
	if c.Request == nil {
		return nil
	}
	a, _ := readAssistantAuthoringRequest(c.Request)
	return &assistantRunStart{AdmissionID: c.AdmissionID, Purpose: a.Purpose, OwnerID: c.ID, Revision: c.Revision, RequestID: c.RequestID}
}
func newAssistantAuthoringCapture(scope assistantAuthoringScope, key string) (assistantAuthoringCapture, error) {
	var nonce [32]byte
	if _, err := rand.Read(nonce[:]); err != nil {
		return assistantAuthoringCapture{}, err
	}
	return assistantAuthoringCapture{ID: "author_" + hex.EncodeToString(nonce[:16]), AdmissionID: "author-admit-" + hex.EncodeToString(nonce[16:]), RequestID: key, Scope: scope}, nil
}
func (s *Server) resolveAssistantAuthoringRun(r *http.Request, start assistantRunStart, identity assistantRunBinding, project string) (assistantRunSource, error) {
	zero := assistantRunSource{}
	if !assistantAuthoringPurpose(start.Purpose) || !assistantAuthoringID.MatchString(start.OwnerID) || !assistantRunDigest(start.Revision) {
		return zero, errAssistantRunInvalid
	}
	data, err := s.openChatData()
	if err != nil {
		return zero, err
	}
	defer data.root.Close()
	raw, err := readPrivateData(data.root, "assistant-runs-"+project+".json", assistantRunRecordLimit)
	if err != nil {
		return zero, err
	}
	ledger, err := decodeAssistantRunStorage("assistant-runs-"+project+".json", raw)
	if err != nil {
		return zero, err
	}
	for _, c := range ledger.AuthoringCaptures {
		if c.ID == start.OwnerID {
			expected := assistantAuthoringStart(c)
			if c.Cancelled || expected == nil || *expected != start || !assistantAuthoringIdentity(c.Scope, identity, project) {
				return zero, errAssistantRunScope
			}
			a, err := readAssistantAuthoringRequest(c.Request)
			if err != nil {
				return zero, err
			}
			current := assistantAuthoringScopeOf(identity, project)
			held := r.Clone(r.Context())
			held.Header.Set(aiConnectionHeader, c.Scope.ConnectionID)
			held.Header.Set(aiRevisionHeader, c.Scope.ConnectionRevision)
			conn, profile, err := s.assistantAuthoringSelection(held, &current, a.Model)
			if err != nil || current != c.Scope {
				return zero, errAssistantRunScope
			}
			snapshot, err := s.assistantAuthoringSnapshot(data.root, a)
			if err != nil {
				return zero, err
			}
			if !bytes.Equal(snapshot, c.Snapshot) {
				return zero, errAssistantRunConflict
			}
			binding := identity
			binding.Project = project
			binding.Owner = assistantRunOwner{Kind: a.Purpose, ID: c.ID, Revision: c.Revision}
			binding.RequestID = c.RequestID
			binding.RequestDigest = digestOf(c.Request)
			binding.SourceDigests = []string{digestOf(c.Snapshot)}
			binding.ConnectionID = c.Scope.ConnectionID
			binding.ConnectionRevision = c.Scope.ConnectionRevision
			binding.ProfileRevision = c.Scope.ProfileRevision
			binding.Model = a.Model
			binding.Phase = a.Phase
			return assistantRunSource{Start: start, Binding: binding, Original: bytes.Clone(c.Snapshot), Request: bytes.Clone(c.Request), Text: a.Arguments["policy"], Connection: *conn, Profile: *profile}, nil
		}
	}
	return zero, errAssistantRunNotFound
}

func (s *Server) registerAssistantAuthoringRuns() {
	s.mux.HandleFunc("POST /api/assistant-authoring/captures", s.handleAssistantAuthoringCapture)
	s.mux.HandleFunc("GET /api/assistant-authoring/captures/{request}", s.handleAssistantAuthoringCapture)
	s.mux.HandleFunc("POST /api/assistant-authoring/captures/{request}/cancel", s.handleAssistantAuthoringCapture)
}
func (s *Server) handleAssistantAuthoringCapture(w http.ResponseWriter, r *http.Request) {
	if !s.guardAssistantRun(w, r) {
		return
	}
	identity, _, err := s.assistantRunIdentity(r)
	if err != nil {
		assistantRunHTTPError(w, err)
		return
	}
	var raw []byte
	var a assistantAuthoringRequest
	var cancellation struct {
		OwnerID  string `json:"ownerId"`
		Revision string `json:"revision"`
	}
	key := r.PathValue("request")
	cancel := r.Method == http.MethodPost && key != ""
	if r.Method == http.MethodPost && !cancel {
		raw, err = readBounded(r.Body, assistantRunRecordLimit)
		if err == nil {
			a, err = readAssistantAuthoringRequest(raw)
		}
		key = a.RequestID
	} else if !assistantAuthoringKey.MatchString(key) {
		err = errAssistantRunInvalid
	} else if cancel {
		err = assistantRunRequest(r, &cancellation)
	} else if r.ContentLength != 0 {
		err = errAssistantRunInvalid
	}
	if err != nil {
		assistantRunHTTPError(w, err)
		return
	}
	store := s.assistantRuns()
	var reply assistantAuthoringReply
	_, err = store.transaction(r.Context(), func(ledger *assistantRunLedger) (bool, error) {
		for i := range ledger.AuthoringCaptures {
			c := &ledger.AuthoringCaptures[i]
			if c.RequestID == key && c.Scope.Principal == identity.Principal {
				if !assistantAuthoringIdentity(c.Scope, identity, store.project) {
					return false, errAssistantRunScope
				}
				if raw != nil && c.Request != nil && !bytes.Equal(raw, c.Request) {
					return false, errAssistantRunConflict
				}
				if cancel && (cancellation.OwnerID != c.ID || cancellation.Revision != c.Revision) {
					return false, errAssistantRunScope
				}
				changed := cancel && !c.Cancelled
				if cancel {
					c.Cancelled = true
				}
				reply.Start, reply.Cancelled = assistantAuthoringStart(*c), c.Cancelled
				for j := range ledger.Runs {
					run := &ledger.Runs[j]
					if run.Binding.Owner.ID == c.ID && run.AdmissionID == c.AdmissionID {
						// Closing the reservation and any admitted run is one
						// atomic transaction, including a lost start response.
						if cancel && !run.Historical && !assistantRunTerminal(run.State) {
							action := assistantRunAction{ID: "author-cancel-" + c.ID, Kind: "cancel", Revision: run.Revision}
							run.Actions = append(run.Actions, assistantRunActionReceipt{ID: action.ID, Digest: assistantRunActionDigest(action), Kind: "cancel"})
							stopAssistantRun(run)
							run.Revision++
							changed = true
						}
						view := assistantRunPublic(*run)
						reply.Run = &view
					}
				}
				return changed, nil
			}
		}
		if r.Method == http.MethodGet || cancel {
			return false, errAssistantRunNotFound
		}
		if len(ledger.AuthoringCaptures)+len(ledger.Runs) >= assistantRunCountLimit {
			return false, withCode(CodeTooLarge, errors.New("Assistant request retention is full. Retained captures are not evicted or replayed."))
		}
		scope := assistantAuthoringScopeOf(identity, store.project)
		c, e := newAssistantAuthoringCapture(scope, key)
		if e != nil {
			return false, e
		}
		{
			if _, _, e = s.assistantAuthoringSelection(r, &c.Scope, a.Model); e != nil {
				return false, e
			}
			data, e := s.openChatData()
			if e != nil {
				return false, e
			}
			defer data.root.Close()
			c.Snapshot, e = s.assistantAuthoringSnapshot(data.root, a)
			if e != nil {
				return false, e
			}
			c.Request = bytes.Clone(raw)
			c.Revision = assistantAuthoringRevision(c)
		}
		ledger.AuthoringCaptures = append(ledger.AuthoringCaptures, c)
		reply.Start, reply.Cancelled = assistantAuthoringStart(c), c.Cancelled
		return true, nil
	})
	if err != nil {
		assistantRunHTTPError(w, err)
		return
	}
	if cancel && reply.Start != nil {
		// No I/O/dispatch while the private-data lock is held. This only
		// interrupts the exact in-memory owner after its durable close.
		host := s.assistantRunService()
		host.mu.Lock()
		for _, active := range host.active {
			if active.source.Start == *reply.Start && assistantRunIdentityMatches(identity, active.record.Binding) {
				active.cancel()
				host.notifyLocked(active.record.ID)
			}
		}
		host.mu.Unlock()
	}
	writeJSON(w, 200, reply)
}
