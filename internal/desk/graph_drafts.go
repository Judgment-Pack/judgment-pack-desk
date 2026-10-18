package desk

// Graph drafts are independent private workspace artifacts. Conversation copies
// remain readable for old clients, but deleting a chat never deletes its graph.
import (
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"regexp"
	"strings"
	"time"
)

var graphDraftFileName = regexp.MustCompile(`^draft-graphs-[a-f0-9]{64}\.json$`)
var graphDraftKey = regexp.MustCompile(`^[a-zA-Z0-9-]{1,80}$`)
var graphDraftDeletedKey = regexp.MustCompile(`^[a-zA-Z0-9-]{0,80}:[a-zA-Z0-9-]{1,80}$`)
var graphDraftGraphID = regexp.MustCompile(`^[a-z][a-z0-9]*(?:-[a-z0-9]+)*$`)

type graphDraft struct {
	DraftID       string                  `json:"draftId"`
	ID            string                  `json:"id"`
	Path          string                  `json:"path"`
	Content       string                  `json:"content"`
	Description   string                  `json:"description,omitempty"`
	BaseSHA256    string                  `json:"baseSha256,omitempty"`
	CreatedAt     string                  `json:"createdAt"`
	Saved         bool                    `json:"saved,omitempty"`
	WritePending  bool                    `json:"writePending,omitempty"`
	OriginChatID  string                  `json:"originChatId,omitempty"`
	PolicySources []policyAuthoringSource `json:"policySources,omitempty"`
}
type graphDraftDocument struct {
	Version int          `json:"version"`
	Drafts  []graphDraft `json:"drafts"`
	Deleted []string     `json:"deleted"`
}

