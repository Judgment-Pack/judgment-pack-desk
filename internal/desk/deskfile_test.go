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
	"io"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
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
			// The endpoint is *carried* out of a refused decode now — see the
			// note at the end of `decodeDeskFile` — so what this asserts is
			// the property the probe actually depends on: the verdict says
			// refused, and `configuredEndpoint` gates on that. Whether an
			// endpoint object was assembled along the way is not the question.
			if !decoded.refused() {
				t.Error("a refused file reports itself clean")
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

// countingTransport records every request that reaches the wire.
//
// **This is the only place "no outbound request was made" can be established.**
// The version this replaced started a stub server and asserted the stub was
// not reached — while leaving each fixture's own unrelated URL in place, so
// the stub was never the destination and `reached == 0` was true of a probe
// that had happily called somewhere else. Counting at the transport is a
// statement about what left the process, not about who happened to answer.
type countingTransport struct {
	mu    sync.Mutex
	calls int
	to    []string
}

func (c *countingTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	c.mu.Lock()
	c.calls++
	c.to = append(c.to, r.URL.String())
	c.mu.Unlock()
	return &http.Response{
		StatusCode: http.StatusOK,
		Status:     "200 OK",
		Body:       io.NopCloser(strings.NewReader("{}")),
		Header:     make(http.Header),
		Request:    r,
	}, nil
}

func (c *countingTransport) seen() (int, []string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.calls, append([]string(nil), c.to...)
}

// countingProbes swaps the probe client's transport for the duration of a test.
func countingProbes(t *testing.T) *countingTransport {
	t.Helper()
	counter := &countingTransport{}
	restore := probeClient.Transport
	probeClient.Transport = counter
	t.Cleanup(func() { probeClient.Transport = restore })
	return counter
}

func TestARefusedFileSendsNothing(t *testing.T) {
	// **The assertion the whole exercise is for**, and now measured where it
	// means something. Every refused fixture is probed with a key stored, and
	// the transport must not be reached once — not "reached a different
	// place", not at all.
	verdicts := fixtureVerdicts(t)
	for _, name := range fixtureNames(t) {
		if verdicts[name].Accepted {
			continue
		}
		t.Run(name, func(t *testing.T) {
			counter := countingProbes(t)
			data, err := os.ReadFile(filepath.Join(fixtureDir(t), name+".json"))
			if err != nil {
				t.Fatalf("read: %v", err)
			}
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
			if calls, to := counter.seen(); calls != 0 {
				t.Fatalf("a refused configuration made %d outbound request(s), to %v", calls, to)
			}
		})
	}
}

func TestAnAcceptedFileDoesReachTheTransport(t *testing.T) {
	// The positive control. Without it "zero requests" is satisfied by a probe
	// that never works at all, which is the failure mode the previous version
	// of this pair actually had.
	counter := countingProbes(t)
	verdicts := fixtureVerdicts(t)
	tried := 0
	for _, name := range fixtureNames(t) {
		if !verdicts[name].Accepted {
			continue
		}
		data, err := os.ReadFile(filepath.Join(fixtureDir(t), name+".json"))
		if err != nil {
			t.Fatalf("read: %v", err)
		}
		// Only the fixtures that actually configure an endpoint can probe; the
		// rest are accepted files with nothing to reach.
		if decodeDeskFile(data).Endpoint == nil {
			continue
		}
		tried++
		s, ts, _ := assistantServer(t)
		writeDeskConfig(t, s, string(data))
		if status, _ := storeKey(t, ts, testKey); status != http.StatusOK {
			t.Fatalf("%s: store", name)
		}
		if status, body := postJSON(t, ts, "/api/assistant/probe"); status != http.StatusOK {
			t.Fatalf("%s: status %d, body %v", name, status, body)
		}
	}
	if tried == 0 {
		t.Fatal("no accepted fixture configures an endpoint, so this control proves nothing")
	}
	calls, to := counter.seen()
	if calls != tried {
		t.Fatalf("%d accepted fixtures made %d requests (%v)", tried, calls, to)
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

func TestAMemberIsRefusedOnceWithTheCredentialSentence(t *testing.T) {
	// The credential sentence has **one producer** — the recursive pre-scan —
	// and the schema walk says only "unknown key". A member that is both was
	// reported twice for two reasons, which reads on Admin as two mistakes,
	// and made the mutation row for either producer survive.
	decoded := decodeDeskFile([]byte(
		`{"deskConfigVersion":1,"assistant":{"endpoint":{"apiKey":"sk-nope"}}}`))
	var about []deskProblem
	for _, problem := range decoded.Problems {
		if problem.Key == "assistant.endpoint.apiKey" {
			about = append(about, problem)
		}
	}
	if len(about) != 1 {
		t.Fatalf("%d refusals for one member: %v", len(about), about)
	}
	if about[0].Reason != keysAreNeverInConfiguration {
		t.Errorf("reason %q, want the sentence about keys", about[0].Reason)
	}

	// And an ordinary unknown member still says so.
	plain := decodeDeskFile([]byte(`{"deskConfigVersion":1,"colour":"blue"}`))
	for _, problem := range plain.Problems {
		if problem.Key == "colour" && problem.Reason != "unknown key" {
			t.Errorf("colour: %q", problem.Reason)
		}
	}
}
