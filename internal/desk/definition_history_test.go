package desk

import (
	"bytes"
	"fmt"
	"strings"
	"testing"
)

func TestDefinitionHistoryExactBytesAndFiltering(t *testing.T) {
	one := `{"recordVersion":"1", "pack":{"id":"one"},"inputs":{"facts":{"n":9007199254740993,"label":"<&>"}}}`
	other := `{"recordVersion":"1","pack":{"id":"two"}}`
	graph := `{"recordVersion":"1","pack":{"id":"one"},"graph":{"id":"graph","node":"a"}}`
	data := []byte(one + "\n" + other + "\n" + graph + "\n" + `{"torn":`)
	got, err := readDefinitionRecords(bytes.NewReader(data), int64(len(data)), -1, "pack", "one")
	if err != nil || len(got.Records) != 2 || got.Records[1].Raw != one || got.Records[0].Raw != graph || got.Unread != 1 {
		t.Fatalf("%+v %v", got, err)
	}
	g, err := readDefinitionRecords(bytes.NewReader(data), int64(len(data)), -1, "graph", "graph")
	if err != nil || len(g.Records) != 1 || g.Records[0].Raw != graph {
		t.Fatalf("%+v %v", g, err)
	}
}
func TestDefinitionHistoryPaging(t *testing.T) {
	var text strings.Builder
	for i := 0; i < 105; i++ {
		fmt.Fprintf(&text, "{\"recordVersion\":\"1\",\"pack\":{\"id\":\"one\"},\"n\":%d}\n", i)
	}
	data := []byte(text.String())
	before := int64(-1)
	count := 0
	seen := map[string]bool{}
	for {
		page, err := readDefinitionRecords(bytes.NewReader(data), int64(len(data)), before, "pack", "one")
		if err != nil {
			t.Fatal(err)
		}
		for _, record := range page.Records {
			if seen[record.Digest] {
				t.Fatal("duplicate")
			}
			seen[record.Digest] = true
			count++
		}
		if page.Next == nil {
			break
		}
		if before >= 0 && *page.Next >= before {
			t.Fatal("no progress")
		}
		before = *page.Next
	}
	if count != 105 {
		t.Fatal(count)
	}
	if _, err := readDefinitionRecords(bytes.NewReader(data), int64(len(data)), int64(len(data)+1), "pack", "one"); err == nil {
		t.Fatal("accepted a truncated trail")
	}
}
func TestDefinitionHistoryBudgetContinuesAtWholeLine(t *testing.T) {
	line := `{"recordVersion":"1","pack":{"id":"one"},"pad":"` + strings.Repeat("a", int(historyReadBytes/3)) + `"}` + "\n"
	data := []byte(strings.Repeat(line, 4))
	first, err := readDefinitionRecords(bytes.NewReader(data), int64(len(data)), -1, "pack", "one")
	if err != nil || first.Next == nil || len(first.Records) != 2 {
		t.Fatalf("%d %v", len(first.Records), err)
	}
	next, err := readDefinitionRecords(bytes.NewReader(data), int64(len(data)), *first.Next, "pack", "one")
	if err != nil || len(next.Records) != 2 || next.Next != nil {
		t.Fatalf("%d %v", len(next.Records), err)
	}
}