func graphDraftIdentity(d graphDraft) string { return d.OriginChatID + ":" + d.DraftID }
func graphDraftName(conversation string) string {
	return strings.Replace(conversation, "conversations-", "draft-graphs-", 1)
}
func graphPolicyWorkspace(raw []byte) ([]byte, error) {
	var doc struct {
		Drafts []json.RawMessage `json:"drafts"`
	}
	if err := json.Unmarshal(raw, &doc); err != nil {
		return nil, err
	}
	entries := []map[string]any{}
	for _, encoded := range doc.Drafts {
		var draft graphDraft
		var fields map[string]json.RawMessage
		if json.Unmarshal(encoded, &draft) != nil || json.Unmarshal(encoded, &fields) != nil {
			return nil, errors.New("Invalid graph draft source.")
		}
		entry := map[string]any{"id": graphDraftIdentity(draft)}
		if sources, present := fields["policySources"]; present {
			entry["policySources"] = sources
		}
		entries = append(entries, entry)
	}
	return json.Marshal(map[string]any{"version": 1, "drafts": entries})
}
func validateGraphDrafts(raw []byte) error {
	var doc graphDraftDocument
	bad := errors.New("Graph drafts must retain unique identities, exact sources and bounded graph documents.")
	if !validUTF8(raw) || decodeDataJSON(raw, &doc) != nil || doc.Version != 1 || doc.Drafts == nil || doc.Deleted == nil || len(doc.Drafts) > 256 || len(doc.Deleted) > 4096 {
		return bad
	}
	var fields map[string]json.RawMessage
	_ = json.Unmarshal(raw, &fields)
	for key := range fields {
		if !contains([]string{"version", "drafts", "deleted"}, key) {
			return bad
		}
	}
	var rows []map[string]json.RawMessage
	if json.Unmarshal(fields["drafts"], &rows) != nil {
		return bad
	}
	for _, row := range rows {
		for key, value := range row {
			if !contains([]string{"draftId", "id", "path", "content", "description", "baseSha256", "createdAt", "saved", "writePending", "originChatId", "policySources"}, key) || string(value) == "null" {
				return bad
			}
		}
		for _, key := range []string{"draftId", "id", "path", "content", "createdAt"} {
			if len(row[key]) == 0 {
				return bad
			}
		}
		if value, present := row["baseSha256"]; present {
			var digest string
			if json.Unmarshal(value, &digest) != nil || !retainedArtifactDigest.MatchString(digest) {
				return bad
			}
		}
	}
	seen := map[string]bool{}
	for _, id := range doc.Deleted {
		if !graphDraftDeletedKey.MatchString(id) || seen[id] {
			return bad
		}
		seen[id] = true
	}
	for _, d := range doc.Drafts {
		if d.Saved && d.WritePending || !graphDraftKey.MatchString(d.DraftID) || seen[graphDraftIdentity(d)] || !graphDraftGraphID.MatchString(d.ID) || len(d.ID) > 128 || d.Path == "" || len(d.Path) > 1024 || len(d.Content) > 1<<20 || len(d.Description) > 4096 || d.BaseSHA256 != "" && !retainedArtifactDigest.MatchString(d.BaseSHA256) || !graphDraftKey.MatchString(d.OriginChatID) {
			return bad
		}
		if _, err := time.Parse(time.RFC3339Nano, d.CreatedAt); err != nil || !policyInterpretationTimeForm.MatchString(d.CreatedAt) {
			return bad
		}
		seen[graphDraftIdentity(d)] = true
	}
	workspace, err := graphPolicyWorkspace(raw)
	if err != nil {
		return err
	}
	_, err = policyWorkspaceEntries(workspace, true)
	return err
}
func (s *Server) admitGraphDraftUpdate(r *http.Request, previous, incoming []byte) error {
	var before, after graphDraftDocument
	if json.Unmarshal(previous, &before) != nil || json.Unmarshal(incoming, &after) != nil {
		return withCode(CodeBadRequest, errors.New("Invalid graph drafts."))
	}
	prior := map[string]graphDraft{}
	deleted := map[string]bool{}
	for _, id := range before.Deleted {
		deleted[id] = true
	}
	for _, d := range before.Drafts {
		prior[graphDraftIdentity(d)] = d
	}
	for _, id := range after.Deleted {
		if old, ok := prior[id]; ok && (old.Saved || old.WritePending) {
			return withCode(CodeForbidden, errors.New("Saved graph basis cannot be deleted as a draft."))
		}
		delete(prior, id)
	}
	for _, d := range after.Drafts {
		if deleted[graphDraftIdentity(d)] {
			return withCode(CodeStale, errors.New("This graph draft was deleted."))
		}
		if old, ok := prior[graphDraftIdentity(d)]; ok {
			if !sameDraftValue(old.PolicySources, d.PolicySources) || old.CreatedAt != d.CreatedAt || old.OriginChatID != d.OriginChatID || old.Saved && !sameDraftValue(old, d) {
				return withCode(CodeForbidden, errors.New("Retained graph identity, ownership and saved history cannot be replaced."))
			}
			if old.WritePending {
				frozen, next := old, d
				frozen.WritePending = false
				next.WritePending = false
				next.Saved = false
				if !sameDraftValue(frozen, next) || !d.WritePending && !d.Saved {
					return withCode(CodeForbidden, errors.New("Reconcile the exact pending graph save before changing its retained candidate."))
				}
				if d.Saved {
					file, config := s.readProjectFile(d.Path), s.readProjectFile(runtimeConfigName)
					var declarations struct {
						Graphs map[string]struct {
							Path        string `json:"path"`
							Description string `json:"description"`
						} `json:"graphs"`
					}
					if file.err != nil || !file.present || string(file.data) != d.Content || config.err != nil || !config.present || json.Unmarshal(config.data, &declarations) != nil {
						return withCode(CodeStale, errors.New("The exact saved graph and registration could not be reconciled; its private history is retained."))
					}
					entry, exists := declarations.Graphs[d.ID]
					if !exists || entry.Path != d.Path || entry.Description != d.Description {
						return withCode(CodeStale, errors.New("The saved graph registration changed; preserve the pending candidate and review the project files."))
					}
				}
			}
			delete(prior, graphDraftIdentity(d))
		}
	}
	if len(prior) > 0 {
		return withCode(CodeBadRequest, errors.New("Delete graph drafts explicitly using their retained identities."))
	}
	for _, id := range before.Deleted {
		found := false
		for _, held := range after.Deleted {
			found = found || held == id
		}
		if !found {
			return withCode(CodeForbidden, errors.New("Keep deleted graph identities to prevent stale restoration."))
		}
	}
	a, err := graphPolicyWorkspace(previous)
	if err != nil {
		return err
	}
	b, err := graphPolicyWorkspace(incoming)
	if err != nil {
		return err
	}
	return s.admitPolicyWorkspaceUpdate(r, a, b, true)
}
func (s *Server) handleGraphDrafts(w http.ResponseWriter, r *http.Request) {
	s.handleWorkspaceRecord(w, r, false)
}

