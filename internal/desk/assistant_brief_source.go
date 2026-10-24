package desk

// A brief owns its existing reservation, never a synthetic conversation. Starts
// carry only its subject, token and frozen-snapshot digest.
import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"regexp"
	"strings"
	"time"
)

var assistantBriefJob = regexp.MustCompile(`^(job:job_|run:run_)[a-f0-9]{32}$`)

func assistantBriefSubject(subject string) bool {
	if !assistantRunText(subject, 1024) {
		return false
	}
	if definitionBriefSubject(subject) || assistantBriefJob.MatchString(subject) {
		return true
	}
	return strings.HasPrefix(subject, "case:") && regexp.MustCompile(`^case:.+:.+$`).MatchString(subject)
}

type assistantBriefReply struct {
	Project  string          `json:"project,omitempty"`
	Content  BriefStore      `json:"content"`
	Snapshot json.RawMessage `json:"snapshot,omitempty"`
}

type assistantBriefCapture struct {
	header http.Header
	code   int
	body   bytes.Buffer
}

func (w *assistantBriefCapture) Header() http.Header { return w.header }
func (w *assistantBriefCapture) WriteHeader(code int) {
	if w.code == 0 {
		w.code = code
	}
}
func (w *assistantBriefCapture) Write(data []byte) (int, error) {
	w.WriteHeader(http.StatusOK)
	if w.body.Len()+len(data) > runnerAnswerLimit {
		return 0, errAssistantRunInvalid
	}
	return w.body.Write(data)
}

func assistantBriefRunnerPath(subject string) string {
	kind, id, _ := strings.Cut(subject, ":")
	return kind + "s/" + id + "/briefs"
}

// Only the closed Runner brief route reaches this existing companion boundary.
// No caller-controlled path, query, credentials or executable offer is forwarded.
func (s *Server) assistantBriefRunner(r *http.Request, subject string, command *BriefCommand) (assistantBriefReply, error) {
	if !assistantBriefJob.MatchString(subject) {
		return assistantBriefReply{}, errAssistantRunInvalid
	}
	method := http.MethodGet
	var body []byte
	if command != nil {
		method = http.MethodPost
		body, _ = json.Marshal(command)
	}
	request := r.Clone(r.Context())
	request.Method = method
	request.URL.Path = "/api/operations/" + assistantBriefRunnerPath(subject)
	request.URL.RawQuery = ""
	request.Header.Del("Idempotency-Key")
	request.Body = io.NopCloser(bytes.NewReader(body))
	request.ContentLength = int64(len(body))
	response := &assistantBriefCapture{header: http.Header{}}
	s.proxyJobs(response, request, assistantBriefRunnerPath(subject), "")
	var reply assistantBriefReply
	if response.code != http.StatusOK || !uniqueJSON(response.body.Bytes()) || json.Unmarshal(response.body.Bytes(), &reply) != nil || reply.Content.Version != 1 || reply.Content.Subjects == nil {
		return reply, errAssistantRunConflict
	}
	return reply, nil
}

// The local read is called under the service's short private-data lock. Runner
// subjects are read from their authoritative companion, not browser snapshots.
func (s *Server) assistantBriefRead(r *http.Request, subject string) (assistantBriefReply, error) {
	if !assistantBriefSubject(subject) {
		return assistantBriefReply{}, errAssistantRunInvalid
	}
	if assistantBriefJob.MatchString(subject) {
		return s.assistantBriefRunner(r, subject, nil)
	}
	store, err := s.openChatData()
	if err != nil {
		return assistantBriefReply{}, err
	}
	defer store.root.Close()
	name, err := resolveConversationName(store.root, s.conversationIdentity())
	if err != nil {
		return assistantBriefReply{}, err
	}
	name = strings.Replace(name, "conversations-", "briefs-", 1)
	saved, err := s.readWorkspaceRecord(store.root, name, false)
	if err != nil {
		return assistantBriefReply{}, err
	}
	var reply assistantBriefReply
	if json.Unmarshal(saved.Content, &reply.Content) != nil || validateBriefs(saved.Content) != nil {
		return reply, errAssistantRunInvalid
	}
	if definitionBriefSubject(subject) {
		reply.Snapshot, err = s.definitionBriefSnapshot(store.root, name, subject)
	} else {
		reply.Snapshot, err = s.assistantBriefCaseSnapshot(store.root, name, subject)
	}
	return reply, err
}

