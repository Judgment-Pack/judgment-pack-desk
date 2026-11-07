package desk

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	runcontext "github.com/Judgment-Pack/judgment-pack-agent/go/context"
)

// Hand-authored port-shaped storage fixture, not a Runtime/factory qualification
// witness. Its inner strings are intentionally never rewritten by this owner.
func assistantDialectFactoryFixtureCandidate() runcontext.DialectCandidate {
	index, err := runcontext.DialectResearchIndexOf([]runcontext.DialectResearchRevision{})
	if err != nil {
		panic(err)
	}
	return runcontext.DialectCandidate{
		DialectIdentity:    runcontext.DialectIdentity{ID: "prep_" + strings.Repeat("1", 32), Revision: strings.Repeat("2", 64), PackKey: "example", PackDigest: "sha256:" + digestOf([]byte(qualificationTestPack))},
		ConversionContract: runcontext.DialectRetainedColumnDraft(),
		DocumentJSON:       "{ \"value\":1.20, \"text\":\"<>&😀\" }\n", PackJSON: qualificationTestPack,
		Columns:       []runcontext.DialectColumn{{Name: "Country", Samples: []runcontext.DialectSample{{ValueJSON: `"CA"`, Origin: "asserted"}}}},
		ResearchIndex: index, Conversions: map[string]runcontext.DialectConversion{"/company/country": {Kind: "text"}},
		Facts:    []runcontext.DialectFactBinding{{Target: "/company/country", Column: "Country", Conversion: runcontext.DialectConversion{Kind: "text"}}},
		Evidence: []runcontext.DialectEvidenceBinding{{Requirement: "license", Column: "Country"}}, Summary: "Retain <>&😀 and exact 1.20",
	}
}

// Compute fixture hashes from the published package canonicalizer directly;
// never obtain expected fixture inputs by recording the store under test.
func assistantDialectFactoryFixtureHash(t *testing.T, value any) string {
	t.Helper()
	raw, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	v, err := runcontext.ParseJSON(raw)
	if err != nil {
		t.Fatal(err)
	}
	return "sha256:" + digestOf([]byte(runcontext.CanonicalJSON(v)))
}
func assistantDialectFactoryFixtureDraft(t *testing.T, stage string, previous *string) (runcontext.DialectDraftCheckpoint, []byte) {
	t.Helper()
	candidate := assistantDialectFactoryFixtureCandidate()
	fields := map[string]any{"version": 2, "attemptId": strings.Repeat("a", 32), "candidate": candidate, "stage": stage, "previousReceipt": previous}
	fields["fingerprint"] = assistantDialectFactoryFixtureHash(t, map[string]any{"attemptId": fields["attemptId"], "candidate": candidate})
	if stage == "checked" {
		fields["check"] = runcontext.DialectDraftCheck{Results: []runcontext.DialectDraftResult{{Operation: "dialect_validate", ResultJSON: `{"isError":true,"content":[{"type":"text","text":"refused <>&"}]}`}}}
	}
	fields["digest"] = assistantDialectFactoryFixtureHash(t, fields)
	raw, err := json.Marshal(fields)
	if err != nil {
		t.Fatal(err)
	}
	var cp runcontext.DialectDraftCheckpoint
	if err = json.Unmarshal(raw, &cp); err != nil {
		t.Fatal(err)
	}
	return cp, raw
}

