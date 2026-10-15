// Package aifailure defines the provider-independent failure contract shared with Desk's UI.
package aifailure

import (
	"embed"
	"encoding/json"
	"net/http"
	"regexp"
	"strconv"
	"time"
)

//go:embed catalog.json
var files embed.FS
var catalog struct {
	Failures map[string]struct {
		Message  string `json:"message"`
		Recovery string `json:"recovery"`
	} `json:"failures"`
	Aliases map[string]string `json:"aliases"`
}

func init() {
	data, _ := files.ReadFile("catalog.json")
	if err := json.Unmarshal(data, &catalog); err != nil {
		panic(err)
	}
}

// Completion carries normalized counters and stop reasons, never model prose.
type Completion struct {
	FinishReason    string `json:"finishReason"`
	InputTokens     *int64 `json:"inputTokens,omitempty"`
	OutputTokens    *int64 `json:"outputTokens,omitempty"`
	ReasoningTokens *int64 `json:"reasoningTokens,omitempty"`
}

type Failure struct {
	Completion *Completion `json:"completion,omitempty"`
	Detail     string      `json:"detail,omitempty"`
	Version    int         `json:"version"`
	Code       string      `json:"code"`
	Source     string      `json:"source"`
	Outcome    string      `json:"outcome"`
	Status     int         `json:"status,omitempty"`
	RequestID  string      `json:"requestId,omitempty"`
	RetryAt    string      `json:"retryAt,omitempty"`
}

func New(code, source, outcome string) *Failure {
	if _, ok := catalog.Failures[code]; !ok {
		code = "unknown"
	}
	return &Failure{Version: 1, Code: code, Source: source, Outcome: outcome}
}

// Authored is only for explicit host validation, never caught provider prose.
func Authored(code, source, outcome, detail string) *Failure {
	f := New(code, source, outcome)
	if len(detail) <= 1000 {
		f.Detail = detail
	}
	return f
}
func (f *Failure) Error() string {
	if f.Detail != "" {
		return f.Detail
	}
	return catalog.Failures[f.Code].Message
}
func (f *Failure) Recovery() string { return catalog.Failures[f.Code].Recovery }

var requestID = regexp.MustCompile(`^[-a-zA-Z0-9_]{1,128}$`)

// HTTP retains only allowlisted metadata. A failed response never proves a workflow safe to replay.
func HTTP(status int, headers http.Header, source string, now time.Time) *Failure {
	code := "unknown"
	outcome := "unknown"
	switch {
	case status == 401:
		code = "authentication"
	case status == 403:
		code = "permission"
	case status == 429:
		code = "rate-limited"
	case status == 408 || status == 504:
		code = "timeout"
	case status == 413:
		code = "context-limit"
	case status == 409:
		code = "stale-state"
	case status >= 500 && status <= 599:
		code = "unavailable"
	case status >= 400 && status <= 499:
		code = "invalid-request"
	}
	if status >= 400 && status < 500 && status != 408 {
		outcome = "refused"
	}
	f := New(code, source, outcome)
	if status >= 100 && status <= 599 {
		f.Status = status
	}
	id := headers.Get("request-id")
	if id == "" {
		id = headers.Get("x-request-id")
	}
	if requestID.MatchString(id) {
		f.RequestID = id
	}
	after := headers.Get("retry-after")
	delay, err := strconv.ParseFloat(after, 64)
	if err != nil {
		if date, e := http.ParseTime(after); e == nil {
			delay = date.Sub(now).Seconds()
			err = nil
		}
	}
	if err == nil && delay >= 0 && delay < 1e15 {
		f.RetryAt = now.Add(time.Duration(min(delay, 86400) * float64(time.Second))).UTC().Format(time.RFC3339Nano)
	}
	return f
}

// FromCode accepts only a stable, catalogue-listed code, never provider prose.
func FromCode(code, source, outcome string) *Failure {
	if mapped, ok := catalog.Aliases[code]; ok {
		code = mapped
	}
	return New(code, source, outcome)
}

// HTTPResponse reads only bounded stable codes; the raw body is never retained.
func HTTPResponse(status int, headers http.Header, raw []byte, source string, now time.Time) *Failure {
	f := HTTP(status, headers, source, now)
	if len(raw) > 16384 {
		return f
	}
	var body map[string]any
	if json.Unmarshal(raw, &body) != nil {
		return f
	}
	nested, _ := body["error"].(map[string]any)
	for _, v := range []any{nested["code"], nested["type"], nested["status"], body["code"]} {
		if code, ok := v.(string); ok {
			if mapped, known := catalog.Aliases[code]; known {
				f.Code = mapped
				break
			}
		}
	}
	return f
}
