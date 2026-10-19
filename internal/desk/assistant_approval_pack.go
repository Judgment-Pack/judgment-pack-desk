package desk

import (
	"bytes"
	"encoding/json"
	"errors"
)

func canonicalApprovalJSON(raw []byte) []byte {
	var value any
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	if decoder.Decode(&value) != nil {
		return nil
	}
	encoded, e := json.Marshal(value)
	if e != nil {
		return nil
	}
	return encoded
}
func approvalSuite(tests json.RawMessage, chat assistantApprovalChat) json.RawMessage {
	if chat.Pack == nil {
		return nil
	}
	var doc struct {
		Suites map[string]struct {
			Cases json.RawMessage `json:"cases"`
		} `json:"suites"`
	}
	if json.Unmarshal(tests, &doc) != nil {
		return nil
	}
	return doc.Suites[chat.Pack.ID].Cases
}
func approvalSuiteRevision(tests json.RawMessage, chat assistantApprovalChat) string {
	return digestOf(canonicalApprovalJSON(approvalSuite(tests, chat)))
}
func packApprovalTarget(s *Server, chat assistantApprovalChat) (string, string, error) {
	if chat.Pack == nil || chat.Pack.ID == "" {
		return "", "", errors.New("Choose a saved pack before allowing automatic updates.")
	}
	clean, e := wireRelativePath(chat.Pack.Path)
	return clean, chat.Pack.ID, e
}
func verifyPackApproval(_ *Server, chat assistantApprovalChat, content, revision string, tests json.RawMessage) error {
	refuse := func() error {
		return errors.New("Check the complete proposal and saved tests before saving automatically.")
	}
	type candidate struct {
		Text       string `json:"text"`
		Digest     string `json:"digest"`
		PackReview struct {
			Basis struct {
				PackID string `json:"packId"`
				Path   string `json:"path"`
				Text   string `json:"text"`
			} `json:"basis"`
			Check struct {
				DocumentDigest string `json:"documentDigest"`
				SuiteKey       string `json:"suiteKey"`
				Valid          bool   `json:"valid"`
				TotalCases     int    `json:"totalCases"`
				Report         struct {
					Status  string `json:"status"`
					Summary struct {
						Total      int `json:"total"`
						Passed     int `json:"passed"`
						Mismatched int `json:"mismatched"`
					} `json:"summary"`
					Packs []struct {
						Rows []struct {
							ID     string `json:"id"`
							Status string `json:"status"`
						} `json:"rows"`
						Coverage []struct {
							Status string `json:"status"`
						} `json:"coverage"`
					} `json:"packs"`
				} `json:"report"`
			} `json:"check"`
		} `json:"packReview"`
	}
	var state struct {
		Status     string            `json:"status"`
		Candidates []candidate       `json:"candidates"`
		Unknowns   []json.RawMessage `json:"unknowns"`
		Questions  []struct {
			Resolution json.RawMessage `json:"resolution"`
		} `json:"questions"`
	}
	if json.Unmarshal(chat.Checkpoint.State, &state) != nil || len(state.Candidates) == 0 || (state.Status != "complete" && state.Status != "ready") {
		return refuse()
	}
	if len(state.Unknowns) > 0 {
		return errors.New("Review open questions before saving automatically.")
	}
	for _, q := range state.Questions {
		if len(q.Resolution) == 0 || string(q.Resolution) == "null" {
			return errors.New("Answer the open question before saving automatically.")
		}
	}
	c := state.Candidates[len(state.Candidates)-1]
	b := c.PackReview.Basis
	check := c.PackReview.Check
	if b.PackID != chat.Pack.ID || b.Path != chat.Pack.Path || digestOf([]byte(b.Text)) != revision || c.Digest != digestOf([]byte(c.Text)) || check.DocumentDigest != c.Digest || !check.Valid {
		return refuse()
	}
	proposed := canonicalApprovalJSON([]byte(c.Text))
	if proposed == nil || !bytes.Equal(proposed, canonicalApprovalJSON([]byte(content))) {
		return errors.New("The proposed document changed. Check it again before saving automatically.")
	}
	cases := approvalSuite(tests, chat)
	if !bytes.Equal(canonicalApprovalJSON(cases), canonicalApprovalJSON([]byte(check.SuiteKey))) {
		return errors.New("The saved tests changed. Check the proposal again.")
	}
	var rows []struct {
		ID string `json:"id"`
	}
	if json.Unmarshal(cases, &rows) != nil || len(rows) == 0 || check.TotalCases != len(rows) {
		return refuse()
	}
	report := check.Report
	if report.Status != "passed" || report.Summary.Total != len(rows) || report.Summary.Passed != len(rows) || report.Summary.Mismatched != 0 || len(report.Packs) != 1 || len(report.Packs[0].Rows) != len(rows) {
		return refuse()
	}
	for i, row := range report.Packs[0].Rows {
		if row.Status != "passed" || row.ID != rows[i].ID {
			return refuse()
		}
	}

	return nil
}
