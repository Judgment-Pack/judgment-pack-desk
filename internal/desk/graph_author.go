package desk

// Graph authoring keeps the proposal as the only assistant output (ADR-0011,
// section 5). Only the owner's confirmation reaches the guarded writer.
import (
	"bytes"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"path/filepath"
	"regexp"
	"strings"
)

type graphProposal struct {
	ID          string `json:"id"`
	Path        string `json:"path"`
	Content     string `json:"content"`
	Description string `json:"description,omitempty"`
	BaseSHA256  string `json:"baseSha256,omitempty"`
}

type graphWriteOffer struct {
	graphProposal
	Before        string `json:"before"`
	ConfigContent string `json:"configContent"`
	ConfigSHA256  string `json:"configSha256"`
	Findings      string `json:"findings"`
	Plan          string `json:"plan"`
	HasLock       bool   `json:"hasLock"`
	Nonce         string `json:"nonce"`
	Token         string `json:"token"`
}

// The nonce makes a fresh review distinct even when its bytes are unchanged.
// The MAC's purpose, desk and project keep other confirmation tokens separate.
func (s *Server) graphWriteToken(offer graphWriteOffer) string {
	offer.Token = ""
	payload, _ := json.Marshal(offer)
	mac := hmac.New(sha256.New, s.reviewKey[:])
	mac.Write([]byte("graph-write\x00" + s.cfg.deskID + "\x00" + s.projectDir + "\x00"))
	mac.Write(payload)
	return hex.EncodeToString(mac.Sum(nil))
}

func graphRequest(w http.ResponseWriter, r *http.Request, into any) bool {
	data, err := io.ReadAll(io.LimitReader(r.Body, 32*maxFileBytes+1))
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields() // in particular, there is no override input
	if err != nil || len(data) > 32*maxFileBytes || decoder.Decode(into) != nil || decoder.Decode(new(any)) != io.EOF {
		writeJSONCoded(w, http.StatusBadRequest, CodeBadRequest, "The graph request could not be read.")
		return false
	}
	return true
}

func (s *Server) handleGraphProposalCheck(w http.ResponseWriter, r *http.Request) {
	if !s.guard(w, r) {
		return
	}
	var request struct {
		Content string `json:"content"`
	}
	if !graphRequest(w, r, &request) {
		return
	}
	if len(request.Content) > maxFileBytes {
		writeJSONCoded(w, http.StatusRequestEntityTooLarge, CodeTooLarge, "The graph exceeds the file writer's limit.")
		return
	}
	dir, refusal := s.graphRuntime()
	if refusal != "" {
		writeJSONCoded(w, http.StatusConflict, CodeBadRequest, refusal)
		return
	}
	command := "validate"
	if strings.HasSuffix(r.URL.Path, "/explain") {
		command = "explain"
	}
	answer, _, err := s.runGraphInput(r.Context(), dir, command, []byte(request.Content), "-")
	if err != nil {
		s.graphFailed(w, "The proposal could not be checked", err)
		return
	}
	s.writeGraphAnswer(w, "", answer)
}

var graphDeclarationID = regexp.MustCompile(`^[a-z][a-z0-9]*(?:-[a-z0-9]+)*$`)

// addGraphMember preserves every existing byte and member order, including
// numbers a JavaScript round trip would round. No configuration version moves.
func addGraphMember(data []byte, name string, value []byte) ([]byte, error) {
	members, err := configMembers(data)
	if err != nil {
		return nil, err
	}
	for _, m := range members {
		if m.name == name {
			return nil, errors.New("That graph id is already declared.")
		}
	}
	at, text := 1, string(mustMarshal(name))+":"+string(value)
	if len(members) > 0 {
		last := members[len(members)-1]
		at = last.end
		text = "," + last.sep + string(mustMarshal(name)) + last.colon + string(value)
	}
	out := append([]byte{}, data[:at]...)
	out = append(out, []byte(text)...)
	out = append(out, data[at:]...)
	return out, nil
}

