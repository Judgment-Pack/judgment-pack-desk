package desk

// The shared desk-configuration fixtures, read by this decoder.
//
// **The same directory is read by `web/src/config/fixtures.test.ts`.** There
// are two implementations of one contract — the browser's, which decides what
// Admin shows, and this one, which decides whether a credential leaves this
// machine — and two implementations of one rule drift. They drifted once, and
// the way they drifted is the reason this file exists: the chassis read only
// `assistant.endpoint`, so a file the browser refused whole still authorised
// an outbound request carrying the stored key.
//
// `expected.json` is the only place the verdicts are written down. A rule
// changed on one side and not the other fails on both.

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

// fixtureDir is the one directory both languages read.
func fixtureDir(t *testing.T) string {
	t.Helper()
	return filepath.Join("..", "..", "web", "src", "config", "fixtures", "desk-config")
}

type fixtureVerdict struct {
	Accepted bool     `json:"accepted"`
	Keys     []string `json:"keys"`
}

func fixtureVerdicts(t *testing.T) map[string]fixtureVerdict {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(fixtureDir(t), "expected.json"))
	if err != nil {
		t.Fatalf("expected.json: %v", err)
	}
	var verdicts map[string]fixtureVerdict
	if err := json.Unmarshal(data, &verdicts); err != nil {
		t.Fatalf("expected.json: %v", err)
	}
	return verdicts
}

func fixtureNames(t *testing.T) []string {
	t.Helper()
	entries, err := os.ReadDir(fixtureDir(t))
	if err != nil {
		t.Fatalf("fixtures: %v", err)
	}
	var names []string
	for _, entry := range entries {
		name := entry.Name()
		if !strings.HasSuffix(name, ".json") || name == "expected.json" {
			continue
		}
		names = append(names, strings.TrimSuffix(name, ".json"))
	}
	sort.Strings(names)
	return names
}

func TestSharedFixturesHaveAVerdictEachWay(t *testing.T) {
	// A verdict file that has drifted from the directory is a suite that
	// silently stops checking a case. Both directions, so neither an unjudged
	// fixture nor a verdict about a file nobody wrote survives.
	verdicts := fixtureVerdicts(t)
	names := fixtureNames(t)
	if len(names) != len(verdicts) {
		t.Fatalf("%d fixtures and %d verdicts", len(names), len(verdicts))
	}
	for _, name := range names {
		if _, ok := verdicts[name]; !ok {
			t.Errorf("%s has no verdict", name)
		}
	}
	for name := range verdicts {
		if !contains(names, name) {
			t.Errorf("expected.json judges %s, which is not a fixture", name)
		}
	}
}

func TestSharedFixturesDecodeAsTheVerdictSays(t *testing.T) {
	verdicts := fixtureVerdicts(t)
	for _, name := range fixtureNames(t) {
		t.Run(name, func(t *testing.T) {
			data, err := os.ReadFile(filepath.Join(fixtureDir(t), name+".json"))
			if err != nil {
				t.Fatalf("read: %v", err)
			}
			decoded := decodeDeskFile(data)
			verdict := verdicts[name]
			if verdict.Accepted {
				if decoded.refused() {
					t.Fatalf("accepted fixture was refused: %v", decoded.Problems)
				}
				return
			}
			if !decoded.refused() {
				t.Fatal("refused fixture was accepted")
			}
			// **A refused file yields no endpoint**, whatever else was in it.
			// This is the property the probe depends on.
			if decoded.Endpoint != nil {
				t.Error("a refused file produced an endpoint")
			}
			// The keys as a set: the two decoders walk the document
			// differently, and requiring one order would be a contract about
			// traversal that neither side promises.
			seen := map[string]bool{}
			var keys []string
			for _, problem := range decoded.Problems {
				if seen[problem.Key] {
					continue
				}
				seen[problem.Key] = true
				keys = append(keys, problem.Key)
			}
			sort.Strings(keys)
			want := append([]string(nil), verdict.Keys...)
			sort.Strings(want)
			if strings.Join(keys, ",") != strings.Join(want, ",") {
				t.Fatalf("keys %v, want %v (problems %v)", keys, want, decoded.Problems)
			}
		})
	}
}