func TestAssistantDialectFactoryStoreRawCodec(t *testing.T) {
	// Supplementary keys precede BMP private-use keys in the package's UTF-16
	// ordering. The host Runtime/document ordering is not a substitute.
	ordered, err := assistantDialectFactoryDigest([]byte(`{"\ue000":1,"\ud800\udc00":2}`))
	if err != nil || ordered != "sha256:"+digestOf([]byte("{\"𐀀\":2,\"\ue000\":1}")) {
		t.Fatalf("package canonical UTF-16 ordering: %v", err)
	}
	cp, raw := assistantDialectFactoryFixtureDraft(t, "candidate", nil)
	decoded, fingerprint, err := readAssistantDialectFactoryDraft(raw)
	if err != nil || decoded.Candidate.DocumentJSON != cp.Candidate.DocumentJSON || fingerprint != assistantDialectFactoryFixtureHash(t, cp.Candidate) {
		t.Fatalf("exact candidate: %v", err)
	}
	if "sha256:"+digestOf(raw) == decoded.Digest {
		t.Fatal("raw digest confused with package canonical digest")
	}
	// Different outer spelling, identical package value, identical package digest.
	spelling := bytes.ReplaceAll(raw, []byte(`\u003c`), []byte(`<`))
	if bytes.Equal(raw, spelling) {
		t.Fatal("fixture lacks escaped HTML")
	}
	changed, _, err := readAssistantDialectFactoryDraft(spelling)
	if err != nil || changed.Digest != cp.Digest || artifactReference(raw) == artifactReference(spelling) {
		t.Fatalf("selected raw spelling: %v", err)
	}
	for name, bad := range map[string][]byte{
		"duplicate":   bytes.Replace(raw, []byte(`"version":2`), []byte(`"version":2,"version":2`), 1),
		"alias":       bytes.Replace(raw, []byte(`"version":2`), []byte(`"Version":2`), 1),
		"missing":     bytes.Replace(raw, []byte(`"version":2`), []byte(`"extra":1`), 1),
		"null scalar": bytes.Replace(raw, []byte(`"version":2`), []byte(`"version":null`), 1),
		"wrong null":  bytes.Replace(raw, []byte(`"previousReceipt":null`), []byte(`"previousReceipt":""`), 1),
		"surrogate":   bytes.Replace(raw, []byte(`Retain `), []byte(`\ud800`), 1),
		"wrong stage": bytes.Replace(raw, []byte(`"stage":"candidate"`), []byte(`"stage":"checked"`), 1),
	} {
		t.Run(name, func(t *testing.T) {
			if _, _, err := readAssistantDialectFactoryDraft(bad); err == nil {
				t.Fatal("malformed checkpoint accepted")
			}
		})
	}
	previous := "ddr-" + strings.Repeat("b", 64)
	checked, raw := assistantDialectFactoryFixtureDraft(t, "checked", &previous)
	decoded, _, err = readAssistantDialectFactoryDraft(raw)
	if err != nil || decoded.Check.Results[0].ResultJSON != checked.Check.Results[0].ResultJSON {
		t.Fatalf("exact refused evidence: %v", err)
	}
}

func TestAssistantDialectFactoryStoreByteGuards(t *testing.T) {
	// Pure owner byte-boundary objects. These padded objects are deliberately
	// not claimed to be reachable package-valid factory maxima.
	for _, limit := range []int{assistantDialectDraftByteLimit, assistantDialectFinalByteLimit} {
		raw := append([]byte(`{"padding":"`), bytes.Repeat([]byte("x"), limit-len(`{"padding":""}`))...)
		raw = append(raw, []byte(`"}`)...)
		if len(raw) != limit || assistantDialectFactoryRawGuard(raw, limit) != nil {
			t.Fatal("inclusive raw boundary refused")
		}
		if assistantDialectFactoryRawGuard(append(raw, ' '), limit) == nil {
			t.Fatal("one-over raw boundary accepted")
		}
	}
}

