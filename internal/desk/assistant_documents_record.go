package desk

// Attachment-v1 consumer contract shared with web/src/documents/record.ts.
// Additional signed members are permitted. Text is checked, never normalized.
import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"
)

type assistantDocumentRecord struct {
	name, mediaType, id string
	size                int
	partial             bool
	pages               []assistantDocumentRecordPage
	raw                 map[string]any
}
type assistantDocumentRecordPage struct {
	text   string
	usable bool
}

func adObject(v any) map[string]any { m, _ := v.(map[string]any); return m }
func adString(v any) string         { s, _ := v.(string); return s }
func adIsString(v any) bool         { _, ok := v.(string); return ok }
func adInt(v any, min int) (int, bool) {
	n, ok := v.(json.Number)
	if !ok {
		return 0, false
	}
	i, e := strconv.ParseInt(string(n), 10, 64)
	return int(i), e == nil && i >= int64(min) && i <= 9007199254740991
}
func adIsInt(v any, min int) bool { _, ok := adInt(v, min); return ok }
func adBool(v any) bool           { _, ok := v.(bool); return ok }
func adArray(v any) ([]any, bool) { a, ok := v.([]any); return a, ok }
func adNullableString(m map[string]any, key string) bool {
	v, ok := m[key]
	return ok && (v == nil || adIsString(v))
}
func adNull(m map[string]any, key string) bool { v, ok := m[key]; return ok && v == nil }
func adDigest(v any) bool {
	return adIsString(v) && regexp.MustCompile(`^sha256:[a-f0-9]{64}$`).MatchString(adString(v))
}
func adMatch(v any, pattern string) bool {
	return adIsString(v) && regexp.MustCompile(pattern).MatchString(adString(v))
}
func adHas(v any, values ...string) bool { return adIsString(v) && contains(values, adString(v)) }
func adNoControls(v string) bool {
	for _, r := range v {
		if r < 32 || r == 127 {
			return false
		}
	}
	return true
}
func adWebURL(v any) bool { return assistantSearchURL(v) }
func adConnected(source map[string]any) bool {
	id, raw := adString(source["resourceId"]), adString(source["url"])
	if !adIsString(source["resourceId"]) || !adIsString(source["url"]) || len(raw) > 4096 {
		return false
	}
	u, e := url.Parse(raw)
	if e != nil || u.User != nil || u.Fragment != "" {
		return false
	}
	switch source["provider"] {
	case "notion":
		return regexp.MustCompile(`^(?:[a-f0-9]{32}|[a-f0-9]{8}-[a-f0-9]{4}-[a-f0-9]{4}-[a-f0-9]{4}-[a-f0-9]{12})$`).MatchString(id) && u.Scheme == "https" && u.Port() == "" && contains([]string{"notion.so", "www.notion.so", "notion.com", "www.notion.com"}, u.Hostname()) && strings.HasSuffix(strings.ReplaceAll(u.Path, "-", ""), strings.ReplaceAll(id, "-", ""))
	case "obsidian":
		if len(id) == 0 || len(id) > 1024 || strings.ContainsAny(id, "\\:") || !adNoControls(id) || !strings.HasSuffix(strings.ToLower(id), ".md") {
			return false
		}
		for _, part := range strings.Split(id, "/") {
			if part == "" || strings.HasPrefix(part, ".") {
				return false
			}
		}
		q, e := url.ParseQuery(u.RawQuery)
		return e == nil && u.Scheme == "obsidian" && u.Host == "open" && u.Path == "" && len(q) == 2 && len(q["vault"]) == 1 && q.Get("vault") != "" && len(q["file"]) == 1 && q.Get("file") == strings.TrimSuffix(id, id[strings.LastIndex(id, "."):])
	}
	return false
}