func graphDeclaration(config []byte, request graphProposal) ([]byte, error) {
	members, err := configMembers(config)
	if err != nil {
		return nil, err
	}
	var version string
	var graphs *configMember
	for i, m := range members {
		if m.name == "configVersion" {
			_ = json.Unmarshal(config[m.start:m.end], &version)
		}
		if m.name == "graphs" {
			graphs = &members[i]
		}
	}
	if version == "" || version == "1" {
		return nil, errors.New("Graphs need configVersion 2 or later. The upgrade offer moves it to 5, turns on requireReviewed and the audit trail, and makes the first Review and lock; review that offer to change the version.")
	}
	// A graph proposal never takes a path already assigned to a pack, rows,
	// configuration, or another graph, including a declared file not yet there.
	var reserved struct {
		Packs map[string]struct {
			Path   string `json:"path"`
			Matrix string `json:"matrix"`
		} `json:"packs"`
	}
	if json.Unmarshal(config, &reserved) != nil {
		return nil, errors.New("The project declarations could not be read.")
	}
	samePath := func(path string) bool { return path != "" && filepath.Clean(path) == filepath.Clean(request.Path) }
	if samePath(runtimeConfigName) || samePath(runtimeLockName) {
		return nil, errors.New("Choose a path for the graph document alone.")
	}
	for _, pack := range reserved.Packs {
		if samePath(pack.Path) || samePath(pack.Matrix) {
			return nil, errors.New("That path is already declared for a pack or its rows.")
		}
	}
	var entries map[string]struct {
		Path string `json:"path"`
		Rows string `json:"rows"`
	}
	if graphs != nil {
		if _, err := configMembers(config[graphs.start:graphs.end]); err != nil {
			return nil, err
		}
		if json.Unmarshal(config[graphs.start:graphs.end], &entries) != nil {
			return nil, errors.New("The graph declarations could not be read.")
		}
	}
	for id, entry := range entries {
		if samePath(entry.Rows) || (samePath(entry.Path) && (request.BaseSHA256 == "" || id != request.ID)) {
			return nil, errors.New("That path is already declared for a graph or its rows.")
		}
	}
	if request.BaseSHA256 != "" {
		entry, ok := entries[request.ID]
		if !ok || entry.Path != request.Path {
			return nil, errors.New("The edit must name the declared graph's path.")
		}
		return config, nil
	}
	entry := map[string]string{"path": request.Path}
	if request.Description != "" {
		entry["description"] = request.Description
	}
	value, _ := json.Marshal(entry)
	if graphs == nil {
		value, err = addGraphMember([]byte("{}"), request.ID, value)
		if err != nil {
			return nil, err
		}
		return addGraphMember(config, "graphs", value)
	}
	value, err = addGraphMember(config[graphs.start:graphs.end], request.ID, value)
	if err != nil {
		return nil, err
	}
	out := append([]byte{}, config[:graphs.start]...)
	out = append(out, value...)
	out = append(out, config[graphs.end:]...)
	return out, nil
}

