package desk

// Definition briefs summarize saved workspace artifacts. They never establish
// runtime validation, approval, or finalization authority.
import (
	"encoding/json"
	"errors"
	"os"
	"strings"
)

type definitionBrief struct {
	Kind   string         `json:"kind"`
	Title  string         `json:"title"`
	Pack   string         `json:"pack"` // Existing wire field; holds the exact definition.
	Record map[string]any `json:"record"`
}
type briefDeclaration struct {
	Path        string `json:"path"`
	Description string `json:"description"`
	Matrix      string `json:"matrix"`
	Rows        string `json:"rows"`
}

func definitionBriefSubject(subject string) bool {
	kind, id, ok := strings.Cut(subject, ":")
	return ok && id != "" && (kind == "pack" || kind == "pack-draft" || kind == "graph" || kind == "graph-draft")
}
func (s *Server) briefJSONFile(path string) ([]byte, error) {
	if path == "" {
		return nil, errors.New("The saved definition has no project path.")
	}
	file := s.readProjectFile(path)
	if file.err != nil || !file.present || !json.Valid(file.data) {
		return nil, errors.New("A saved definition or referenced file could not be read. Check Project files before generating a brief.")
	}
	return file.data, nil
}
func (s *Server) definitionBriefSnapshot(root *os.Root, name, subject string) (json.RawMessage, error) {
	if !definitionBriefSubject(subject) {
		return nil, errors.New("Choose a saved pack or graph.")
	}
	scope, id, _ := strings.Cut(subject, ":")
	kind := strings.TrimSuffix(scope, "-draft")
	snapshot := definitionBrief{Kind: kind, Title: id, Record: map[string]any{}}
	definition := map[string]any{"scope": scope, "id": id}
	snapshot.Record["definition"] = definition
	var config struct {
		Packs  map[string]briefDeclaration `json:"packs"`
		Graphs map[string]briefDeclaration `json:"graphs"`
	}
	if scope != "pack-draft" {
		data, err := s.briefJSONFile(runtimeConfigName)
		if err != nil {
			return nil, err
		}
		if json.Unmarshal(data, &config) != nil {
			return nil, errors.New("Project declarations could not be read.")
		}
	}
	switch scope {
	case "pack", "graph":
		declared, ok := config.Packs[id]
		if kind == "graph" {
			declared, ok = config.Graphs[id]
		}
		if !ok {
			return nil, errors.New("This definition is no longer declared in the project.")
		}
		data, err := s.briefJSONFile(declared.Path)
		if err != nil {
			return nil, err
		}
		snapshot.Pack = string(data)
		definition["description"] = declared.Description
		rows := declared.Matrix
		if kind == "graph" {
			rows = declared.Rows
		}
		if rows != "" {
			data, err := s.briefJSONFile(rows)
			if err != nil {
				return nil, err
			}
			snapshot.Record["configuredCases"] = json.RawMessage(data)
		}
	case "pack-draft":
		saved, err := s.readWorkspaceRecord(root, strings.Replace(name, "briefs-", "draft-packs-", 1), true)
		if err != nil {
			return nil, err
		}
		var store struct {
			Drafts []struct {
				ID         string `json:"id"`
				Title      string `json:"title"`
				Checkpoint struct {
					State struct {
						Candidates []struct {
							Text          string          `json:"text"`
							Revision      int             `json:"revision"`
							Check         json.RawMessage `json:"check"`
							PreviousCheck json.RawMessage `json:"previousCheck"`
						} `json:"candidates"`
						Unknowns json.RawMessage `json:"unknowns"`
						Cases    json.RawMessage `json:"cases"`
					} `json:"state"`
				} `json:"checkpoint"`
			} `json:"drafts"`
		}
		if json.Unmarshal(saved.Content, &store) != nil {
			return nil, errors.New("Saved drafts could not be read.")
		}
		for _, draft := range store.Drafts {
			if draft.ID == id && len(draft.Checkpoint.State.Candidates) > 0 {
				state := draft.Checkpoint.State
				latest := state.Candidates[len(state.Candidates)-1]
				snapshot.Pack, snapshot.Title = latest.Text, draft.Title
				definition["revision"] = latest.Revision
				snapshot.Record["openQuestions"], snapshot.Record["draftCases"] = state.Unknowns, state.Cases
				check := latest.Check
				if len(check) == 0 || string(check) == "null" {
					check = latest.PreviousCheck
				}
				snapshot.Record["recordedCheck"] = check
				break
			}
		}
	case "graph-draft":
		saved, err := s.readWorkspaceRecord(root, strings.Replace(name, "briefs-", "draft-graphs-", 1), false)
		if err != nil {
			return nil, err
		}
		var store graphDraftDocument
		if json.Unmarshal(saved.Content, &store) != nil {
			return nil, errors.New("Saved graph drafts could not be read.")
		}
		for _, draft := range store.Drafts {
			// Scoped subjects are canonical; accept an old unscoped identity only
			// when it resolves uniquely, so legacy links cannot select another chat.
			if graphDraftIdentity(draft) == id || draft.DraftID == id {
				if snapshot.Pack != "" {
					return nil, errors.New("Choose the graph draft with its conversation identity.")
				}
				snapshot.Pack, snapshot.Title = draft.Content, draft.ID
				definition["description"] = draft.Description
			}
		}

	}
	if snapshot.Pack == "" || !json.Valid([]byte(snapshot.Pack)) || !strings.HasPrefix(strings.TrimSpace(snapshot.Pack), "{") {
		return nil, errors.New("Keep a valid saved definition before generating its brief.")
	}
	definition["sha256"] = digestOf([]byte(snapshot.Pack))
	var doc struct {
		Title string `json:"title"`
		Nodes map[string]struct {
			Pack string `json:"pack"`
		} `json:"nodes"`
	}
	if json.Unmarshal([]byte(snapshot.Pack), &doc) != nil {
		return nil, errors.New("The definition could not be read.")
	}
	if scope == "pack" && doc.Title != "" {
		snapshot.Title = doc.Title
	}
	if kind == "pack" {
		tests, err := s.briefPackTests(root, name, id, snapshot.Pack)
		if err != nil {
			return nil, err
		}
		snapshot.Record["tests"] = tests
	} else {
		packs := map[string]json.RawMessage{}
		for _, node := range doc.Nodes {
			if _, ok := packs[node.Pack]; ok {
				continue
			}
			declared, ok := config.Packs[node.Pack]
			if !ok {
				return nil, errors.New("A referenced pack is not declared. Resolve it before generating a graph brief.")
			}
			data, err := s.briefJSONFile(declared.Path)
			if err != nil {
				return nil, err
			}
			if !strings.HasPrefix(strings.TrimSpace(string(data)), "{") {
				return nil, errors.New("A referenced pack is not a definition object.")
			}
			packs[node.Pack] = data
		}
		snapshot.Record["packs"] = packs
	}
	snapshot.Record["scopeNote"] = "Definition summary only. Saved test observations and draft checks are historical, not proof of current readiness or approval. No operational run result is included."
	result, err := json.Marshal(snapshot)
	if err == nil && len(result) > 1000000 {
		return nil, errors.New("The saved inputs are too large for a one-page brief.")
	}
	return result, err
}
func (s *Server) briefPackTests(root *os.Root, name, owner, text string) (any, error) {
	saved, err := s.readWorkspaceRecord(root, strings.Replace(name, "briefs-", "pack-tests-", 1), false)
	if err != nil {
		return nil, err
	}
	var store struct {
		Suites map[string]struct {
			Cases json.RawMessage `json:"cases"`
			Runs  []struct {
				ID       string          `json:"id"`
				At       string          `json:"at"`
				PackText string          `json:"packText"`
				Cases    json.RawMessage `json:"cases"`
				Report   json.RawMessage `json:"report"`
				Trial    json.RawMessage `json:"trial"`
				Error    string          `json:"error"`
			} `json:"runs"`
		} `json:"suites"`
	}
	if json.Unmarshal(saved.Content, &store) != nil {
		return nil, errors.New("Saved tests could not be read.")
	}
	suite := store.Suites[owner]
	result := map[string]any{"cases": suite.Cases}
	if len(suite.Runs) > 0 {
		run := suite.Runs[len(suite.Runs)-1]
		result["latestRun"] = map[string]any{"id": run.ID, "at": run.At, "report": run.Report, "trial": run.Trial, "error": run.Error,
			"matchesDefinition": run.PackText == text, "matchesCases": sameBriefJSON(run.Cases, suite.Cases)}
	}
	return result, nil
}
