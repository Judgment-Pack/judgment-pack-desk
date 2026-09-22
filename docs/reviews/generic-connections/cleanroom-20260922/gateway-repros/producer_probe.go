//go:build ignore

package main

import (
	"adapters/attachment"
	"adapters/connections"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

func must(ok bool, detail string) {
	if !ok {
		panic(detail)
	}
}
func main() {
	root := "/tmp/jp-foundation-cleanroom-20260922"
	output := filepath.Join(root, "gateway-repros", "records")
	for _, media := range []string{"text/plain", "text/markdown", "text/csv", "application/json"} {
		name := strings.ReplaceAll(media, "/", "-")
		raw, err := connections.ResourceDocument(context.Background(), "fixture-files", "folder/"+name, name, media, "", []byte("\xef\xbb\xbf\r\nPolicy line 1.\r\nPolicy line 2.\n"))
		must(err == nil, fmt.Sprint(media, err))
		must(attachment.Check(raw) == nil, "checker")
		must(os.WriteFile(filepath.Join(output, name+".json"), raw, 0600) == nil, "save")
		fmt.Println("PASS text producer", media)
	}
	for _, file := range []string{"normal.pdf", "mixed.pdf", "scanned.pdf", "encrypted-user.pdf", "malformed-truncated.pdf", "inflate-bomb.pdf", "many-pages.pdf"} {
		data, err := os.ReadFile(filepath.Join(root, "gateway", "adapters", "document", "testdata", file))
		must(err == nil, "read PDF")
		raw, err := connections.ResourceDocument(context.Background(), "fixture-files", "folder/"+file, file, "application/pdf", "https://example.com/policy", data)
		must(err == nil, fmt.Sprint(file, err))
		must(attachment.Check(raw) == nil, "PDF checker")
		var rec attachment.Record
		must(json.Unmarshal(raw, &rec) == nil, "decode")
		must(rec.Provenance.OCR == nil, "OCR enabled unexpectedly")
		must(os.WriteFile(filepath.Join(output, file+".json"), raw, 0600) == nil, "save PDF")
		fmt.Printf("PASS PDF producer %s status=%s pages=%d\n", file, rec.Processing.Status, len(rec.Content.Pages))
	}
	for _, size := range []int{0, connections.MaxFileBytes, connections.MaxFileBytes + 1} {
		_, err := connections.ResourceDocument(context.Background(), "fixture-files", "id", "policy.txt", "text/plain", "", []byte(strings.Repeat("x", size)))
		must((err == nil) == (size == connections.MaxFileBytes), fmt.Sprint("size", size, err))
		fmt.Println("PASS byte limit", size)
	}
	for _, url := range []string{"https://user:pass@example.com/file", "https://example.com/file?token=secret", "https://example.com/file#token", "file:///private", "https://example.com/?"} {
		_, err := connections.ResourceDocument(context.Background(), "fixture-files", "id", "policy.txt", "text/plain", url, []byte("Policy"))
		must(err != nil, "unsafe display URL admitted: "+url)
	}
	fmt.Println("PASS unsafe URL refusals")
	page := connections.ResourcePage{SelectionContext: "epoch", Items: []connections.ResourceItem{}, More: true, NextPageToken: "page-2"}
	must(connections.ValidateResourcePage(page) == nil, "empty continuing page")
	page.More = false
	must(connections.ValidateResourcePage(page) != nil, "cursor mismatch")
	page.NextPageToken = ""
	page.Items = []connections.ResourceItem{{ID: "id", Title: "Policy", URL: "", UnavailableReason: "archived"}}
	must(connections.ValidateResourcePage(page) == nil, "archived page")
	page.Items = append(page.Items, page.Items[0])
	must(connections.ValidateResourcePage(page) != nil, "duplicates")
	fmt.Println("PASS page boundary refusals")
}