func (s *Server) resolveAssistantBriefRun(r *http.Request, start assistantRunStart, identity assistantRunBinding, project string) (assistantRunSource, error) {
	var zero assistantRunSource
	if start.Purpose != "brief" || !assistantBriefSubject(start.OwnerID) || start.AdmissionID != "brief-"+start.RequestID || !regexp.MustCompile(`^[a-f0-9]{32}$`).MatchString(start.RequestID) || !assistantRunDigest(start.Revision) {
		return zero, errAssistantRunInvalid
	}
	reply, err := s.assistantBriefRead(r, start.OwnerID)
	if err != nil {
		return zero, err
	}
	pending := reply.Content.Subjects[start.OwnerID].Pending
	if pending == nil || pending.Token != start.RequestID || digestOf(pending.Snapshot) != start.Revision || !sameBriefJSON(pending.Snapshot, reply.Snapshot) {
		return zero, errAssistantRunConflict
	}
	expires, err := time.Parse(time.RFC3339, pending.Expires)
	if err != nil || !time.Now().Before(expires) {
		return zero, errAssistantRunConflict
	}
	connection, err := s.selectedAIConnection(r, true)
	if err != nil || connection == nil {
		return zero, errAssistantRunScope
	}
	profile, err := s.readAssistantProfile()
	if err != nil || !contains(connectionModels(connection, profile), pending.Model) {
		return zero, errAssistantRunScope
	}
	profileRaw, _ := json.Marshal(profile)
	request, _ := json.Marshal(struct{ Subject, Token, Model string }{start.OwnerID, pending.Token, pending.Model})
	binding := identity
	binding.Project, binding.Owner = project, assistantRunOwner{Kind: "brief", ID: start.OwnerID, Revision: start.Revision}
	binding.RequestID, binding.RequestDigest = start.RequestID, digestOf(request)
	binding.SourceDigests = []string{digestOf(pending.Snapshot)}
	binding.ConnectionID, binding.ConnectionRevision = connection.ID, connection.Revision
	binding.ProfileRevision, binding.Model, binding.Phase = digestOf(profileRaw), pending.Model, "work"
	return assistantRunSource{Start: start, Binding: binding, Original: bytes.Clone(pending.Snapshot), Request: request, Text: assistantBriefInstruction, Connection: *connection, Profile: *profile}, nil
}

// Reconstruct the case and its matching historical observation from saved
// records. A browser cannot supply pack bytes, a result or research metadata.
func (s *Server) assistantBriefCaseSnapshot(root *os.Root, name, subject string) (json.RawMessage, error) {
	saved, err := s.readWorkspaceRecord(root, strings.Replace(name, "briefs-", "pack-tests-", 1), false)
	if err != nil {
		return nil, err
	}
	type briefSuite struct {
		Cases    []json.RawMessage `json:"cases"`
		Runs     []json.RawMessage `json:"runs"`
		Research json.RawMessage   `json:"research"`
	}
	var tests struct {
		Suites map[string]briefSuite `json:"suites"`
	}
	if json.Unmarshal(saved.Content, &tests) != nil {
		return nil, errAssistantRunInvalid
	}
	var owner, id, title string
	var chosen json.RawMessage
	var suite briefSuite
	for key, candidateSuite := range tests.Suites {
		for _, candidate := range candidateSuite.Cases {
			var c struct{ ID, Name string }
			if json.Unmarshal(candidate, &c) == nil && "case:"+key+":"+c.ID == subject {
				if chosen != nil {
					return nil, errAssistantRunConflict
				}
				owner, id, title, chosen, suite = key, c.ID, c.Name, candidate, candidateSuite
			}
		}
	}
	if chosen == nil {
		return nil, errAssistantRunConflict
	}
	definition, err := s.definitionBriefSnapshot(root, name, "pack:"+owner)
	if err != nil {
		definition, err = s.definitionBriefSnapshot(root, name, "pack-draft:"+owner)
	}
	if err != nil {
		return nil, err
	}
	var pack definitionBrief
	if json.Unmarshal(definition, &pack) != nil {
		return nil, errAssistantRunInvalid
	}
	record := map[string]any{"case": chosen}
	if briefResearchPresent(suite.Research) {
		record["research"] = suite.Research
	}
	for i := len(suite.Runs) - 1; i >= 0; i-- {
		var run map[string]json.RawMessage
		if json.Unmarshal(suite.Runs[i], &run) != nil {
			return nil, errAssistantRunInvalid
		}
		var packText string
		var cases []json.RawMessage
		_ = json.Unmarshal(run["packText"], &packText)
		_ = json.Unmarshal(run["cases"], &cases)
		matched := false
		for _, c := range cases {
			matched = matched || sameBriefJSON(c, chosen)
		}
		if packText != pack.Pack || !matched {
			continue
		}
		observation := map[string]json.RawMessage{}
		for _, key := range []string{"id", "at", "packDigest", "trial", "error"} {
			if value := run[key]; value != nil {
				observation[key] = value
			}
		}
		var report struct {
			Packs []struct {
				Rows []json.RawMessage `json:"rows"`
			} `json:"packs"`
		}
		_ = json.Unmarshal(run["report"], &report)
		for _, p := range report.Packs {
			for _, row := range p.Rows {
				var value struct{ ID string }
				if json.Unmarshal(row, &value) == nil && value.ID == id && observation["result"] == nil {
					observation["result"] = row
				}
			}
		}
		record["run"] = observation
		break
	}
	return json.Marshal(definitionBrief{Kind: "case", Title: title, Pack: pack.Pack, Record: record})
}

func briefResearchPresent(raw json.RawMessage) bool {
	var value any
	if json.Unmarshal(raw, &value) != nil {
		return false
	}
	switch value := value.(type) {
	case nil:
		return false
	case bool:
		return value
	case string:
		return value != ""
	case float64:
		return value != 0
	default:
		return true
	}
}
