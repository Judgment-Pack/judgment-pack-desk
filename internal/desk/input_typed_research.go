package desk

import (
	"encoding/json"
	"errors"
)

// typedResearchValue reads the original typed value, never its display column.
// The field declaration is pinned to the record's research recipe revision.
// Review/evidence admission remains at the shared selection and handoff boundaries.
func typedResearchValue(doc inputPreparation, record preparedInputRecord, column string) (any, error) {
	if record.Class != "generated" || record.Research == nil {
		return nil, errors.New("Research value requires a typed research field.")
	}
	// These columns belong to the host record and selected-parent relationship.
	// A same-named model field must never replace that metadata.
	if contains([]string{"name", "url", "website", "company_id", "company_name", "company_url"}, column) {
		return nil, errors.New("Record metadata cannot use Research value. Choose another conversion.")
	}
	var recipe *inputResearchRecipe
	if doc.RecipeVersion == record.Research.RecipeVersion {
		recipe = doc.Recipe.Research
	}
	for _, revision := range doc.Recipes {
		if revision.Version == record.Research.RecipeVersion {
			recipe = revision.Recipe.Research
			break
		}
	}
	if recipe == nil {
		return nil, errors.New("The original research field declaration is unavailable.")
	}
	var kind string
	for _, field := range recipe.Fields {
		if field.Name == column {
			kind = field.Type
			break
		}
	}
	if kind == "" {
		return nil, errors.New("Choose a declared research field for Research value.")
	}
	field, present := record.Research.Entity.Fields[column]
	if !present || (field.Status != "supported" && field.Status != "interpretation") {
		return nil, nil
	}
	if !validResearchValue(field.Value, kind) {
		return nil, errors.New("The research value does not match its original field type.")
	}
	return append(json.RawMessage(nil), field.Value...), nil
}