func TestAssistantDialectFactoryStoreMissingCompleteReadRefuses(t *testing.T) {
	if _, err := assistantDialectFactoryBoundSnapshot(t.Context(), nil, assistantRunRecord{}, "host"); err == nil {
		t.Fatal("missing complete read accepted")
	}
	index, err := runcontext.DialectResearchIndexOf([]runcontext.DialectResearchRevision{})
	assistantRunMust(t, err)
	snapshot := runcontext.DialectSnapshot{ID: "id", Revision: "revision", PackKey: "pack", PackJSON: "{}", Columns: []runcontext.DialectColumn{}, ResearchIndex: index}
	candidate := runcontext.DialectCandidate{DialectIdentity: runcontext.DialectIdentity{ID: "id", Revision: "revision", PackKey: "pack", PackDigest: "sha256:" + digestOf([]byte("{}"))}, PackJSON: "{}", Columns: []runcontext.DialectColumn{}, ResearchIndex: index}
	if !assistantDialectFactorySnapshotMatches(snapshot, candidate) {
		t.Fatal("exact snapshot did not match")
	}
	candidate.ResearchIndex = nil
	if assistantDialectFactorySnapshotMatches(snapshot, candidate) {
		t.Fatal("unavailable inventory collapsed into known empty inventory")
	}
}

// These literal v0.11 candidate shapes deliberately bypass the current package
// struct: its omitempty tag would erase the identity this regression protects.
func assistantDialectFactoryLegacyFixture(t *testing.T, declarations string) (runcontext.DialectDraftCheckpoint, []byte, []byte) {
	t.Helper()
	candidate := []byte(strings.NewReplacer("PACK_STRING", string(assistantChatJSON(qualificationTestPack)), "PACK_DIGEST", "sha256:"+digestOf([]byte(qualificationTestPack)), "DECLARATIONS", declarations).Replace(`{"retainedRead":null,"researchDeclarations":DECLARATIONS,"conversionContract":{"contract":"retained-column/1","profile":"base","spec":{"version":"0.3.0","revision":"27cb29bf13e4a2d58d69fe19c7ebfeb762b014fc","schemaSha256":"sha256:0cb49f238c78391cb163a65474cc9af962df2bb1024a55c5310c2b784b2f0557","corpusSha256":"sha256:17b16ff7a9b74fe4cc81eb6f6129a6aabdd9834621d9e756aa2d4c1a3f245863"}},"id":"prep_11111111111111111111111111111111","revision":"2222222222222222222222222222222222222222222222222222222222222222","packKey":"example","packDigest":"PACK_DIGEST","dialectDigest":null,"documentJson":"{ \"value\":1.20 }\n","packJson":PACK_STRING,"conversions":{},"facts":[],"evidence":[],"summary":"Historical exact identity","columns":[]}`))
	fields := map[string]any{"version": 1, "attemptId": strings.Repeat("a", 32), "candidate": json.RawMessage(candidate), "stage": "candidate", "previousReceipt": nil}
	fields["fingerprint"] = assistantDialectFactoryFixtureHash(t, map[string]any{"attemptId": fields["attemptId"], "candidate": json.RawMessage(candidate)})
	fields["digest"] = assistantDialectFactoryFixtureHash(t, fields)
	raw := assistantChatJSON(fields)
	var cp runcontext.DialectDraftCheckpoint
	assistantRunMust(t, json.Unmarshal(raw, &cp))
	return cp, raw, candidate
}