func readAssistantDocumentRecord(raw []byte) (assistantDocumentRecord, error) {
	var zero assistantDocumentRecord
	var r map[string]any
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	if decoder.Decode(&r) != nil {
		return zero, errAssistantDocument
	}
	d, o, c, p, v := adObject(r["document"]), adObject(r["original"]), adObject(r["content"]), adObject(r["processing"]), adObject(r["provenance"])
	if d == nil || o == nil || c == nil || p == nil || v == nil {
		return zero, errAssistantDocument
	}
	name := adString(d["name"])
	if r["attachmentVersion"] != "1" || !adDigest(d["id"]) || len(name) < 1 || len(name) > 255 || !adNoControls(name) || !adMatch(d["mediaType"], `^[a-z0-9][a-z0-9!#$&^_.+-]{0,126}/[a-z0-9][a-z0-9!#$&^_.+-]{0,126}$`) || (!adNull(d, "detectedMediaType") && !adHas(d["detectedMediaType"], "application/pdf", "text/plain")) || !adIsInt(d["size"], 0) || !adNullableString(d, "version") {
		return zero, errAssistantDocument
	}
	if !adNull(d, "encryption") {
		e := adObject(d["encryption"])
		if e == nil || !adNullableString(e, "handler") || (!adNull(e, "revision") && !adIsInt(e["revision"], 0)) || !adBool(e["opened"]) {
			return zero, errAssistantDocument
		}
	}
	source := adObject(v["source"])
	if source == nil {
		return zero, errAssistantDocument
	}
	if adHas(source["kind"], "google-drive", "gmail", "connected-source", "web", "connection-resource") {
		if o["retention"] != "inline" || o["encoding"] != "base64" || !adIsString(o["bytes"]) {
			return zero, errAssistantDocument
		}
	} else if o["retention"] != "caller" || !adNull(o, "encoding") || !adNull(o, "bytes") {
		return zero, errAssistantDocument
	}
	pages, ok := adArray(c["pages"])
	count, countOK := adInt(c["pageCount"], 0)
	chars, charsOK := adInt(c["chars"], 0)
	errs, errorsOK := adArray(p["errors"])
	if c["kind"] != "text" || !adHas(c["extraction"], "text-layer", "ocr", "mixed", "verbatim", "none") || !countOK || !adBool(c["truncated"]) || !charsOK || !ok || !adHas(p["status"], "complete", "partial", "failed") || !errorsOK || !adIsInt(p["durationMs"], 0) {
		return zero, errAssistantDocument
	}
	bounds := adObject(p["bounds"])
	for _, key := range []string{"maxBytes", "maxPages", "maxTextBytes", "maxInflateBytes", "maxOcrOutputBytes", "timeoutMs"} {
		if !adIsInt(bounds[key], 1) {
			return zero, errAssistantDocument
		}
	}
	maxPages, _ := adInt(bounds["maxPages"], 1)
	maxText, _ := adInt(bounds["maxTextBytes"], 1)
	if len(pages) > 500 || len(pages) > count || count > maxPages || (len(pages) != count && c["truncated"] != true) {
		return zero, errAssistantDocument
	}
	out := assistantDocumentRecord{name: name, mediaType: adString(d["mediaType"]), id: adString(d["id"]), raw: r}
	out.size, _ = adInt(d["size"], 0)
	totalChars, totalBytes := 0, 0
	methods := map[string]bool{}
	incomplete := false
	unmappedAny := false
	ocrPages := []int{}
	for i, item := range pages {
		page := adObject(item)
		number, nOK := adInt(page["number"], 1)
		nChars, chOK := adInt(page["chars"], 0)
		unmapped, uOK := adInt(page["unmapped"], 0)
		text := adString(page["text"])
		if !nOK || number != i+1 || !adHas(page["status"], "ok", "no-text", "needs-ocr", "failed") || !adHas(page["extraction"], "text-layer", "ocr", "verbatim", "none") || !adIsString(page["text"]) || !chOK || !uOK || nChars != utf8.RuneCountInString(text) {
			return zero, errAssistantDocument
		}
		for _, r := range text {
			if (r < 32 && r != '\t' && r != '\n') || r == 127 {
				return zero, errAssistantDocument
			}
		}
		if text != "" && (regexp.MustCompile(`^[ \t]*\n|\n[ \t]*$|^[ \t]+$`).MatchString(text)) {
			return zero, errAssistantDocument
		}
		status, method := adString(page["status"]), adString(page["extraction"])
		if status == "ok" && (text == "" || method == "none") || status != "ok" && text != "" || status == "no-text" && method == "none" || adHas(status, "needs-ocr", "failed") && method != "none" || method != "text-layer" && unmapped != 0 || unmapped > strings.Count(text, "\ufffd") {
			return zero, errAssistantDocument
		}
		if method != "none" {
			methods[method] = true
		}
		if method == "ocr" {
			ocrPages = append(ocrPages, number)
		}
		totalChars += nChars
		totalBytes += len(text)
		incomplete = incomplete || adHas(status, "failed", "needs-ocr")
		unmappedAny = unmappedAny || unmapped > 0
		out.pages = append(out.pages, assistantDocumentRecordPage{text: text, usable: status == "ok" && unmapped == 0})
	}
	method := "none"
	if len(methods) == 1 {
		for key := range methods {
			method = key
		}
	} else if len(methods) == 2 && methods["text-layer"] && methods["ocr"] {
		method = "mixed"
	} else if len(methods) > 1 {
		return zero, errAssistantDocument
	}
	if totalChars != chars || totalBytes > maxText || c["extraction"] != method {
		return zero, errAssistantDocument
	}
	codes := []string{"media-type-unsupported", "media-type-mismatch", "document-over-bound", "document-empty", "pdf-malformed", "pdf-encrypted", "pdf-unsupported", "pdf-page-failed", "pdf-pages-over-bound", "text-over-bound", "stream-over-bound", "timeout", "ocr-not-run", "ocr-failed", "ocr-incomplete", "ocr-timeout"}
	encrypted := false
	openingTimeout := false
	for _, item := range errs {
		e := adObject(item)
		page, pageOK := adInt(e["page"], 1)
		if !adHas(e["code"], codes...) || !adIsString(e["message"]) || len(adString(e["message"])) > 512 || (!adNull(e, "page") && (!pageOK || page > count)) {
			return zero, errAssistantDocument
		}
		if adHas(e["code"], "pdf-unsupported", "pdf-page-failed", "stream-over-bound", "text-over-bound") == adNull(e, "page") {
			return zero, errAssistantDocument
		}
		encrypted = encrypted || e["code"] == "pdf-encrypted"
		openingTimeout = openingTimeout || (len(errs) == 1 && e["code"] == "timeout" && count == 0)
	}
	for _, item := range pages {
		page := adObject(item)
		if page["status"] != "failed" {
			continue
		}
		matches := 0
		for _, item := range errs {
			e := adObject(item)
			if e["page"] == page["number"] && adHas(e["code"], "pdf-page-failed", "pdf-unsupported", "stream-over-bound") {
				matches++
			}
		}
		if matches != 1 {
			return zero, errAssistantDocument
		}
	}
	status := "partial"
	if len(pages) == 0 {
		status = "failed"
	} else if len(errs) == 0 && c["truncated"] == false {
		status = "complete"
	}
	if p["status"] != status || status == "failed" && len(errs) == 0 || incomplete && status == "complete" {
		return zero, errAssistantDocument
	}
	out.partial = status != "complete" || unmappedAny
	if status == "failed" {
		for i := range out.pages {
			out.pages[i].usable = false
		}
	}
	adapter := adObject(v["adapter"])
	if !adIsString(adapter["name"]) || adString(adapter["name"]) == "" || !adIsString(adapter["version"]) || !adDigest(adapter["digest"]) || !adHas(source["kind"], "inline", "google-drive", "gmail", "connected-source", "web", "connection-resource") {
		return zero, errAssistantDocument
	}
	switch source["kind"] {
	case "google-drive":
		if !adMatch(source["fileId"], `^[A-Za-z0-9_-]{1,200}$`) || !adIsString(source["version"]) || source["version"] != d["version"] || !adIsString(source["mediaType"]) || o["retention"] != "inline" {
			return zero, errAssistantDocument
		}
	case "gmail":
		if !adMatch(source["messageId"], `^[a-f0-9]{1,64}$`) || !adMatch(source["threadId"], `^[a-f0-9]{1,64}$`) || !adMatch(source["version"], `^[0-9]{1,32}$`) || source["version"] != d["version"] || source["format"] != "text-export-v1" || d["mediaType"] != "text/plain" || v["processor"] != "adapter-document/text/1" {
			return zero, errAssistantDocument
		}
	case "connected-source":
		if !adConnected(source) || source["version"] != d["version"] || source["version"] != d["id"] || source["format"] != "text-snapshot-v1" || d["mediaType"] != "text/plain" || v["processor"] != "adapter-document/text/1" {
			return zero, errAssistantDocument
		}
	case "connection-resource":
		id := adString(source["resourceId"])
		if !adMatch(source["provider"], `^[a-z][a-z0-9-]{0,47}$`) || len(id) < 1 || len(id) > 4096 || !adNoControls(id) || (source["url"] != "" && (!adWebURL(source["url"]) || strings.Contains(adString(source["url"]), "?"))) || source["version"] != d["version"] || source["version"] != d["id"] || source["format"] != "retained-file-v1" {
			return zero, errAssistantDocument
		}
	case "web":
		if !adWebURL(source["requestedUrl"]) || !adWebURL(source["url"]) || !adDigest(source["responseDigest"]) || source["version"] != d["version"] || source["version"] != d["id"] {
			return zero, errAssistantDocument
		}
		if source["format"] == "static-text-v1" {
			if !adNull(v, "ocr") || source["mediaType"] != "text/html" || d["mediaType"] != "text/plain" || v["processor"] != "adapter-document/text/1" {
				return zero, errAssistantDocument
			}
		} else if source["format"] != "original-v1" || !adHas(source["mediaType"], "text/plain", "application/pdf") || source["mediaType"] != d["mediaType"] || source["responseDigest"] != d["id"] {
			return zero, errAssistantDocument
		}
	}
	if !adMatch(v["observedAt"], `^\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}Z$`) {
		return zero, errAssistantDocument
	}
	if _, e := time.Parse("2006-01-02T15:04:05Z", adString(v["observedAt"])); e != nil {
		return zero, errAssistantDocument
	}
	if !adNull(v, "processor") && !adHas(v["processor"], "adapter-document/pdf/1", "adapter-document/text/1") {
		return zero, errAssistantDocument
	}
	if adNull(v, "processor") && (len(pages) != 0 || !adNull(d, "detectedMediaType")) || v["processor"] == "adapter-document/pdf/1" && (d["mediaType"] != "application/pdf" || d["detectedMediaType"] != "application/pdf") || v["processor"] == "adapter-document/text/1" && (d["mediaType"] == "application/pdf" || d["detectedMediaType"] != "text/plain" || (len(pages) > 0 && c["extraction"] != "verbatim")) {
		return zero, errAssistantDocument
	}
	if adNull(d, "encryption") {
		if encrypted {
			return zero, errAssistantDocument
		}
	} else {
		e := adObject(d["encryption"])
		revision, ok := adInt(e["revision"], 2)
		if v["processor"] != "adapter-document/pdf/1" {
			return zero, errAssistantDocument
		}
		if e["opened"] == true {
			if encrypted || e["handler"] != "Standard" || !ok || revision > 6 {
				return zero, errAssistantDocument
			}
		} else if len(pages) != 0 || encrypted == openingTimeout {
			return zero, errAssistantDocument
		}
	}
	if !adNull(v, "ocr") {
		ocr := adObject(v["ocr"])
		numbers, ok := adArray(ocr["pages"])
		if !adIsString(ocr["program"]) || adString(ocr["program"]) == "" || !adDigest(ocr["digest"]) || !ok || len(numbers) != len(ocrPages) {
			return zero, errAssistantDocument
		}
		for i, n := range numbers {
			number, ok := adInt(n, 1)
			if !ok || number > count || number != ocrPages[i] || (i > 0 && number <= ocrPages[i-1]) {
				return zero, errAssistantDocument
			}
		}
	} else if methods["ocr"] {
		return zero, errAssistantDocument
	}
	return out, nil
}

