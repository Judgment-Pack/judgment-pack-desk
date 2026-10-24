package desk

import (
	"encoding/json"
	"errors"
	"net/http"
	"reflect"
	"strings"
	"time"
)

type assistantBriefReferenceRequest struct {
	Subject string `json:"subject"`
	Token   string `json:"token"`
}
type assistantBriefReferenceReply struct {
	Start assistantRunStart `json:"start"`
	Run   *assistantRunView `json:"run,omitempty"`
}

var errAssistantBriefVerification = errors.New("Brief source verification is required before committing.")

func (s *Server) registerAssistantBriefRuns() {
	s.mux.HandleFunc("POST /api/assistant-briefs/reference", s.handleAssistantBriefReference)
	s.mux.HandleFunc("POST /api/assistant-runs/{run}/brief", s.handleAssistantBriefCommit)
	s.mux.HandleFunc("GET /api/assistant-runs/{run}/brief", s.handleAssistantBriefCommit)
}

// Getting a reference is model-free. Reconnection may inspect an existing run,
// but can never allocate a replacement request after uncertain delivery.
func (s *Server) handleAssistantBriefReference(w http.ResponseWriter, r *http.Request) {
	if !s.guardAssistantRun(w, r) {
		return
	}
	var request assistantBriefReferenceRequest
	if assistantRunRequest(r, &request) != nil || !assistantBriefSubject(request.Subject) || !assistantRunText(request.Token, 128) {
		assistantRunHTTPError(w, errAssistantRunInvalid)
		return
	}
	identity, _, err := s.assistantRunIdentity(r)
	if err != nil {
		assistantRunHTTPError(w, err)
		return
	}
	admission := "brief-" + request.Token
	if run, lookupErr := s.assistantRuns().lookup(r.Context(), identity, "", admission, true); lookupErr == nil {
		if run.Binding.Owner.Kind != "brief" || run.Binding.Owner.ID != request.Subject || run.Binding.RequestID != request.Token {
			assistantRunHTTPError(w, errAssistantRunScope)
			return
		}
		view := assistantRunPublic(run)
		writeJSON(w, 200, assistantBriefReferenceReply{Start: assistantRunStart{AdmissionID: admission, Purpose: "brief", OwnerID: request.Subject, Revision: run.Binding.Owner.Revision, RequestID: request.Token}, Run: &view})
		return
	} else if !errors.Is(lookupErr, errAssistantRunNotFound) {
		assistantRunHTTPError(w, lookupErr)
		return
	}
	lock, err := s.privateDataLockContext(r.Context(), true)
	if err != nil {
		assistantRunHTTPError(w, err)
		return
	}
	defer lock.Close()
	reply, err := s.assistantBriefRead(r, request.Subject)
	pending := reply.Content.Subjects[request.Subject].Pending
	if err != nil || pending == nil || pending.Token != request.Token {
		assistantRunHTTPError(w, errAssistantRunConflict)
		return
	}
	writeJSON(w, 200, assistantBriefReferenceReply{Start: assistantRunStart{AdmissionID: admission, Purpose: "brief", OwnerID: request.Subject, Revision: digestOf(pending.Snapshot), RequestID: pending.Token}})
}

func (s *Server) handleAssistantBriefCommit(w http.ResponseWriter, r *http.Request) {
	if !s.guardAssistantRun(w, r) {
		return
	}
	var request struct {
		Token string `json:"token"`
	}
	if r.Method == http.MethodPost && assistantRunRequest(r, &request) != nil || r.Method == http.MethodGet && r.ContentLength != 0 {
		assistantRunHTTPError(w, errAssistantRunInvalid)
		return
	}
	run, err := s.assistantRunService().read(r, r.PathValue("run"))
	if err != nil {
		assistantRunHTTPError(w, err)
		return
	}
	if err = s.authorizeAssistantRun(r, run); err != nil {
		assistantRunHTTPError(w, err)
		return
	}
	if r.Method == http.MethodGet {
		request.Token = run.Binding.RequestID
	}
	if run.Historical || run.State != "complete" || run.Binding.Owner.Kind != "brief" || request.Token != run.Binding.RequestID || run.AdmissionID != "brief-"+request.Token {
		assistantRunHTTPError(w, errAssistantRunConflict)
		return
	}
	text, err := assistantBriefOutput(run)
	if err != nil {
		assistantRunHTTPError(w, errAssistantRunInvalid)
		return
	}
	// The retained connection is supplied by the canonical run, never the
	// current browser selection. Current policy/profile checks still apply.
	held := r.Clone(r.Context())
	held.Header.Set(aiConnectionHeader, run.Binding.ConnectionID)
	held.Header.Set(aiRevisionHeader, run.Binding.ConnectionRevision)
	s.writes.Lock()
	defer s.writes.Unlock()
	lock, err := s.privateDataLockContext(r.Context(), true)
	if err != nil {
		assistantRunHTTPError(w, err)
		return
	}
	reply, err := s.commitAssistantBrief(held, run, text, nil, false, r.Method == http.MethodGet)
	_ = lock.Close()
	if errors.Is(err, errAssistantBriefVerification) {
		// Verification may run the existing offline verifier. It never holds the
		// private-data lock; its exact bytes are checked again in the final CAS.
		identity, _, identityErr := s.assistantRunIdentity(held)
		if identityErr != nil {
			assistantRunHTTPError(w, identityErr)
			return
		}
		start := assistantRunStart{AdmissionID: run.AdmissionID, Purpose: "brief", OwnerID: run.Binding.Owner.ID, Revision: run.Binding.Owner.Revision, RequestID: run.Binding.RequestID}
		source, sourceErr := s.readAssistantRunSource(held, start, identity, run.Binding.Project)
		if sourceErr != nil {
			assistantRunHTTPError(w, sourceErr)
			return
		}
		e := &assistantRunExecution{source: source}
		refs, refErr := e.assistantBriefReferences()
		if refErr != nil {
			assistantRunHTTPError(w, refErr)
			return
		}
		documents := []assistantVerifiedDocument{}
		for _, ref := range refs {
			document, verifyErr := s.readAssistantDocument(held, source, ref)
			if verifyErr != nil {
				assistantRunHTTPError(w, verifyErr)
				return
			}
			documents = append(documents, document)
		}
		lock, err = s.privateDataLockContext(r.Context(), true)
		if err != nil {
			assistantRunHTTPError(w, err)
			return
		}
		reply, err = s.commitAssistantBrief(held, run, text, documents, true, false)
		_ = lock.Close()
	}
	if err != nil {
		assistantRunHTTPError(w, err)
		return
	}
	writeJSON(w, 200, reply)
}