func (s *Server) handleGraphProposal(w http.ResponseWriter, r *http.Request) {
	if !s.guard(w, r) {
		return
	}
	var request graphProposal
	if !graphRequest(w, r, &request) {
		return
	}
	clean, err := wireRelativePath(request.Path)
	if err != nil || clean != request.Path || !graphDeclarationID.MatchString(request.ID) || len(request.Content) > maxFileBytes || !json.Valid([]byte(request.Content)) {
		writeJSONCoded(w, http.StatusBadRequest, CodeBadRequest, "Name a graph id, a project-relative path and a JSON graph within the file writer's limit.")
		return
	}
	dir, refusal := s.graphRuntime()
	if refusal != "" {
		writeJSONCoded(w, http.StatusConflict, CodeBadRequest, refusal)
		return
	}
	offer, status, body := func() (*graphWriteOffer, int, any) {
		unlock, failure := s.lockProjectFor(r.Context(), "a graph proposal", "written")
		if failure != nil {
			return nil, failure.status, errorBody(withCode(failure.code, errors.New(failure.message)))
		}
		defer unlock()
		s.writes.Lock()
		defer s.writes.Unlock()
		config := s.readProjectFile(runtimeConfigName)
		if config.err != nil || !config.present {
			return nil, http.StatusConflict, errorBody(withCode(CodeBadRequest, errors.New("The project's jpack.json could not be read.")))
		}
		next, err := graphDeclaration(config.data, request)
		if err != nil {
			return nil, http.StatusConflict, errorBody(withCode(CodeBadRequest, err))
		}
		if len(next) > maxFileBytes {
			return nil, http.StatusRequestEntityTooLarge, errorBody(withCode(CodeTooLarge, errors.New("The declaration exceeds the file writer's limit.")))
		}
		findings, _, err := s.runGraphInput(r.Context(), dir, "validate", []byte(request.Content), "-")
		if err != nil {
			return nil, http.StatusConflict, errorBody(withCode(CodeBadRequest, errors.New(s.withoutPaths(err.Error()))))
		}
		// The runtime's digest must bind the findings to exactly the shown bytes.
		var binding struct {
			GraphSHA256 string `json:"graphSha256"`
		}
		_ = json.Unmarshal(findings, &binding)
		if binding.GraphSHA256 != digestOf([]byte(request.Content)) {
			return nil, http.StatusConflict, errorBody(withCode(CodeBadRequest, errors.New("The runtime's findings did not name this proposal's digest.")))
		}
		plan, _, err := s.runGraphInput(r.Context(), dir, "explain", []byte(request.Content), "-")
		if err != nil {
			return nil, http.StatusConflict, errorBody(withCode(CodeBadRequest, errors.New(s.withoutPaths(err.Error()))))
		}
		nonce, err := NewToken()
		if err != nil {
			return nil, http.StatusInternalServerError, errorBody(err)
		}
		_, lockErr := s.root.Lstat(runtimeLockName)
		offer := &graphWriteOffer{graphProposal: request, Before: string(config.data), ConfigContent: string(next), ConfigSHA256: digestOf(config.data), Findings: string(s.shownGraphAnswer(findings)), Plan: string(s.shownGraphAnswer(plan)), HasLock: lockErr == nil, Nonce: nonce}
		offer.Token = s.graphWriteToken(*offer)
		s.graphOffer = offer
		return offer, http.StatusOK, nil
	}()
	w.Header().Set("Cache-Control", "no-store")
	if offer != nil {
		writeJSON(w, status, offer)
	} else {
		writeJSON(w, status, body)
	}
}

func (s *Server) handleGraphWrite(w http.ResponseWriter, r *http.Request) {
	if !s.guard(w, r) {
		return
	}
	var request graphWriteOffer
	if !graphRequest(w, r, &request) {
		return
	}
	status, body := s.writeGraph(r, request)
	writeJSON(w, status, body)
}

func (s *Server) writeGraph(r *http.Request, request graphWriteOffer) (int, any) {
	// Spend before attempting the folder lock, so even a refused use is spent.
	s.writes.Lock()
	offer := s.graphOffer
	s.graphOffer = nil
	s.writes.Unlock()
	stale := func() (int, any) {
		return http.StatusConflict, errorBody(withCode(CodeStale, errors.New("The graph confirmation is spent or changed. Review the proposal again; nothing was written.")))
	}
	if offer == nil || !hmac.Equal([]byte(request.Token), []byte(offer.Token)) || !hmac.Equal([]byte(s.graphWriteToken(request)), []byte(offer.Token)) {
		return stale()
	}
	unlock, failure := s.lockProjectFor(r.Context(), "a graph write", "written")
	if failure != nil {
		return failure.status, errorBody(withCode(failure.code, errors.New(failure.message)))
	}
	defer unlock()
	s.writes.Lock()
	defer s.writes.Unlock()
	config := s.readProjectFile(runtimeConfigName)
	if config.err != nil || !config.present || digestOf(config.data) != offer.ConfigSHA256 {
		return stale()
	}
	status, body := s.commitWriteLocked(offer.Path, WriteRequest{Path: offer.Path, Content: offer.Content, BaseSHA256: offer.BaseSHA256, Override: false})
	if status != http.StatusOK && status != http.StatusCreated {
		return status, body
	}
	if offer.BaseSHA256 == "" {
		configStatus, configBody := s.commitWriteLocked(runtimeConfigName, WriteRequest{Path: runtimeConfigName, Content: offer.ConfigContent, BaseSHA256: offer.ConfigSHA256, Override: false})
		if configStatus != http.StatusOK {
			return configStatus, map[string]any{"graphWritten": true, "declared": false, "error": "The graph document was written and not declared. Review jpack.json and add the declaration before using this graph.", "cause": configBody}
		}
	}
	return http.StatusOK, map[string]any{"graphWritten": true, "declared": true, "file": body}
}
