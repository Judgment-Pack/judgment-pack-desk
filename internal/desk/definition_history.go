package desk

// History is a bounded read of retained records, not a verifier or a second
// audit writer. Exact record text travels separately from its display fields.
import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"io/fs"
	"net/http"
	"net/url"
	"strconv"
	"unicode/utf8"
)

const historyReadBytes int64 = 4 << 20

type definitionRecord struct {
	Raw    string `json:"raw"`
	Digest string `json:"digest"`
}
type definitionRecords struct {
	State   string             `json:"state"`
	Records []definitionRecord `json:"records"`
	Next    *int64             `json:"next,omitempty"`
	Unread  int                `json:"unread"`
}

func readDefinitionRecords(reader io.ReaderAt, size, before int64, kind, id string) (definitionRecords, error) {
	out := definitionRecords{State: "records", Records: []definitionRecord{}}
	end := size
	if before >= 0 {
		end = before
	}
	if end > size || end < 0 {
		return out, errors.New("The retained trail changed. Reload History.")
	}
	if end == 0 {
		return out, nil
	}
	start := end - historyReadBytes
	if start < 0 {
		start = 0
	}
	data := make([]byte, end-start)
	if _, err := reader.ReadAt(data, start); err != nil {
		return out, err
	}
	if start > 0 {
		i := bytes.IndexByte(data, '\n')
		if i < 0 {
			return out, errors.New("A record exceeds the History reading limit. Download the trail from Decision safeguards.")
		}
		start += int64(i + 1)
		data = data[i+1:]
	}
	if len(data) > 0 && data[len(data)-1] != '\n' {
		// Do not turn an interrupted append into a completed decision.
		out.Unread++
		i := bytes.LastIndexByte(data, '\n')
		data = data[:i+1]
	}
	offset := start + int64(len(data))
	lines := bytes.Split(data, []byte{'\n'})
	for i := len(lines) - 2; i >= 0; i-- {
		line := lines[i]
		offset -= int64(len(line) + 1)
		var record struct {
			RecordVersion string `json:"recordVersion"`
			Pack          *struct {
				ID string `json:"id"`
			} `json:"pack"`
			Graph *struct {
				ID string `json:"id"`
			} `json:"graph"`
		}
		if len(line) == 0 || !utf8.Valid(line) || json.Unmarshal(line, &record) != nil {
			out.Unread++
			continue
		}
		if record.RecordVersion != "1" {
			out.Unread++
			continue
		}
		matches := kind == "pack" && record.Pack != nil && record.Pack.ID == id || kind == "graph" && record.Graph != nil && record.Graph.ID == id
		if !matches {
			continue
		}
		digest := sha256.Sum256(line)
		out.Records = append(out.Records, definitionRecord{Raw: string(line), Digest: "sha256:" + hex.EncodeToString(digest[:])})
		if len(out.Records) == 50 {
			if offset > 0 {
				out.Next = &offset
			}
			return out, nil
		}
	}
	if start > 0 {
		out.Next = &start
	}
	return out, nil
}

func (s *Server) handleDefinitionHistory(w http.ResponseWriter, r *http.Request) {
	if !s.guard(w, r) {
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	q, err := url.ParseQuery(r.URL.RawQuery)
	kind, id := q.Get("kind"), q.Get("id")
	before := int64(-1)
	valid := err == nil && (kind == "pack" || kind == "graph") && id != "" && len(id) <= 2048
	for k, v := range q {
		if (k != "kind" && k != "id" && k != "before") || len(v) != 1 {
			valid = false
		}
	}
	if q.Has("before") {
		before, err = strconv.ParseInt(q.Get("before"), 10, 64)
		valid = valid && err == nil && before >= 0
	}
	if !valid {
		writeJSONCoded(w, http.StatusBadRequest, CodeBadRequest, "Choose one pack or graph identity and a valid history cursor.")
		return
	}
	dir, declared, err := s.projectAuditDir()
	if err != nil {
		writeJSONCoded(w, http.StatusInternalServerError, CodeInternal, "The decision history configuration could not be read.")
		return
	}
	if !declared {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(definitionRecords{State: "no-trail", Records: []definitionRecord{}})
		return
	}
	parts, err := auditDirParts(dir)
	if err != nil {
		writeJSONCoded(w, http.StatusForbidden, CodeOutsideRoot, "The audit directory is outside the readable project.")
		return
	}
	root, err := s.openAuditDir(parts)
	var snapshot auditSnapshot
	if err == nil {
		defer root.Close()
		snapshot, err = snapshotAuditFile(r.Context(), root, "evaluations")
	}
	if errors.Is(err, fs.ErrNotExist) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(definitionRecords{State: "no-records", Records: []definitionRecord{}})
		return
	}
	if err != nil {
		status, code, message := auditTrailRefusal(err, "evaluations.jsonl")
		writeJSONCoded(w, status, code, message)
		return
	}
	defer snapshot.file.Close()
	result, err := readDefinitionRecords(snapshot.file, snapshot.size, before, kind, id)
	if err != nil {
		writeJSONCoded(w, http.StatusConflict, CodeStale, "Decision history could not be read completely. Reload History or download the trail from Decision safeguards.")
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(result)
}
