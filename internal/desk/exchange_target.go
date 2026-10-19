package desk

import (
	"context"
	"encoding/json"
	"errors"
	exchangeCatalog "github.com/Judgment-Pack/judgment-pack-desk/internal/exchange"
	"net/http"
	"os"
	"slices"
	"strings"
)

func (s *Server) exchangeCandidate(ctx context.Context, id string) (exchangeCatalog.Candidate, error) {
	if !retainedArtifactDigest.MatchString(id) {
		return exchangeCatalog.Candidate{}, errors.New("Choose a retained template import")
	}
	root, err := s.exchangeRoot()
	if err != nil {
		return exchangeCatalog.Candidate{}, err
	}
	defer root.Close()
	history, err := readExchangeHistory(root)
	if err != nil {
		return exchangeCatalog.Candidate{}, err
	}
	for _, entry := range history.Imports {
		if entry.ID == id {
			_, candidate, err := s.readExchangeImport(ctx, entry)
			return candidate, err
		}
	}
	return exchangeCatalog.Candidate{}, errors.New("Imported template is unavailable")
}
func (s *Server) checkExchangeTarget(ctx context.Context, id, pack string, document []byte) error {
	candidate, err := s.exchangeCandidate(ctx, id)
	if err != nil {
		return err
	}
	_, checked, err := s.checkDialectAuthoring(ctx, document, pack)
	if err != nil {
		return err
	}
	if !checked.accepted(true) || checked.Digest != candidate.Digest {
		return errors.New("The imported definition or its pack coverage changed. Review a new template configuration")
	}
	target := candidate.Entry.Pack
	if target == nil {
		return nil
	}
	var identity struct{ ID, Version string }
	if rejectDuplicateMembers([]byte(pack)) != nil || json.Unmarshal([]byte(pack), &identity) != nil || identity.ID != target.ID {
		return errors.New("Choose the pack declared by this template")
	}
	if target.Digests != nil {
		if !slices.Contains(target.Digests, "sha256:"+digestOf([]byte(pack))) {
			return errors.New("The saved pack bytes do not match this template's declared digests")
		}
		return nil
	}
	return s.checkExchangePackRange(ctx, identity.Version, target.Versions)
}

// Exchange keeps a publisher's range as text. Desk supports an intersection of
// explicit comparison clauses; Runtime alone parses and compares each version.
func (s *Server) checkExchangePackRange(ctx context.Context, version, expression string) error {
	clauses := strings.Fields(expression)
	if len(clauses) == 0 || len(clauses) > 4 {
		return errors.New("Unsupported template pack range; use explicit version comparisons or pack digests")
	}
	where, err := os.MkdirTemp("", "jpack-exchange-version-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(where)
	file, err := os.Open(where)
	if err != nil {
		return err
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return err
	}
	dir := heldDir{file: file, path: where, info: info}
	for _, clause := range clauses {
		op, base := "=", clause
		for _, prefix := range []string{">=", "<=", ">", "<", "="} {
			if strings.HasPrefix(clause, prefix) {
				op, base = prefix, strings.TrimPrefix(clause, prefix)
				break
			}
		}
		reply, err := s.artifactVersionComparison(ctx, dir, base, version)
		if err != nil {
			return errors.New("Runtime cannot read this template's pack version range")
		}
		pass := op == "=" && reply.Comparison == "equal" || op == ">=" && reply.Comparison != "lower" || op == "<=" && reply.Comparison != "greater" || op == ">" && reply.Comparison == "greater" || op == "<" && reply.Comparison == "lower"
		if !pass {
			return errors.New("The saved pack version is outside this template's declared range")
		}
	}
	return nil
}

func (s *Server) handleExchangeTarget(w http.ResponseWriter, r *http.Request) {
	if !s.guard(w, r) || s.refuseUnusableStore(w) {
		return
	}
	var request struct {
		Pack     string
		Document string
	}
	if inputRequest(r, &request) != nil || request.Pack == "" || request.Document == "" {
		writeJSONCoded(w, 400, CodeBadRequest, "Choose an imported definition and saved pack")
		return
	}
	if err := s.checkExchangeTarget(r.Context(), r.PathValue("importId"), request.Pack, []byte(request.Document)); err != nil {
		writeJSONCoded(w, 422, CodeBadRequest, err.Error())
		return
	}
	writeJSON(w, 200, map[string]any{"status": "checked"})
}

// The import identity belongs to Desk, never Runner's execution schema. The
// authoritative check repeats on the exact preview bytes before forwarding.
func (s *Server) checkExchangePreview(ctx context.Context, raw []byte) ([]byte, error) {
	var fields map[string]json.RawMessage
	if json.Unmarshal(raw, &fields) != nil {
		return raw, nil
	} // Runner reports malformed generic requests.
	origin, exists := fields["exchangeImportId"]
	if !exists {
		return raw, nil
	}
	if rejectDuplicateMembers(raw) != nil {
		return nil, errors.New("Invalid imported preview")
	}
	var id, pack string
	if json.Unmarshal(origin, &id) != nil || json.Unmarshal(fields["pack"], &pack) != nil {
		return nil, errors.New("Invalid imported preview")
	}
	var input struct {
		Source struct {
			Mapping json.RawMessage
			Import  *struct{ Dialect json.RawMessage }
		}
	}
	if json.Unmarshal(fields["input"], &input) != nil {
		return nil, errors.New("Invalid imported input")
	}
	document := input.Source.Mapping
	if input.Source.Import != nil {
		document = input.Source.Import.Dialect
	}
	if err := s.checkExchangeTarget(ctx, id, pack, document); err != nil {
		return nil, err
	}
	delete(fields, "exchangeImportId")
	return json.Marshal(fields)
}
