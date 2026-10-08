package desk

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// processingRig is a desk whose local gateway and document-processing
// companion are stand-ins in a directory of their own, outside the desk's.
// The companion writes down every line it is sent and its length, and answers
// each method with the member the test put in place. The worker announces a
// gateway whose plan gives the document sources the processing envelope while
// the file `plan-processing` exists, and writes down each start.
type processingRig struct {
	s   *Server
	ts  *httptest.Server
	log *bytes.Buffer
	dir string
}

const processingRigDigest = "sha256:" + "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"

func newProcessingRig(t *testing.T) *processingRig {
	t.Helper()
	s, ts, logged := assistantServer(t)
	dir, bundle := t.TempDir(), t.TempDir()
	companion := `#!/bin/sh
exec /usr/bin/awk -v dir='` + dir + `' '{
	f = dir "/lengths"; print length($0) >> f; close(f)
	f = dir "/lines"; print $0 >> f; close(f)
	match($0, /"id":"[^"]*"/); id = substr($0, RSTART + 6, RLENGTH - 7)
	match($0, /"method":"[^"]*"/); method = substr($0, RSTART + 10, RLENGTH - 11)
	answer = ""; f = dir "/answer-" method
	while ((getline part < f) > 0) answer = answer part
	close(f)
	printf "{\"id\":\"%s\",%s}\n", id, answer
	fflush()
}'
`
	files := map[string]string{}
	for name, content := range map[string]string{"gateway": "gateway", "adapter-document": "adapter-document", "gateway-connections": companion} {
		if err := os.WriteFile(filepath.Join(bundle, executableName(name)), []byte(content), 0o700); err != nil {
			t.Fatal(err)
		}
		sum := sha256.Sum256([]byte(content))
		files[executableName(name)] = hex.EncodeToString(sum[:])
	}
	manifest, _ := json.Marshal(map[string]any{"revision": GatewayRevision, "files": files})
	if err := os.WriteFile(filepath.Join(bundle, "gateway-bundle.json"), manifest, 0o600); err != nil {
		t.Fatal(err)
	}
	worker := filepath.Join(dir, "worker")
	script := `#!/bin/sh
IFS= read -r options
public=$(printf '%s\n' "$options" | /usr/bin/sed -n 's/.*"Public":"\([0-9a-f]*\)".*/\1/p')
processing=false
if [ -f '` + dir + `/plan-processing' ]; then processing=true; fi
echo start >> '` + dir + `/starts'
printf '{"url":"http://127.0.0.1:9","authority":"gateway:desk-local","signer":{"algorithm":"ed25519","public":"%s"},"documentProcessing":%s}\n' "$public" "$processing"
exec /bin/cat > /dev/null
`
	if err := os.WriteFile(worker, []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}
	s.localGateway = &localGateway{bundle: bundle, executable: worker}
	rig := &processingRig{s: s, ts: ts, log: logged, dir: dir}
	rig.answer(t, "status", `"result":`+processingSettings("off", ""))
	return rig
}

// processingSettings is a companion's status with one connection.
func processingSettings(mode, connection string) string {
	return `{"version":1,"mode":"` + mode + `","connection":"ocr-work","connections":[{"id":"ocr-work","name":"Work scans","kind":"azure-document-intelligence",` +
		`"enabled":true,"ready":true,"endpoint":"https://work.cognitiveservices.azure.com"` + connection + `}],"timeoutSeconds":60,"sha256":"` + processingRigDigest + `","state":"ready"}`
}

func (r *processingRig) answer(t *testing.T, method, member string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(r.dir, "answer-"+method), []byte(member), 0o600); err != nil {
		t.Fatal(err)
	}
}

// sent is every line the companion was sent.
func (r *processingRig) sent() string {
	raw, _ := os.ReadFile(filepath.Join(r.dir, "lines"))
	return string(raw)
}

func (r *processingRig) starts() int {
	raw, _ := os.ReadFile(filepath.Join(r.dir, "starts"))
	return strings.Count(string(raw), "start")
}

