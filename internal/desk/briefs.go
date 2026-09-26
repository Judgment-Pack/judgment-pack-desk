package desk

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"os"
	"strings"
	"time"
)

func (s *Server) changeBrief(root *os.Root, name string, current, request []byte) ([]byte, error) {
	var c BriefCommand
	decoder := json.NewDecoder(bytes.NewReader(request))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&c) != nil || decoder.Decode(new(any)) != io.EOF {
		return nil, errors.New("Invalid brief request.")
	}
	if c.Project != "" && c.Project != s.projectDir {
		return nil, errors.New("The project changed. Reopen the brief before continuing.")
	}
	var d BriefStore
	if err := json.Unmarshal(current, &d); err != nil {
		return nil, err
	}
	if c.Action == "begin" {
		if c.Owner == "" || c.CaseID == "" || c.Subject != "case:"+c.Owner+":"+c.CaseID {
			return nil, errors.New("Choose a saved test case.")
		}
		tests, err := s.readWorkspaceRecord(root, strings.Replace(name, "briefs-", "pack-tests-", 1), false)
		if err != nil {
			return nil, err
		}
		var store struct {
			Suites map[string]struct {
				Cases []json.RawMessage `json:"cases"`
			} `json:"suites"`
		}
		if json.Unmarshal(tests.Content, &store) != nil {
			return nil, errors.New("Cannot read test cases.")
		}
		var snapshot struct {
			Kind   string `json:"kind"`
			Pack   string `json:"pack"`
			Record struct {
				Case json.RawMessage `json:"case"`
			} `json:"record"`
		}
		if json.Unmarshal(c.Snapshot, &snapshot) != nil || snapshot.Kind != "case" || !json.Valid([]byte(snapshot.Pack)) {
			return nil, errors.New("Invalid case snapshot.")
		}
		found := false
		for _, entry := range store.Suites[c.Owner].Cases {
			var x struct {
				ID string `json:"id"`
			}
			_ = json.Unmarshal(entry, &x)
			if x.ID == c.CaseID && sameBriefJSON(entry, snapshot.Record.Case) {
				found = true
			}
		}
		if !found {
			return nil, errors.New("The saved case changed or was deleted. Reload before generating a brief.")
		}
	}
	if err := applyBrief(&d, c, time.Now()); err != nil {
		return nil, err
	}
	return json.Marshal(d)
}