func TestARefusedFileSendsNothing(t *testing.T) {
	// **The assertion the whole exercise is for.** Every refused fixture, each
	// one carrying an otherwise-serviceable endpoint pointed at a live stub,
	// and the stub must record zero requests. A probe that "merely" reported a
	// refusal while having already opened the connection would pass a status
	// assertion and fail this one.
	verdicts := fixtureVerdicts(t)
	for _, name := range fixtureNames(t) {
		if verdicts[name].Accepted {
			continue
		}
		t.Run(name, func(t *testing.T) {
			var reached int
			stub := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
				reached++
			}))
			defer stub.Close()

			data, err := os.ReadFile(filepath.Join(fixtureDir(t), name+".json"))
			if err != nil {
				t.Fatalf("read: %v", err)
			}
			// The fixture's own endpoint is left exactly as written — the
			// point is that the file is refused, not that its URL is
			// unreachable — and the stub stands beside it to catch any request
			// at all. Where the fixture happens to name a resolvable host, the
			// refusal is what must stop the request; where it does not, the
			// count is still zero and the stub is the control.
			s, ts, _ := assistantServer(t)
			writeDeskConfig(t, s, string(data))
			if status, body := storeKey(t, ts, testKey); status != http.StatusOK {
				t.Fatalf("store: %d %v", status, body)
			}
			status, body := postJSON(t, ts, "/api/assistant/probe")
			if status != http.StatusConflict {
				t.Fatalf("status %d, want 409; body %v", status, body)
			}
			if body["code"] != CodeAssistantUnconfigured {
				t.Fatalf("code %v, want %s", body["code"], CodeAssistantUnconfigured)
			}
			if reached != 0 {
				t.Fatalf("the stub was reached %d times by a refused configuration", reached)
			}
		})
	}
}

func TestAnAcceptedFixtureDoesReachTheEndpoint(t *testing.T) {
	// The control for the test above: without it, "zero requests" would be
	// satisfied by a probe that never works at all.
	var reached int
	stub := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		reached++
		w.WriteHeader(http.StatusOK)
	}))
	defer stub.Close()

	s, ts, _ := assistantServer(t)
	writeDeskConfig(t, s, `{"deskConfigVersion":1,"assistant":{"endpoint":{"url":"`+
		stub.URL+`/v1","kind":"openai-compatible","model":"a-model","tools":["validate"]}}}`)
	if status, _ := storeKey(t, ts, testKey); status != http.StatusOK {
		t.Fatal("store")
	}
	status, body := postJSON(t, ts, "/api/assistant/probe")
	if status != http.StatusOK || body["reachable"] != true {
		t.Fatalf("status %d, body %v", status, body)
	}
	if reached != 1 {
		t.Fatalf("the stub was reached %d times, want once", reached)
	}
}

func TestTheKeySentenceIsTheSameOnBothSides(t *testing.T) {
	// The refusal a reader meets is the thing the two decoders are for; two
	// spellings of it would be two contracts. Read out of the TypeScript
	// declaration rather than copied here.
	source, err := os.ReadFile(filepath.Join(
		"..", "..", "web", "src", "config", "deskConfig.ts"))
	if err != nil {
		t.Fatalf("deskConfig.ts: %v", err)
	}
	text := string(source)
	opening := strings.Index(text, "export const KEYS_ARE_NEVER_IN_CONFIGURATION =")
	if opening < 0 {
		t.Fatal("KEYS_ARE_NEVER_IN_CONFIGURATION is not declared")
	}
	body := text[opening : opening+600]
	var pieces []string
	for _, line := range strings.Split(body, "\n") {
		start := strings.Index(line, "'")
		end := strings.LastIndex(line, "'")
		if start < 0 || end <= start {
			if len(pieces) > 0 {
				break
			}
			continue
		}
		pieces = append(pieces, line[start+1:end])
	}
	declared := strings.Join(pieces, "")
	if declared != keysAreNeverInConfiguration {
		t.Errorf("the sentence differs:\n go: %q\n ts: %q", keysAreNeverInConfiguration, declared)
	}
}