func assistantDocumentOriginalBinding(record assistantDocumentRecord, object attachmentObject) bool {
	original, p := object.Original, object.Proof
	decoded, e := base64.StdEncoding.Strict().DecodeString(original.Bytes)
	if e != nil || record.id != original.SHA256 || record.name != original.Name || record.mediaType != original.MediaType || record.size != len(decoded) {
		return false
	}
	source := adObject(adObject(record.raw["provenance"])["source"])
	retained := adObject(record.raw["original"])
	if p.Drive != nil {
		return adMatch(p.Drive.Grant, `^[a-f0-9]{64}$`) && source["kind"] == "google-drive" && source["fileId"] == p.Drive.FileID && retained["bytes"] == original.Bytes
	}
	if p.Gmail != nil {
		return adMatch(p.Gmail.Grant, `^[a-f0-9]{64}$`) && p.Source == "gmail" && source["kind"] == "gmail" && source["messageId"] == p.Gmail.MessageID && retained["bytes"] == original.Bytes
	}
	if p.Connected != nil {
		return adMatch(p.Connected.Grant, `^[a-f0-9]{64}$`) && contains([]string{"notion", "obsidian"}, p.Source) && source["kind"] == "connected-source" && source["provider"] == p.Source && source["resourceId"] == p.Connected.ResourceID && retained["bytes"] == original.Bytes
	}
	if p.Resource != nil {
		return adMatch(p.Resource.Grant, `^[a-f0-9]{64}$`) && source["kind"] == "connection-resource" && source["provider"] == p.Source && source["resourceId"] == p.Resource.ResourceID && retained["bytes"] == original.Bytes
	}
	if p.Web != nil {
		if p.Source != "web" || source["kind"] != "web" || source["requestedUrl"] != p.Web.URL || retained["bytes"] != original.Bytes {
			return false
		}
		if p.Web.Site != "" {
			if !adWebURL(p.Web.Site) {
				return false
			}
			if !assistantSameOrigin(p.Web.URL, p.Web.Site) || !assistantSameOrigin(adString(source["url"]), p.Web.Site) {
				return false
			}
		}
		return true
	}
	return source["kind"] == "inline"
}