func (r *processingRig) post(t *testing.T, path string, body []byte) (int, []byte) {
	t.Helper()
	req, err := http.NewRequest(http.MethodPost, r.ts.URL+path, bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	bearer(req)
	req.Header.Set("Content-Type", "application/json")
	resp, err := r.ts.Client().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var out bytes.Buffer
	_, _ = out.ReadFrom(resp.Body)
	return resp.StatusCode, out.Bytes()
}

// processingAnswered is the page's view of one answer.
type processingAnswered struct {
	Result       json.RawMessage `json:"result"`
	Error        string          `json:"error"`
	LocalGateway struct {
		Status             string `json:"status"`
		DocumentProcessing *bool  `json:"documentProcessing"`
		Restarted          bool   `json:"restarted"`
	} `json:"localGateway"`
}

func decodeProcessing(t *testing.T, raw []byte) processingAnswered {
	t.Helper()
	var answered processingAnswered
	if err := json.Unmarshal(raw, &answered); err != nil {
		t.Fatalf("%v: %s", err, raw)
	}
	return answered
}

// The key the tests enter: an Azure resource key, as the owner would paste it.
const processingKey = "azure-key-5f1d0c7e9b2a4c68a1e3d7f0b9c2e4a6"

func processingConfigure(mode, credential string) []byte {
	member := ""
	if credential != "" {
		member = `,"credential":"` + credential + `"`
	}
	return []byte(`{"ifMatch":"` + processingRigDigest + `","config":{"version":1,"mode":"` + mode + `","connection":"ocr-work","connections":[{"id":"ocr-work",` +
		`"name":"Work scans","kind":"azure-document-intelligence","enabled":true,"endpoint":"https://work.cognitiveservices.azure.com"` + member + `}],"timeoutSeconds":60}}`)
}

func TestDocumentProcessingRefusesWithoutASessionAnUnknownOperationOrAQuery(t *testing.T) {
	rig := newProcessingRig(t)
	resp, err := rig.ts.Client().Post(rig.ts.URL+"/api/document-processing/status", "application/json", strings.NewReader(`{}`))
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode == http.StatusOK {
		t.Fatal("the settings were answered without a session")
	}
	for _, path := range []string{"/api/document-processing/delete", "/api/document-processing/configure?credential=" + processingKey} {
		if code, raw := rig.post(t, path, processingConfigure("off", processingKey)); code != http.StatusBadRequest || bytes.Contains(raw, []byte(processingKey)) {
			t.Fatalf("%s: %d %s", path, code, raw)
		}
	}
	if rig.sent() != "" {
		t.Fatal("a refused request reached the companion")
	}
}

// The request that carries a credential goes only to this desk's own local
// gateway's companion: never while the desk file names another gateway, and
// never when there is no local gateway.
func TestDocumentProcessingIsNeverSentWhileAnotherGatewayIsInUse(t *testing.T) {
	rig := newProcessingRig(t)
	writeDeskConfig(t, rig.s, researchDeskFile("http://127.0.0.1:1"))
	for _, method := range []string{"status", "configure", "test"} {
		code, raw := rig.post(t, "/api/document-processing/"+method, processingConfigure("auto", processingKey))
		if code != http.StatusConflict || bytes.Contains(raw, []byte(processingKey)) {
			t.Fatalf("%s: %d %s", method, code, raw)
		}
	}
	rig.s.localGateway = nil
	if code, _ := rig.post(t, "/api/document-processing/configure", processingConfigure("auto", processingKey)); code != http.StatusConflict {
		t.Fatalf("no local gateway: %d", code)
	}
	if rig.sent() != "" || rig.starts() != 0 {
		t.Fatal("a request reached a companion while another gateway is in use")
	}
}

// What the page is shown is rebuilt from the members Desk names: a
// credential the companion answers is not among them, nor anything else it
// adds; a refusal is its word from a closed list; and an answer that holds a
// credential the request carried is refused whole.
func TestDocumentProcessingAnswersCarryNoCredential(t *testing.T) {
	rig := newProcessingRig(t)
	rig.answer(t, "status", `"result":`+processingSettings("off", `,"credential":"`+processingKey+`","credentialConfigured":true,"secret":"`+processingKey+`"`))
	code, raw := rig.post(t, "/api/document-processing/status", []byte(`{}`))
	if code != http.StatusOK || bytes.Contains(raw, []byte(processingKey)) || bytes.Contains(raw, []byte(`"credential"`)) || !bytes.Contains(raw, []byte(`"credentialConfigured":true`)) {
		t.Fatalf("status: %d %s", code, raw)
	}
	// The key carried by a save, answered back in another member.
	rig.answer(t, "configure", `"result":`+strings.Replace(processingSettings("off", ""), "https://work.cognitiveservices.azure.com", processingKey, 1))
	code, raw = rig.post(t, "/api/document-processing/configure", processingConfigure("off", processingKey))
	if code != http.StatusBadGateway || bytes.Contains(raw, []byte(processingKey)) {
		t.Fatalf("an answer holding the key: %d %s", code, raw)
	}
	// A refusal Desk does not name, holding the key.
	rig.answer(t, "configure", `"error":"failed `+processingKey+`"`)
	code, raw = rig.post(t, "/api/document-processing/configure", processingConfigure("off", processingKey))
	if answered := decodeProcessing(t, raw); code != http.StatusOK || answered.Error != "processing-failed" || bytes.Contains(raw, []byte(processingKey)) {
		t.Fatalf("an unnamed refusal: %d %s", code, raw)
	}
	rig.answer(t, "configure", `"error":"processing-changed"`)
	if _, raw = rig.post(t, "/api/document-processing/configure", processingConfigure("off", processingKey)); decodeProcessing(t, raw).Error != "processing-changed" {
		t.Fatalf("a named refusal: %s", raw)
	}
	// The key reached the companion, and only by the lines it was sent.
	if strings.Count(rig.sent(), processingKey) != 3 {
		t.Fatalf("the companion was not sent the key with each save: %q", rig.sent())
	}
}

// A credential passes through Desk and stays nowhere in it: not in its log,
// not in its configuration or storage, not in the project.
func TestDocumentProcessingKeepsNoCredential(t *testing.T) {
	rig := newProcessingRig(t)
	rig.answer(t, "configure", `"result":`+processingSettings("off", `,"credentialConfigured":true`))
	code, raw := rig.post(t, "/api/document-processing/configure", processingConfigure("off", processingKey))
	if code != http.StatusOK || bytes.Contains(raw, []byte(processingKey)) {
		t.Fatalf("%d %s", code, raw)
	}
	if !strings.Contains(rig.sent(), processingKey) {
		t.Fatal("the save did not reach the companion")
	}
	if strings.Contains(rig.log.String(), processingKey) || strings.Contains(rig.log.String(), "work.cognitiveservices") {
		t.Fatalf("the log holds the request: %s", rig.log.String())
	}
	for _, root := range []string{rig.s.configDir, rig.s.assistant.dir, rig.s.projectDir} {
		_ = filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
			if err != nil || entry.IsDir() {
				return nil
			}
			if raw, err := os.ReadFile(path); err == nil && bytes.Contains(raw, []byte(processingKey)) {
				t.Fatalf("the key was written to %s", path)
			}
			return nil
		})
	}
}

