package desk

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestResearchQuotationRestoresRetainedWhitespace(t *testing.T) {
	for _, tt := range []struct{ name, source, quote, want string }{
		{"exact", "Prefix. Local exceptions apply. Suffix.", "Local exceptions apply.", "Local exceptions apply."},
		{"PDF line wraps", "Prefix. Local exceptions may apply. \nPlease consult with your underwriter. Suffix.", "Local exceptions may apply. Please consult with your underwriter.", "Local exceptions may apply. \nPlease consult with your underwriter."},
		{"PDF spacing", "Prefix. ACE  Express UmbrellaSM \n  \nAccount Eligibility. Suffix.", "ACE Express UmbrellaSM Account Eligibility.", "ACE  Express UmbrellaSM \n  \nAccount Eligibility."},
		{"Unicode", "Prefix. Équipe\u00a0canadienne\r\nà Toronto. Suffix.", "Équipe canadienne à Toronto.", "Équipe\u00a0canadienne\r\nà Toronto."},
		{"formatting in submitted quote", "Local exceptions apply.", "  Local\nexceptions  apply.  ", "Local exceptions apply."},
		{"later bounded span", "a" + strings.Repeat(" ", 2001) + "b; a\nb", "a b", "a\nb"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := researchSourceQuote(tt.source, tt.quote)
			if !ok || got != tt.want || !strings.Contains(tt.source, got) {
				t.Fatalf("lost original source span: %q %v", got, ok)
			}
			again, ok := researchSourceQuote(tt.source, got)
			if !ok || again != got {
				t.Fatal("saved quotation is not stable")
			}
		})
	}
}

func TestResearchQuotationStillRejectsChangedOrUnboundedEvidence(t *testing.T) {
	for _, tt := range []struct{ name, source, quote string }{
		{"changed words", "No exceptions\nare allowed.", "All exceptions are allowed."},
		{"omitted negation", "Exceptions are\nnot allowed.", "Exceptions are allowed."},
		{"changed number", "Limit:\n500.", "Limit: 5000."},
		{"changed punctuation", "Referral required;\nexceptions possible.", "Referral required, exceptions possible."},
		{"case", "Local\nexceptions.", "local exceptions."},
		{"removed word boundary", "under writer", "underwriter"},
		{"inserted word boundary", "underwriter", "under writer"},
		{"hyphenation", "under-\nwriting", "underwriting"},
		{"zero width character", "under\u200bwriter", "underwriter"},
		{"empty", "words", ""},
		{"whitespace only", "words\n \t", " \n"},
		{"long model quote", strings.Repeat("a", 2001), strings.Repeat("a", 2001)},
		{"long source span", "a" + strings.Repeat(" ", 2001) + "b", "a b"},
		{"invalid UTF8", "valid text", "\xff"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			if got, ok := researchSourceQuote(tt.source, tt.quote); ok {
				t.Fatalf("accepted unsupported quote: %q", got)
			}
		})
	}
}

func TestInputResearchFormattedQuoteKeepsItsSourceAndClaimType(t *testing.T) {
	recipe := researchRecipeFixture()
	source := inputResearchSource{ID: "page-1", Kind: "page", Text: "The Canadian\n team.", Verification: "verified"}
	sources := map[string]inputResearchSource{source.ID: source}
	allowed := map[string]bool{"https://example.com/": true}
	for _, status := range []string{"supported", "interpretation"} {
		entity := inputResearchEntity{Kind: "company", Name: "Example", URL: "https://example.com/", Fields: map[string]inputResearchValue{"country": {Value: json.RawMessage(`"Canada"`), Status: status, Sources: []inputResearchCitation{{SourceID: source.ID, Quote: "Canadian team"}}}}}
		if err := validateResearchEntity(&entity, recipe, sources, allowed, nil); err != nil {
			t.Fatal(err)
		}
		value := entity.Fields["country"]
		if value.Status != status || value.Sources[0].SourceID != source.ID || value.Sources[0].Quote != "Canadian\n team" || sources[source.ID].Text != source.Text {
			t.Fatal("claim type, citation identity or retained source changed")
		}
	}
}

// Revalidate an already retained proposal without model/provider requests or
// writes to the live run. Source signatures are checked independently first.
func TestResearchRetainedProposalOffline(t *testing.T) {
	path, program := os.Getenv("JPS_TEST_RESEARCH_RUN"), os.Getenv("JPS_TEST_GATEWAY")
	if path == "" || program == "" {
		t.Skip("set JPS_TEST_RESEARCH_RUN and JPS_TEST_GATEWAY for an offline proposal check")
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var run inputResearchRun
	if err = json.Unmarshal(raw, &run); err != nil {
		t.Fatal(err)
	}
	for _, source := range run.Sources {
		rawProof, err := os.ReadFile(filepath.Join(filepath.Dir(path), source.ID+".proof.json"))
		if err != nil {
			t.Fatal(err)
		}
		var proof inputResearchProof
		if json.Unmarshal(rawProof, &proof) != nil || proof.Pin != run.Gateway {
			t.Fatal("source Gateway differs from review")
		}
		result, digest, err := verifyResearchProof(context.Background(), program, proof)
		if err != nil {
			t.Fatal(err)
		}
		decoded, err := decodeInputResearchSource(inputResearchSource{Kind: source.Kind}, proof.Arguments, result)
		if err != nil || decoded.Text != source.Text || digest != source.Digest {
			t.Fatal("retained source differs from verified response", err)
		}
	}
	var proposed int
	for i := len(run.Attempts) - 1; i >= 0; i-- {
		attempt := run.Attempts[i]
		if attempt.Kind != "model" || attempt.State != "completed" {
			continue
		}
		action, err := decodeResearchAction(attempt.Result)
		if err != nil || action.Tool != "records" {
			continue
		}
		var batch struct {
			Records []inputResearchEntity `json:"records"`
		}
		if json.Unmarshal(action.Arguments, &batch) != nil {
			t.Fatal("invalid retained batch")
		}
		proposed = len(batch.Records)
		if err := applyResearchRecords(&run, action.Arguments); err != nil {
			t.Fatal(err)
		}
		break
	}
	if proposed == 0 || len(run.Records) < proposed {
		t.Fatal("saved proposal not validated")
	}
	for _, record := range run.Records {
		for _, field := range record.Fields {
			for _, cite := range field.Sources {
				found := false
				for _, source := range run.Sources {
					if source.ID == cite.SourceID && strings.Contains(source.Text, cite.Quote) {
						found = true
						break
					}
				}
				if !found {
					t.Fatal("saved citation is not an exact retained passage")
				}
			}
		}
	}
	after, err := os.ReadFile(path)
	if err != nil || !bytes.Equal(after, raw) {
		t.Fatal("historical run changed")
	}
	t.Logf("Validated %d proposed records against %d independently verified retained sources; no external requests or saved-run changes.", proposed, len(run.Sources))
}