func TestAssistantDialectFactoryVersionedRawIdentity(t *testing.T) {
	fingerprints := map[string]string{}
	for _, declarations := range []string{"null", "[]"} {
		t.Run(declarations, func(t *testing.T) {
			original, raw, candidate := assistantDialectFactoryLegacyFixture(t, declarations)
			cp, fingerprint, err := readAssistantDialectFactoryDraft(raw)
			assistantRunMust(t, err)
			want := assistantDialectFactoryFixtureHash(t, json.RawMessage(candidate))
			if cp.Version != 1 || fingerprint != want || cp.Fingerprint != original.Fingerprint || (cp.Candidate.ResearchDeclarations == nil) != (declarations == "null") {
				t.Fatal("legacy exact identity or null/empty distinction changed")
			}
			fingerprints[declarations] = fingerprint
			if current := assistantDialectFactoryFixtureHash(t, cp.Candidate); current == fingerprint {
				t.Fatal("fixture did not detect current-struct remarshal drift")
			}
			// The old flattened final uses the very same literal candidate bytes.
			final := append([]byte(`{"version":1,"draft":{"attemptId":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa","fingerprint":"unused","candidateReceipt":"old-candidate","checkedReceipt":"old-checked","checkedDigest":"old-digest"},"fingerprint":`+string(assistantChatJSON(want))+`,"mappingDigest":"sha256:`+strings.Repeat("1", 64)+`","status":"review-required","validation":{},"coverage":{},"preview":{"fingerprint":`+string(assistantChatJSON(want))+`,"recordCount":0,"recordsWithIssues":0,"issues":[]},`), candidate[1:]...)
			proposal, err := readAssistantDialectFactoryFinal(final)
			assistantRunMust(t, err)
			if proposal.Version != 1 || proposal.Fingerprint != want {
				t.Fatal("legacy final identity changed")
			}
			if _, err := (&assistantDialectFactoryStore{}).retainDraft(t.Context(), raw); err == nil {
				t.Fatal("historical draft authorized a current write")
			}
			if _, err := (&assistantDialectFactoryStore{}).retainFinal(t.Context(), final); err == nil {
				t.Fatal("historical proposal authorized a current write")
			}
			for name, bad := range map[string][]byte{
				"v1-new-index":            bytes.Replace(raw, []byte(`"researchDeclarations":`), []byte(`"researchIndex":null,"researchDeclarations":`), 1),
				"v1-missing-declarations": bytes.Replace(raw, []byte(`"researchDeclarations":`+declarations+`,`), nil, 1),
				"v1-alias":                bytes.Replace(raw, []byte(`"researchDeclarations"`), []byte(`"ResearchDeclarations"`), 1),
				"v1-duplicate":            bytes.Replace(raw, []byte(`"researchDeclarations":`), []byte(`"researchDeclarations":null,"researchDeclarations":`), 1),
			} {
				t.Run(name, func(t *testing.T) {
					if _, _, err := readAssistantDialectFactoryDraft(bad); err == nil {
						t.Fatal("open legacy shape accepted")
					}
				})
			}
		})
	}
	if fingerprints["null"] == fingerprints["[]"] {
		t.Fatal("legacy null and empty fingerprints collapsed")
	}
	for _, originals := range [][]runcontext.DialectResearchRevision{nil, {}, {{Revision: "unavailable", Fields: nil}, {Revision: "empty", Fields: []runcontext.DialectResearchField{}}}} {
		candidate := assistantDialectFactoryFixtureCandidate()
		candidate.ResearchIndex, _ = runcontext.DialectResearchIndexOf(originals)
		fields := map[string]any{"version": 2, "attemptId": strings.Repeat("a", 32), "candidate": candidate, "stage": "candidate", "previousReceipt": nil}
		fields["fingerprint"] = assistantDialectFactoryFixtureHash(t, map[string]any{"attemptId": fields["attemptId"], "candidate": candidate})
		fields["digest"] = assistantDialectFactoryFixtureHash(t, fields)
		raw := assistantChatJSON(fields)
		cp, _, err := readAssistantDialectFactoryDraft(raw)
		assistantRunMust(t, err)
		if (cp.Candidate.ResearchIndex == nil) != (originals == nil) {
			t.Fatal("v2 unavailable/empty inventory collapsed")
		}
		for _, bad := range [][]byte{
			bytes.Replace(raw, []byte(`"researchIndex":`), []byte(`"researchDeclarations":null,"researchIndex":`), 1),
			bytes.Replace(raw, []byte(`"researchIndex":`), []byte(`"ResearchIndex":`), 1),
			bytes.Replace(raw, []byte(`"researchIndex":`), []byte(`"researchIndex":null,"researchIndex":`), 1),
		} {
			if _, _, err := readAssistantDialectFactoryDraft(bad); err == nil {
				t.Fatal("open v2 shape accepted")
			}
		}
	}
}