// Desk holds each request's line to what the companion reads for it: a line
// at the bound is sent; one byte more is refused, and the companion is sent
// none of it.
func TestDocumentProcessingRequestLinesAreHeldToTheCompanionsBound(t *testing.T) {
	rig := newProcessingRig(t)
	rig.answer(t, "test", `"result":{"processing":{"status":"complete","errors":[]},"extraction":"text-layer","pageCount":1,"pages":[{"number":1,"status":"ok","extraction":"text-layer","text":"Text"}]}`)
	rig.answer(t, "configure", `"result":`+processingSettings("off", ""))
	for _, tt := range []struct {
		method string
		bound  int
	}{{"test", 6 << 20}, {"configure", 64 << 10}} {
		envelope := len(`{"id":"rpc-` + strings.Repeat("0", 24) + `.tmp","method":"` + tt.method + `","params":}`)
		body := func(line int) []byte {
			frame := `{"document":{"bytes":""}}`
			return []byte(`{"document":{"bytes":"` + strings.Repeat("A", line-envelope-len(frame)) + `"}}`)
		}
		if code, raw := rig.post(t, "/api/document-processing/"+tt.method, body(tt.bound)); code != http.StatusOK {
			t.Fatalf("%s at the bound: %d %s", tt.method, code, raw)
		}
		lengths, _ := os.ReadFile(filepath.Join(rig.dir, "lengths"))
		if !strings.HasSuffix(string(lengths), strconv.Itoa(tt.bound)+"\n") {
			t.Fatalf("%s: the companion read %q", tt.method, lengths)
		}
		if code, raw := rig.post(t, "/api/document-processing/"+tt.method, body(tt.bound+1)); code != http.StatusRequestEntityTooLarge {
			t.Fatalf("%s a byte past the bound: %d %s", tt.method, code, raw)
		}
		if after, _ := os.ReadFile(filepath.Join(rig.dir, "lengths")); !bytes.Equal(after, lengths) {
			t.Fatalf("%s: a line past the bound was sent: %q", tt.method, after)
		}
	}
}