// Conversation mirrors contain proposals only. Their owning chat's top-level
// source list is the sole private provenance boundary for historical snapshots.
func validateConversationGraphSnapshots(raw []byte) error {
	var fields map[string]json.RawMessage
	if json.Unmarshal(raw, &fields) != nil {
		return errors.New("Invalid conversation graph snapshots.")
	}
	for key := range fields {
		if strings.EqualFold(key, "graphDrafts") && key != "graphDrafts" {
			return errors.New("Use exact graphDrafts field names.")
		}
	}
	encoded, present := fields["graphDrafts"]
	if !present {
		return nil
	}
	var drafts []map[string]json.RawMessage
	if json.Unmarshal(encoded, &drafts) != nil || drafts == nil || len(drafts) > 16 {
		return errors.New("Keep at most sixteen graph proposal snapshots in a conversation.")
	}
	for _, draft := range drafts {
		for key := range draft {
			if strings.EqualFold(key, "policySources") || strings.EqualFold(key, "originChatId") || strings.EqualFold(key, "writePending") {
				return errors.New("Keep private graph provenance in the independent record and owning conversation source list.")
			}
		}
	}
	return nil
}

// Caller holds the shared workspace lock. Retention is completed before a
// conversation deletion can commit. Existing independent edits win over an
// unchanged chat snapshot. Chat snapshots never update independent entries.
func (s *Server) retainConversationGraphDrafts(r *http.Request, root *os.Root, name string, documents ...[]byte) error {
	saved, err := s.readWorkspaceRecord(root, graphDraftName(name), false)
	if err != nil {
		return err
	}
	var doc graphDraftDocument
	if json.Unmarshal(saved.Content, &doc) != nil {
		return errors.New("Invalid retained graph drafts.")
	}
	indices := map[string]int{}
	deleted := map[string]bool{}
	for i, d := range doc.Drafts {
		indices[graphDraftIdentity(d)] = i
	}
	for _, id := range doc.Deleted {
		deleted[id] = true
	}
	changed := false
	candidates := map[string]graphDraft{}
	order := []string{}
	for _, raw := range documents {
		var chats struct {
			Chats []struct {
				ID      string                  `json:"id"`
				Drafts  []graphDraft            `json:"graphDrafts"`
				Sources []policyAuthoringSource `json:"policySources"`
			} `json:"chats"`
		}
		if json.Unmarshal(raw, &chats) != nil {
			return errors.New("Invalid conversation graph drafts.")
		}
		seen := map[string]bool{}
		for _, chat := range chats.Chats {
			for _, d := range chat.Drafts {
				d.OriginChatID = chat.ID
				d.PolicySources = chat.Sources
				key := graphDraftIdentity(d)
				if seen[key] {
					return withCode(CodeBadRequest, errors.New("Duplicate graph draft identity in a conversation."))
				}
				seen[key] = true
				if deleted[key] {
					continue
				}
				if _, exists := candidates[key]; !exists {
					order = append(order, key)
				}
				// The incoming snapshot wins on the first migration. If deletion
				// removes it from incoming, retain the previous snapshot instead.
				candidates[key] = d
			}
		}
	}
	for _, key := range order {
		// Already-independent edits always win over both chat snapshots.
		if _, exists := indices[key]; !exists {
			indices[key] = len(doc.Drafts)
			doc.Drafts = append(doc.Drafts, candidates[key])
			changed = true
		}
	}
	if !changed {
		return nil
	}
	if err = s.authorizePrivateWorkspace(r); err != nil {
		return err
	}
	raw, err := json.Marshal(doc)
	if err != nil {
		return err
	}
	if len(raw) > maxConversationBytes {
		return withCode(CodeTooLarge, errors.New("Graph draft storage is full."))
	}
	if err = validateGraphDrafts(raw); err != nil {
		return err
	}
	if err = s.admitGraphDraftUpdate(r, saved.Content, raw); err != nil {
		return err
	}
	stage, temp, err := newDataStage(root)
	if err != nil {
		return err
	}
	defer root.Remove(temp)
	defer stage.Close()
	if _, err = stage.Write(raw); err != nil {
		return err
	}
	if err = stage.Chmod(custodyFileMode); err != nil {
		return err
	}
	if err = stage.Sync(); err != nil {
		return err
	}
	if err = stage.Close(); err != nil {
		return err
	}
	if err = s.authorizePrivateWorkspace(r); err != nil {
		return err
	}
	if err = root.Rename(temp, graphDraftName(name)); err != nil {
		return err
	}
	if directory, e := root.Open("."); e == nil {
		_ = directory.Sync()
		_ = directory.Close()
	}
	verified, err := s.readWorkspaceRecord(root, graphDraftName(name), false)
	if err != nil {
		return err
	}
	if verified.SHA256 != digestOf(raw) {
		return withCode(CodeStale, errors.New("Graph draft storage changed after retention."))
	}
	return nil
}
