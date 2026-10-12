package desk

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"slices"
	"strconv"
	"strings"
	"testing"
)

// Desk reading a real Runner's journal from its newest entries (Runner v0.8.0,
// capability journal-backward, #235): the journal of a job holds several
// thousand entries, written by Runner's own journal writer through the
// changes of one trigger's state. The newest entries are one request away;
// walking back with the `before` cursor of each page reaches the first entry
// and every entry once, in order; the newest page's `next` follows the journal
// forward, and entries that arrive after a backward read are found there once;
// forward answers carry no `before`; and the parameters Runner refuses are
// refused by Desk before they reach it.
func TestJobsRealRunnerReadsAJournalFromItsNewestEntries(t *testing.T) {
	bin, runtime := os.Getenv("JPACK_RUNNER_TEST_BIN"), os.Getenv("JPACK_BIN")
	if bin == "" || runtime == "" {
		t.Skip("set JPACK_RUNNER_TEST_BIN and JPACK_BIN for companion integration")
	}
	config, project := t.TempDir(), t.TempDir()
	if err := os.Chmod(config, 0o700); err != nil {
		t.Fatal(err)
	}
	_, ts := startDesk(t, Config{RunnerBin: bin, JpackBin: runtime, RunnerAllowUntestedReleases: true, ProjectDir: project, DeskConfigDir: config, Token: testToken})
	call := func(ts *httptest.Server, method, path, body string) (int, []byte) {
		t.Helper()
		r, err := http.NewRequest(method, ts.URL+"/api/operations/"+path, strings.NewReader(body))
		if err != nil {
			t.Fatal(err)
		}
		r.Header.Set("Authorization", "Bearer "+testToken)
		r.Header.Set("Content-Type", "application/json")
		response, err := ts.Client().Do(r)
		if err != nil {
			t.Fatal(err)
		}
		defer response.Body.Close()
		data, err := io.ReadAll(response.Body)
		if err != nil {
			t.Fatal(err)
		}
		return response.StatusCode, data
	}
	// The capability, as the page reads it.
	var status struct{ Capabilities []string }
	if code, data := call(ts, "GET", "status", ""); code != 200 || json.Unmarshal(data, &status) != nil || !slices.Contains(status.Capabilities, "journal-backward") {
		t.Fatalf("Runner does not list journal-backward: %d %s", code, data)
	}

	pack, _ := json.Marshal(`{"specVersion":"0.2.0-draft","id":"https://example.invalid/judgment-packs/scored","version":"0.1.0","title":"A scored pack","decision":{"intent":"Decide the one thing this pack decides.","question":"Does this request proceed?"},"outcomes":[{"id":"proceed","label":"Proceed"},{"id":"hold","label":"Hold"}],"rules":[{"id":"seven","description":"Proceed at seven.","when":{"op":"fact","path":"/score","operator":"equals","value":7},"outcome":"proceed","onUnknown":"escalate"}]}`)
	input := `{"source":{"mapping":{"version":2,"case":{"facts":[{"target":"/score","source":"/score"}],"evidence":[]},"sources":[]},"case":{"score":7}}}`
	var release struct{ ID string }
	if code, data := call(ts, "POST", "previews", `{"pack":`+string(pack)+`,"input":`+input+`}`); code != 201 || json.Unmarshal(data, &release) != nil || release.ID == "" {
		t.Fatalf("preview: %d %s", code, data)
	}
	var job struct{ ID string }
	if code, data := call(ts, "POST", "jobs", `{"name":"Journal","releaseId":"`+release.ID+`","reviewed":true}`); code != 201 || json.Unmarshal(data, &job) != nil || job.ID == "" {
		t.Fatalf("job: %d %s", code, data)
	}
	var trigger struct {
		ID       string
		Revision int
	}
	if code, data := call(ts, "POST", "jobs/"+job.ID+"/triggers", `{"config":{"name":"Journal writer","kind":"event","missed":"skip","overlap":"skip","queueSeconds":3600}}`); code != 200 || json.Unmarshal(data, &trigger) != nil || trigger.ID == "" {
		t.Fatalf("trigger: %d %s", code, data)
	}
	// Each change of the trigger's state is one entry of the job's journal.
	paused := true
	change := func() {
		t.Helper()
		paused = !paused
		code, data := call(ts, "POST", "triggers/"+trigger.ID+"/state", `{"revision":`+strconv.Itoa(trigger.Revision)+`,"paused":`+strconv.FormatBool(paused)+`,"reviewed":true}`)
		var answer struct{ Trigger struct{ Revision int } }
		if code != 200 || json.Unmarshal(data, &answer) != nil || answer.Trigger.Revision <= trigger.Revision {
			t.Fatalf("state: %d %s", code, data)
		}
		trigger.Revision = answer.Trigger.Revision
	}
	const changes = 2600
	for i := 0; i < changes; i++ {
		change()
	}

	type page struct {
		JournalBegan string         `json:"journalBegan"`
		Items        []journalEntry `json:"items"`
		Next         int            `json:"next"`
		More         bool           `json:"more"`
		Before       *int           `json:"before"`
	}
	read := func(query string) page {
		t.Helper()
		code, data := call(ts, "GET", "jobs/"+job.ID+"/events?"+query, "")
		var p page
		if code != 200 || json.Unmarshal(data, &p) != nil {
			t.Fatalf("?%s: %d %.300s", query, code, data)
		}
		return p
	}
	// The whole journal, read forward as every Desk has read it.
	var forward []int
	for cursor := 0; ; {
		p := read("after=" + strconv.Itoa(cursor))
		if p.Before != nil {
			t.Fatalf("a forward answer carries before: %+v", p)
		}
		forward = append(forward, entriesOf(p.Items)...)
		if !p.More || p.Next <= cursor {
			break
		}
		cursor = p.Next
	}
	if len(forward) < changes || !slices.IsSorted(forward) {
		t.Fatalf("the forward reading holds %d entries", len(forward))
	}

	// The newest entries after one request.
	newest := read("tail=true")
	if len(newest.Items) != 50 || !newest.More || newest.Before == nil || *newest.Before != newest.Items[0].Sequence || newest.Next != newest.Items[49].Sequence || newest.Next != forward[len(forward)-1] {
		t.Fatalf("the newest page: %+v", newest)
	}
	if got := entriesOf(newest.Items); !slices.Equal(got, forward[len(forward)-50:]) {
		t.Fatalf("the newest page holds %v, not the last 50 of the journal", got)
	}

	// Walking back reaches the first entry, each entry once, in order.
	var back [][]int
	for cursor, last := *newest.Before, false; !last; {
		p := read("before=" + strconv.Itoa(cursor))
		if p.Before == nil || len(p.Items) == 0 || *p.Before != p.Items[0].Sequence || p.Next != p.Items[len(p.Items)-1].Sequence || p.Next >= cursor || p.JournalBegan != newest.JournalBegan {
			t.Fatalf("a page before %d: %+v", cursor, p)
		}
		back = append(back, entriesOf(p.Items))
		cursor, last = *p.Before, !p.More
	}
	walked := entriesOf(newest.Items)
	for _, older := range back {
		walked = append(slices.Clone(older), walked...)
	}
	if !slices.Equal(walked, forward) {
		t.Fatalf("walking back read %d entries, the forward reading %d", len(walked), len(forward))
	}
	if walked[0] != forward[0] {
		t.Fatal("walking back did not reach the first entry")
	}

	// Entries that arrive after the backward read are found by its `next`, once, and nothing is repeated.
	for i := 0; i < 7; i++ {
		change()
	}
	arrived := read("after=" + strconv.Itoa(newest.Next))
	if len(arrived.Items) != 7 || arrived.More || arrived.Before != nil || arrived.Items[0].Sequence <= newest.Next || arrived.Next != arrived.Items[6].Sequence {
		t.Fatalf("the entries after the newest page: %+v", arrived)
	}
	// The newest page now ends with them, and the page before a sequence is the same as it was.
	again := read("tail=true")
	if again.Next != arrived.Next || again.Items[len(again.Items)-1].Sequence != arrived.Next {
		t.Fatalf("the newest page after %+v", again)
	}
	if got := read("before=" + strconv.Itoa(*newest.Before)); !slices.Equal(entriesOf(got.Items), back[0]) {
		t.Fatalf("the page before %d changed", *newest.Before)
	}

	// Bounds: before=0 answers an empty page, a before past the end the newest.
	if empty := read("before=0"); len(empty.Items) != 0 || empty.More || empty.Before == nil || *empty.Before != 0 || empty.Next != 0 {
		t.Fatalf("before=0: %+v", empty)
	}
	if beyond := read("before=999999999999999999"); len(beyond.Items) != 50 || beyond.Next != arrived.Next {
		t.Fatalf("before past the end: %+v", beyond)
	}
	// The conflicts, and a tail or a before Runner would refuse, are refused by Desk.
	for _, query := range []string{"tail=true&before=5", "tail=true&after=5", "before=5&after=5", "tail=maybe", "before=-1", "before=5&before=6"} {
		if code, data := call(ts, "GET", "jobs/"+job.ID+"/events?"+query, ""); code != 400 || !strings.Contains(string(data), "journal page") {
			t.Errorf("?%s: %d %s", query, code, data)
		}
	}
}

type journalEntry struct {
	Sequence int    `json:"sequence"`
	Kind     string `json:"kind"`
}

func entriesOf(items []journalEntry) []int {
	out := make([]int, len(items))
	for i, e := range items {
		out[i] = e.Sequence
	}
	return out
}
