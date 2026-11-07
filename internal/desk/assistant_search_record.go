package desk

// Search leads are authenticated acquisition data, never page evidence.
import (
	"bytes"
	"encoding/json"
	"errors"
	"strings"
	"time"

	runcontext "github.com/Judgment-Pack/judgment-pack-agent/go/context"
	"github.com/Judgment-Pack/judgment-pack-agent/go/failure"
)

const assistantSearchTool = "search_sources"
const assistantSearchResultLimit = 1 << 20

// An acquisition contains at most a one-MiB canonical result plus the existing
// receipt envelope bound. JSON escaping can expand each result byte six times.
const assistantSearchResponseLimit = 6*assistantSearchResultLimit + (64 << 10)
const assistantSearchSealLimit = 64 << 10

var errAssistantSearch = errors.New("Search results could not be verified. No new links were authorized.")

type assistantSearchRequest struct {
	Connection string `json:"connection"`
	Revision   string `json:"revision"`
	Query      string `json:"query"`
	MaxResults int    `json:"maxResults"`
}
type assistantSearchReference struct {
	ID      string                 `json:"id"`
	Digest  string                 `json:"digest"`
	Request assistantSearchRequest `json:"request"`
}
type assistantSearchStep struct {
	Query     string                    `json:"query"`
	Provider  string                    `json:"provider,omitempty"`
	Submitted bool                      `json:"submitted,omitempty"`
	Reference *assistantSearchReference `json:"reference,omitempty"`
}
type assistantSearchHit struct {
	Title   string `json:"title"`
	URL     string `json:"url"`
	Snippet string `json:"snippet"`
}
type assistantSearchResult struct {
	Provider string               `json:"provider"`
	Kind     string               `json:"kind"`
	Hits     []assistantSearchHit `json:"hits"`
}

func readAssistantSearchQuery(raw []byte) (string, error) {
	var args struct {
		Query *string `json:"query"`
	}
	fields, err := configMembers(raw)
	_, unicodeErr := assistantChatValue(raw)
	if len(raw) > 12*2000+64 || !validUTF8(raw) || !uniqueJSON(raw) || err != nil || unicodeErr != nil || len(fields) != 1 || fields[0].name != "query" || assistantHostArguments(raw, &args) != nil || args.Query == nil {
		return "", failure.ToolInput("Supply one search query, at most 2000 characters.")
	}
	// ECMAScript trim includes BOM and excludes U+0085; Go's TrimSpace differs.
	query := strings.TrimFunc(*args.Query, func(r rune) bool {
		return strings.ContainsRune("\u0009\u000a\u000b\u000c\u000d\u0020\u00a0\u1680\u2000\u2001\u2002\u2003\u2004\u2005\u2006\u2007\u2008\u2009\u200a\u2028\u2029\u202f\u205f\u3000\ufeff", r)
	})
	if query == "" || runcontext.U16Length(query) > 2000 {
		return "", failure.ToolInput("Supply one search query, at most 2000 characters.")
	}
	return query, nil
}
func validAssistantSearchRequest(q assistantSearchRequest) bool {
	return researchSourceName.MatchString(q.Connection) && researchConnectionRevision.MatchString(q.Revision) && q.Query != "" && runcontext.U16Length(q.Query) <= 2000 && q.MaxResults == 5
}
func validAssistantSearchReference(ref assistantSearchReference) bool {
	return attachmentID.MatchString(ref.ID) && adDigest(ref.Digest) && validAssistantSearchRequest(ref.Request)
}

// Match validateSearch, including signed extensions, UTF-16 string bounds and
// the browser's URL spelling checks. DNS/redirect policy stays at the Gateway.
func readAssistantSearchResult(raw []byte, q assistantSearchRequest) (assistantSearchResult, error) {
	var out assistantSearchResult
	if len(raw) > assistantSearchResultLimit || !validUTF8(raw) || !uniqueJSON(raw) {
		return out, errAssistantSearch
	}
	value, err := assistantChatValue(raw)
	if err != nil || !value.IsObject() {
		return out, errAssistantSearch
	}
	d := adObject(nil)
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	if decoder.Decode(&d) != nil || !adIsInt(d["version"], 1) || d["version"].(json.Number) != "1" || adString(d["connection"]) != q.Connection || adString(d["revision"]) != q.Revision || adString(d["query"]) != q.Query || !adMatch(d["provider"], `^[a-z][a-z0-9-]{0,47}$`) || !adHas(d["kind"], "search-results", "grounded-answer") {
		return out, errAssistantSearch
	}
	if !adIsString(d["retrievedAt"]) || !assistantSearchDate(adString(d["retrievedAt"])) {
		return out, errAssistantSearch
	}
	hits, ok := adArray(d["hits"])
	if !ok || len(hits) > q.MaxResults {
		return out, errAssistantSearch
	}
	seen := map[string]bool{}
	for _, v := range hits {
		h := adObject(v)
		u := adString(h["url"])
		if !assistantSearchURL(h["url"]) || runcontext.U16Length(u) > 4096 || seen[u] || !adIsString(h["title"]) || runcontext.U16Length(adString(h["title"])) > 512 || !adIsString(h["snippet"]) || runcontext.U16Length(adString(h["snippet"])) > 2000 {
			return out, errAssistantSearch
		}
		seen[u] = true
	}
	for key, limit := range map[string]int{"generatedAnswer": 12000, "attributionHtml": 32768} {
		if v, exists := d[key]; exists && (adString(d["kind"]) != "grounded-answer" || !adIsString(v) || runcontext.U16Length(adString(v)) > limit) {
			return out, errAssistantSearch
		}
	}
	if v, exists := d["queries"]; exists {
		queries, ok := adArray(v)
		if !ok || len(queries) > 20 {
			return out, errAssistantSearch
		}
		for _, q := range queries {
			if !adIsString(q) || runcontext.U16Length(adString(q)) > 2000 {
				return out, errAssistantSearch
			}
		}
	}
	if json.Unmarshal(raw, &out) != nil {
		return out, errAssistantSearch
	}
	return out, nil
}
func assistantSearchURL(value any) bool {
	if !adIsString(value) {
		return false
	}
	_, err := assistantPublicURL(adString(value))
	return err == nil
}

// The pinned Gateway search producer writes UTC RFC3339 (time.RFC3339).
// This private host deliberately refuses legacy Date.parse formats even though
// old browser-only searches may still display them. Signed bytes are unchanged.
func assistantSearchDate(raw string) bool {
	_, err := time.Parse(time.RFC3339Nano, raw)
	return err == nil
}
