package desk

import (
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"strings"
	"testing"
	"unicode/utf8"

	runcontext "github.com/Judgment-Pack/judgment-pack-agent/go/context"
	"github.com/Judgment-Pack/judgment-pack-agent/go/failure"
)

// The Go and browser paths consume the same manually authored inputs and
// canonical strings. No expected descriptor/canonical value comes from a run.
type assistantRuntimeIdentityVector struct {
	ID         string `json:"id"`
	Descriptor string `json:"descriptor"`
	Result     string `json:"result"`
	Canonical  string `json:"canonical"`
	Version    string `json:"version"`
	VersionHex string `json:"versionHex"`
}
type assistantRuntimeIdentityVectors struct {
	Version           int                              `json:"version"`
	CanonicalCases    []assistantRuntimeIdentityVector `json:"canonicalCases"`
	PayloadCases      []assistantRuntimeIdentityVector `json:"payloadCases"`
	DisabledResults   []assistantRuntimeIdentityVector `json:"disabledResults"`
	MalformedVersions []assistantRuntimeIdentityVector `json:"malformedVersions"`
}

func assistantRuntimeIdentityTestVectors(t *testing.T) assistantRuntimeIdentityVectors {
	t.Helper()
	raw, err := os.ReadFile("testdata/runtime-cache-identity.json")
	if err != nil {
		t.Fatal(err)
	}
	var vectors assistantRuntimeIdentityVectors
	if err = json.Unmarshal(raw, &vectors); err != nil {
		t.Fatal(err)
	}
	if vectors.Version != 1 || len(vectors.CanonicalCases) == 0 || len(vectors.PayloadCases) == 0 || len(vectors.DisabledResults) == 0 || len(vectors.MalformedVersions) == 0 {
		t.Fatal("invalid shared runtime identity vectors")
	}
	return vectors
}

// Exercise the real MCP-result selection and identity construction boundary,
// not just the string decoder. The Agent v0.13 opener must refuse these preserved
// invalid versions; nil would instead admit a run with an unavailable identity.
func TestAssistantRuntimeCacheIdentityMalformedVersions(t *testing.T) {
	for _, row := range assistantRuntimeIdentityTestVectors(t).MalformedVersions {
		t.Run(row.ID, func(t *testing.T) {
			canonical, version, ok := assistantRuntimeCacheDescriptor([]byte(row.Result))
			if !ok || string(canonical) != row.Canonical || hex.EncodeToString([]byte(version)) != row.VersionHex || utf8.ValidString(version) {
				t.Fatalf("canonical=%s version bytes=%x ok=%v; want %s version bytes=%s invalid UTF-8", canonical, version, ok, row.Canonical, row.VersionHex)
			}
			identity := assistantRuntimeCacheIdentity([]byte(row.Result))
			if identity == nil || identity.Identity != digestOf([]byte(row.Canonical)) || identity.Version != version {
				t.Fatal("malformed version was repaired or downgraded to unavailable")
			}
			t.Run("admission", func(t *testing.T) {
				// This assertion requires Agent v0.13; v0.12 does not refuse
				// malformed Runtime identities at its opening.
				input := observationFixtureInput()
				input.Runtime = identity
				opened, err := runcontext.ContextualSession(t.Context(), observationFixtureSession(input))
				if opened != nil {
					opened.Context.Close()
				}
				var refused *failure.Error
				const detail = "Runtime identity refused: its identity or version holds a lone UTF-16 surrogate or is not valid UTF-8. Nothing was sent."
				if opened != nil || !errors.As(err, &refused) || refused.Failure.Code != "configuration" || refused.Failure.Detail == nil || *refused.Failure.Detail != detail {
					t.Fatalf("malformed source identity did not receive typed admission refusal: opened=%v err=%v", opened != nil, err)
				}
			})
		})
	}
}

