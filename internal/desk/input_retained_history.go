package desk

import (
	"encoding/json"
	"errors"
	"reflect"
	"unicode/utf8"
)

// retainedResearchHistory is a detached original inventory, not an acquisition
// or review credential. Source/run/record identities retain their own domains.
type retainedResearchHistory struct {
	PreparationID string
	RecordID      string
	RunID         string
	RunRevision   string
	RecipeVersion int
	Fields        []inputResearchField
}

// resolveRetainedResearchHistory must run before a new producer issues a
// retained-column carrier. Ordinary typedResearchValue remains unchanged.
func resolveRetainedResearchHistory(doc inputPreparation, record preparedInputRecord, run inputResearchRun) (retainedResearchHistory, error) {
	bad := errors.New("The complete original research field inventory is unavailable or inconsistent.")
	if record.Class != "generated" || record.Research == nil || !retainedRunID.MatchString(record.Research.RunID) || record.Research.RunID != run.ID || run.PreparationID != doc.ID || !retainedSafeVersion(record.Research.RecipeVersion) || run.RecipeVersion != record.Research.RecipeVersion {
		return retainedResearchHistory{}, bad
	}
	version := record.Research.RecipeVersion
	var original *inputResearchRecipe
	matches := 0
	for _, revision := range doc.Recipes {
		if revision.Version == version {
			matches++
			original = revision.Recipe.Research
		}
	}
	if matches > 1 {
		return retainedResearchHistory{}, bad
	}
	if doc.RecipeVersion == version {
		current := doc.Recipe.Research
		if matches == 1 && (current == nil || original == nil || !reflect.DeepEqual(current.Fields, original.Fields)) {
			return retainedResearchHistory{}, bad
		}
		if matches == 0 {
			original = current
		}
	}
	// nil is unavailable; a non-nil empty inventory is known empty.
	if original == nil || original.Fields == nil || run.Recipe.Fields == nil || !reflect.DeepEqual(original.Fields, run.Recipe.Fields) {
		return retainedResearchHistory{}, bad
	}
	if len(original.Fields) > retainedFieldCount {
		return retainedResearchHistory{}, bad
	}
	seen := map[string]bool{}
	for _, field := range original.Fields {
		if !retainedColumnName(field.Name) || !utf8.ValidString(field.Description) || !utf8.ValidString(field.Type) || seen[field.Name] {
			return retainedResearchHistory{}, bad
		}
		seen[field.Name] = true
		raw, err := json.Marshal(field)
		if err != nil || len(raw) > retainedDeclarationBytes {
			return retainedResearchHistory{}, bad
		}
	}
	fields := make([]inputResearchField, len(original.Fields))
	copy(fields, original.Fields)
	return retainedResearchHistory{PreparationID: doc.ID, RecordID: record.ID, RunID: run.ID, RunRevision: run.Revision, RecipeVersion: version, Fields: fields}, nil
}