func assistantBriefSaved(reply assistantBriefReply, run assistantRunRecord, text BriefText) bool {
	for _, revision := range reply.Content.Subjects[run.Binding.Owner.ID].Revisions {
		if revision.ID == run.Binding.RequestID {
			return revision.Text == text && revision.Model == run.Binding.Model && digestOf(revision.Snapshot) == run.Binding.Owner.Revision
		}
	}
	return false
}

// Caller holds the project write fence and short private-data lock. First
// reconcile an already saved exact revision, even if its source later changed.
func (s *Server) commitAssistantBrief(r *http.Request, run assistantRunRecord, text BriefText, documents []assistantVerifiedDocument, verified, readOnly bool) (assistantBriefReply, error) {
	subject := run.Binding.Owner.ID
	var reply assistantBriefReply
	var name string
	var data *chatDataStore
	var err error
	if assistantBriefJob.MatchString(subject) {
		reply, err = s.assistantBriefRunner(r, subject, nil)
	} else {
		data, err = s.openChatData()
		if err != nil {
			return reply, err
		}
		defer data.root.Close()
		name, err = resolveConversationName(data.root, s.conversationIdentity())
		if err != nil {
			return reply, err
		}
		name = strings.Replace(name, "conversations-", "briefs-", 1)
		saved, readErr := s.readWorkspaceRecord(data.root, name, false)
		if readErr != nil {
			return reply, readErr
		}
		if json.Unmarshal(saved.Content, &reply.Content) != nil {
			return reply, errAssistantRunInvalid
		}
	}
	if err != nil {
		return reply, err
	}
	if assistantBriefSaved(reply, run, text) {
		return reply, nil
	}
	if readOnly {
		return reply, errAssistantRunConflict
	}
	identity, _, err := s.assistantRunIdentity(r)
	if err != nil || !assistantRunIdentityMatches(identity, run.Binding) {
		return reply, errAssistantRunScope
	}
	start := assistantRunStart{AdmissionID: run.AdmissionID, Purpose: "brief", OwnerID: subject, Revision: run.Binding.Owner.Revision, RequestID: run.Binding.RequestID}
	fresh, err := s.resolveAssistantBriefRun(r, start, identity, run.Binding.Project)
	if err != nil {
		return reply, err
	}
	fresh.Binding.ExecutionKey, fresh.Binding.Unit = run.Binding.ExecutionKey, run.Binding.Unit
	if !assistantRunFreshSourceEqual(fresh, assistantRunSource{Binding: run.Binding, Original: fresh.Original, Request: fresh.Request}) {
		return reply, errAssistantRunScope
	}
	refs, err := (&assistantRunExecution{source: fresh}).assistantBriefReferences()
	if err != nil {
		return reply, err
	}
	if len(refs) > 0 {
		if !verified {
			return reply, errAssistantBriefVerification
		}
		if data == nil || len(documents) != len(refs) {
			return reply, errAssistantDocument
		}
		conversation, err := resolveConversationName(data.root, s.conversationIdentity())
		if err != nil {
			return reply, err
		}
		pin, err := s.configuredResearch()
		if err != nil {
			return reply, err
		}
		for i, document := range documents {
			raw, err := readPrivateData(data.root, attachmentProjectPrefix(conversation)+refs[i].ID+".json", maxAttachmentObjectBytes)
			if err != nil || !reflect.DeepEqual(refs[i], document.reference) || digestOf(raw) != document.attachmentDigest || !reflect.DeepEqual(pin, document.gateway) {
				return reply, errAssistantDocument
			}
		}
	}
	command := BriefCommand{Action: "complete", Subject: subject, Token: run.Binding.RequestID, Text: text}
	if assistantBriefJob.MatchString(subject) {
		reply, err = s.assistantBriefRunner(r, subject, &command)
		if err != nil {
			return reply, err
		}
		readback, readErr := s.assistantBriefRunner(r, subject, nil)
		if readErr != nil || !assistantBriefSaved(readback, run, text) {
			return reply, errAssistantRunConflict
		}
		return readback, nil
	}
	if err := applyBrief(&reply.Content, command, time.Now()); err != nil {
		return reply, err
	}
	raw, err := json.Marshal(reply.Content)
	if err != nil || len(raw) > maxConversationBytes || validateBriefs(raw) != nil {
		return reply, errAssistantRunInvalid
	}
	if err = writePrivateData(data.root, name, raw); err != nil {
		return reply, err
	}
	readback, err := s.readWorkspaceRecord(data.root, name, false)
	if err != nil || json.Unmarshal(readback.Content, &reply.Content) != nil || !assistantBriefSaved(reply, run, text) {
		return reply, errAssistantRunConflict
	}
	return reply, nil
}