// The plan follows the settings only when the local gateway starts: a save
// that turns processing on or off restarts it, and the answer says which
// plan the running gateway has. A save that leaves it as it is, and a
// refused save, restart nothing.
func TestDocumentProcessingRestartsTheLocalGatewayWhenProcessingTurnsOnOrOff(t *testing.T) {
	rig := newProcessingRig(t)
	gateway := func(raw []byte) (bool, bool) {
		t.Helper()
		answered := decodeProcessing(t, raw)
		if answered.LocalGateway.Status != "ready" || answered.LocalGateway.DocumentProcessing == nil {
			t.Fatalf("the running gateway was not said: %s", raw)
		}
		return *answered.LocalGateway.DocumentProcessing, answered.LocalGateway.Restarted
	}
	_, raw := rig.post(t, "/api/document-processing/status", []byte(`{}`))
	if processing, restarted := gateway(raw); processing || restarted || rig.starts() != 1 {
		t.Fatalf("status: %s, %d starts", raw, rig.starts())
	}
	steps := []struct {
		answer     string
		plan       bool
		processing bool
		restarted  bool
		starts     int
	}{
		{`"result":` + processingSettings("off", ""), false, false, false, 1},
		{`"result":` + processingSettings("auto", ""), true, true, true, 2},
		{`"result":` + processingSettings("auto", ""), true, true, false, 2},
		{`"error":"processing-changed"`, false, true, false, 2},
		{`"result":` + processingSettings("off", ""), false, false, true, 3},
	}
	for i, step := range steps {
		rig.answer(t, "configure", step.answer)
		if step.plan {
			os.WriteFile(filepath.Join(rig.dir, "plan-processing"), nil, 0o600)
		} else {
			os.Remove(filepath.Join(rig.dir, "plan-processing"))
		}
		_, raw = rig.post(t, "/api/document-processing/configure", processingConfigure("off", ""))
		if processing, restarted := gateway(raw); processing != step.processing || restarted != step.restarted || rig.starts() != step.starts {
			t.Fatalf("step %d: %s, %d starts", i, raw, rig.starts())
		}
	}
	if status := rig.s.localGatewayStatus(nil); status.DocumentProcessing == nil || *status.DocumentProcessing {
		t.Fatal("the desk's status does not say the running gateway's plan")
	}
}

// A test's preview is what the page is shown of it: the records' error
// messages are left out, and a preview past its bounds is refused.
func TestDocumentProcessingTestPreviewIsHeldToItsBounds(t *testing.T) {
	rig := newProcessingRig(t)
	request := []byte(`{"connection":"ocr-work","revision":"` + processingRigDigest + `","document":{"name":"scan.pdf","mediaType":"application/pdf","bytes":"JVBERg=="}}`)
	rig.answer(t, "test", `"result":{"processing":{"status":"partial","errors":[{"code":"ocr-incomplete","message":"the program under /home/someone/ocr answered 1","page":null}],"bounds":{}},"extraction":"ocr","pageCount":2,"pages":[{"number":1,"status":"ok","extraction":"ocr","text":"Scanned text"}]}`)
	code, raw := rig.post(t, "/api/document-processing/test", request)
	if code != http.StatusOK || bytes.Contains(raw, []byte("/home/someone")) || !bytes.Contains(raw, []byte(`"code":"ocr-incomplete"`)) || !bytes.Contains(raw, []byte("Scanned text")) {
		t.Fatalf("%d %s", code, raw)
	}
	rig.answer(t, "test", `"result":{"processing":{"status":"complete","errors":[]},"extraction":"ocr","pageCount":1,"pages":[{"number":1,"status":"ok","extraction":"ocr","text":"`+strings.Repeat("x", 401)+`"}]}`)
	if code, _ := rig.post(t, "/api/document-processing/test", request); code != http.StatusBadGateway {
		t.Fatalf("a page past 400 characters: %d", code)
	}
}