func TestAssistantRuntimeCacheIdentityInvalidUTF8EnvelopeRemainsUnavailable(t *testing.T) {
	// These are invalid transport JSON bytes, rather than a JSON string carrying
	// a surrogate escape. Preserve the existing unavailable-descriptor behavior.
	for _, raw := range []string{
		`{"structuredContent":{"tool":{"name":"Runtime","version":"` + "\xed\xa0\x80" + `"}}}`,
		`{"content":[{"type":"text","text":"` + "\xff" + `"}]}`,
	} {
		if assistantRuntimeCacheIdentity([]byte(raw)) != nil {
			t.Fatal("invalid UTF-8 transport JSON named a Runtime")
		}
	}
}
func assistantRuntimeIdentityAssertVector(t *testing.T, row assistantRuntimeIdentityVector, result []byte) {
	t.Helper()
	canonical, version, ok := assistantRuntimeCacheDescriptor(result)
	if !ok || version != row.Version || string(canonical) != row.Canonical {
		t.Fatalf("canonical=%s version=%q ok=%v; want %s version=%q", canonical, version, ok, row.Canonical, row.Version)
	}
	identity := assistantRuntimeCacheIdentity(result)
	if identity == nil || identity.Identity != digestOf([]byte(row.Canonical)) || identity.Version != row.Version {
		t.Fatal("identity did not hash actual canonical descriptor/version")
	}
}
func TestAssistantRuntimeCacheIdentityCanonicalVectors(t *testing.T) {
	for _, row := range assistantRuntimeIdentityTestVectors(t).CanonicalCases {
		t.Run(row.ID, func(t *testing.T) {
			assistantRuntimeIdentityAssertVector(t, row, []byte(`{"structuredContent":`+row.Descriptor+`}`))
			// Transport quoting only; expected canonical strings remain handwritten.
			quoted, err := json.Marshal(row.Descriptor)
			if err != nil {
				t.Fatal(err)
			}
			assistantRuntimeIdentityAssertVector(t, row, []byte(`{"content":[{"type":"text","text":`+string(quoted)+`}]}`))
			if strings.HasPrefix(row.ID, "version-") {
				t.Run("admission", func(t *testing.T) {
					input := observationFixtureInput()
					input.Runtime = assistantRuntimeCacheIdentity([]byte(`{"structuredContent":` + row.Descriptor + `}`))
					opened, err := runcontext.ContextualSession(t.Context(), observationFixtureSession(input))
					if err != nil || opened == nil {
						t.Fatalf("valid scalar version did not open: %v", err)
					}
					defer opened.Context.Close()
					if opened.Session.Context.Runtime.Version != row.Version {
						t.Fatal("admission changed a valid scalar version")
					}
				})
			}
		})
	}
}
func TestAssistantRuntimeCacheIdentitySharedPayloadsAndExclusions(t *testing.T) {
	vectors := assistantRuntimeIdentityTestVectors(t)
	for _, row := range vectors.PayloadCases {
		t.Run(row.ID, func(t *testing.T) { assistantRuntimeIdentityAssertVector(t, row, []byte(row.Result)) })
	}
	for _, row := range vectors.DisabledResults {
		t.Run(row.ID, func(t *testing.T) {
			if assistantRuntimeCacheIdentity([]byte(row.Result)) != nil {
				t.Fatal("invalid/unreleased descriptor cached")
			}
		})
	}
}
func TestAssistantRuntimeCacheIdentityDependsOnDescriptorNotNonce(t *testing.T) {
	raw := `{"structuredContent":{"tool":{"name":"Runtime","version":"0.39.0"},"schema":"one","order":[1,2]}}`
	first := assistantRuntimeCacheIdentity([]byte(raw))
	same := assistantRuntimeCacheIdentity([]byte(raw))
	if first == nil || same == nil || *first != *same {
		t.Fatal("same actual descriptor is not reusable")
	}
	for _, changed := range []string{strings.Replace(raw, `"one"`, `"two"`, 1), strings.Replace(raw, `[1,2]`, `[2,1]`, 1), strings.Replace(raw, `0.39.0`, `0.39.1`, 1)} {
		got := assistantRuntimeCacheIdentity([]byte(changed))
		if got == nil || got.Identity == first.Identity {
			t.Fatal("descriptor/version mutation reused old identity")
		}
	}
}
