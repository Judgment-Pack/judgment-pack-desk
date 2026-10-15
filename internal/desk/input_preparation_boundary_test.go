package desk

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"strconv"
	"strings"
	"testing"
)

// Future features must not silently return to local record files. Journals and
// evidence have separate storage ownership; this guard concerns collections.
func TestDialectCollectionsUseRepository(t *testing.T) {
	adapters := map[string]bool{"input_preparation_file.go": true, "input_preparation_read.go": true, "input_preparation_repository.go": true}
	forbidden := map[string]bool{"openInputPreparations": true, "readInputPreparation": true, "writeInputPreparation": true, "inputPreparationDir": true}
	assertStorageBoundary(t, adapters, forbidden, []string{".desk-private/input-preparations"})
}

func TestResearchEvidenceUsesArtifactRepository(t *testing.T) {
	adapters := map[string]bool{"retained_artifacts.go": true, "retained_artifacts_file.go": true, "input_research_proof_store.go": true}
	forbidden := map[string]bool{"openRetainedArtifacts": true, "retainedArtifactDirectory": true, "fileRetainedArtifactRepository": true}
	assertStorageBoundary(t, adapters, forbidden, []string{".desk-private/retained-artifacts", ".proof.json"})
}

func assertStorageBoundary(t *testing.T, adapters, forbidden map[string]bool, paths []string) {
	t.Helper()
	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		name := entry.Name()
		if !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") || adapters[name] {
			continue
		}
		set := token.NewFileSet()
		f, err := parser.ParseFile(set, name, nil, 0)
		if err != nil {
			t.Fatal(err)
		}
		ast.Inspect(f, func(node ast.Node) bool {
			switch n := node.(type) {
			case *ast.Ident:
				if forbidden[n.Name] {
					t.Errorf("%s: storage files must be accessed through the owning repository", set.Position(n.Pos()))
				}
			case *ast.BasicLit:
				if n.Kind == token.STRING {
					if value, err := strconv.Unquote(n.Value); err == nil {
						for _, path := range paths {
							if strings.Contains(value, path) {
								t.Errorf("%s: feature code hardcodes storage", set.Position(n.Pos()))
							}
						}
					}
				}
			}
			return true
		})
	}
}

func TestResearchJournalsUseRepository(t *testing.T) {
	adapters := map[string]bool{"input_research_journal_file.go": true, "input_research_journal_store.go": true}
	forbidden := map[string]bool{"readInputResearch": true, "writeInputResearch": true, "changeInputResearch": true, "fileResearchJournal": true}
	assertStorageBoundary(t, adapters, forbidden, nil)
}

func TestEvaluationJournalsUseRepository(t *testing.T) {
	adapters := map[string]bool{"input_evaluation_journal.go": true, "input_evaluation_journal_file.go": true}
	forbidden := map[string]bool{"readInputEvaluation": true, "writeInputEvaluation": true, "fileEvaluationJournal": true}
	assertStorageBoundary(t, adapters, forbidden, nil)
}

func TestDialectDraftsUseRepository(t *testing.T) {
	adapters := map[string]bool{"dialect_draft_file.go": true, "dialect_draft_repository.go": true, "dialect_draft_coordination.go": true}
	forbidden := map[string]bool{"readDialectDraft": true, "writeDialectDraft": true, "readDialectDrafts": true, "openDialectDrafts": true, "dialectDraftDir": true, "fileDialectDraftRepository": true}
	assertStorageBoundary(t, adapters, forbidden, []string{".desk-private/dialect-drafts"})
}
