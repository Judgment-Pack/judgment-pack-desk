package desk

// Model acquisition is a private, one-use bridge from Gateway to an already
// admitted workflow operation. It is not a browser API or a permission grant.
// The workflow must persist admission and completion before handing off output.
import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"log"
	"net"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"
)

const modelSourceName = "desk-model"
const modelSourceTool = "answer"
const modelSourceEndpointPrefix = "desk-local:model-answer:"
const modelSourceEnvironment = "JPACK_MODEL_SOURCE"
const modelSourceMaxPending = 64

// Kept only in the parent/worker pipe and the named adapter's environment. This
// process-local credential is never a profile member or a Gateway request field.
type modelSourceConfig struct {
	URL   string `json:"url"`
	Token string `json:"token"`
}

func (c modelSourceConfig) valid() bool {
	u, err := url.Parse(c.URL)
	if err != nil || u.Scheme != "http" || u.User != nil || u.Path != "" || u.RawQuery != "" || u.ForceQuery || u.Fragment != "" || u.Hostname() != "127.0.0.1" {
		return false
	}
	port, err := strconv.Atoi(u.Port())
	token, e := hex.DecodeString(c.Token)
	return err == nil && port > 0 && port <= 65535 && e == nil && len(token) == 32
}

type modelSourceRequest struct {
	Tool      string `json:"tool"`
	Arguments struct {
		Grant  string `json:"grant"`
		Digest string `json:"requestDigest"`
	} `json:"arguments"`
}
type modelSourceOperation struct {
	context  context.Context
	digest   string
	endpoint string
	execute  func(context.Context) (json.RawMessage, error)
}
type modelSourceBroker struct {
	adapter    map[string]string
	active     sync.WaitGroup
	config     modelSourceConfig
	server     *http.Server
	done       chan struct{}
	mu         sync.Mutex
	closed     bool
	operations map[string]modelSourceOperation
}

func newModelSourceBroker() (*modelSourceBroker, error) {
	_, adapter, err := deskModelAdapter()
	if err != nil {
		return nil, err
	}
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return nil, errors.New("The private model acquisition adapter could not start.")
	}
	var nonce [32]byte
	_, _ = rand.Read(nonce[:])
	b := &modelSourceBroker{adapter: adapter, config: modelSourceConfig{URL: "http://" + listener.Addr().String(), Token: hex.EncodeToString(nonce[:])}, operations: make(map[string]modelSourceOperation), done: make(chan struct{})}
	b.server = &http.Server{Handler: http.HandlerFunc(b.serve), ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 5 * time.Second, IdleTimeout: 5 * time.Second, MaxHeaderBytes: 8192, ErrorLog: log.New(io.Discard, "", 0)}
	go func() { defer close(b.done); _ = b.server.Serve(listener) }()
	return b, nil
}
func (b *modelSourceBroker) close() {
	b.mu.Lock()
	b.closed = true
	b.operations = make(map[string]modelSourceOperation)
	b.mu.Unlock()
	_ = b.server.Close()
	<-b.done
	b.active.Wait()
}

// register is called by the owning workflow after durable admission. The digest
// binds the exact frozen operation; no prompt, model or connection arrives from
// the adapter caller. A consumed or lost grant can never invoke a model again.
func (b *modelSourceBroker) register(ctx context.Context, choice backgroundModelChoice, frozenRequest []byte, execute func(context.Context) (json.RawMessage, error)) (modelSourceRequest, func(), error) {
	var request modelSourceRequest
	if execute == nil || choice.Connection == "" || choice.Revision == "" || choice.Model == "" || len(frozenRequest) == 0 || !json.Valid(frozenRequest) {
		return request, nil, errors.New("Invalid admitted model operation.")
	}
	if err := ctx.Err(); err != nil {
		return request, nil, err
	}
	var nonce [32]byte
	_, _ = rand.Read(nonce[:])
	grant := hex.EncodeToString(nonce[:])
	request.Tool = modelSourceTool
	request.Arguments.Grant = grant
	request.Arguments.Digest = sha256Digest(frozenRequest)
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.closed || len(b.operations) >= modelSourceMaxPending {
		return modelSourceRequest{}, nil, errors.New("The private model acquisition adapter is unavailable.")
	}
	b.operations[grant] = modelSourceOperation{context: ctx, digest: request.Arguments.Digest, endpoint: modelSourceEndpoint(choice), execute: execute}
	cleanup := func() { b.mu.Lock(); delete(b.operations, grant); b.mu.Unlock() }
	return request, cleanup, nil
}
func (b *modelSourceBroker) serve(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	auth := r.Header.Values("Authorization")
	if r.Method != "POST" || r.URL.Path != "/answer" || r.URL.RawQuery != "" || r.URL.ForceQuery || browserMarked(r) || len(auth) != 1 || subtle.ConstantTimeCompare([]byte(auth[0]), []byte("Bearer "+b.config.Token)) != 1 {
		http.Error(w, "Private model acquisition required.", http.StatusUnauthorized)
		return
	}
	raw, err := readBounded(r.Body, 4096)
	var request modelSourceRequest
	if err != nil || decodeDataJSON(raw, &request) != nil || request.Tool != modelSourceTool {
		http.Error(w, "Invalid model acquisition request.", 400)
		return
	}
	b.mu.Lock()
	operation, ok := b.operations[request.Arguments.Grant]
	if b.closed || !ok || operation.digest != request.Arguments.Digest {
		b.mu.Unlock()
		http.Error(w, "Model acquisition is unavailable or already consumed.", 409)
		return
	}
	delete(b.operations, request.Arguments.Grant) // before model dispatch; never restore
	b.active.Add(1)
	defer b.active.Done()
	b.mu.Unlock()
	ctx, cancel := context.WithTimeout(operation.context, backgroundModelTimeout)
	stop := context.AfterFunc(r.Context(), cancel)
	defer stop()
	defer cancel()
	if ctx.Err() != nil {
		http.Error(w, "Model acquisition was stopped.", 409)
		return
	}
	result, err := operation.execute(ctx)
	if err != nil {
		http.Error(w, "The admitted model operation did not complete. No replay was made.", 502)
		return
	}
	if len(result) > backgroundModelResponseBytes || !json.Valid(result) || bytes.Contains(result, []byte(b.config.Token)) {
		http.Error(w, "Invalid admitted model response.", 502)
		return
	}
	// Keep the accepted result bytes intact. json.Encoder would HTML-escape a
	// RawMessage, expanding valid at-bound model output beyond the adapter limit.
	acquisition, _ := json.Marshal(map[string]any{"adapter": b.adapter, "endpoint": operation.endpoint, "statement": nil, "snapshot": nil, "peerIdentity": nil, "schema": nil, "upstreamToken": nil, "observedAt": time.Now().UTC().Format(time.RFC3339)})
	envelope := append([]byte(`{"result":`), result...)
	envelope = append(envelope, []byte(`,"acquisition":`)...)
	envelope = append(envelope, acquisition...)
	envelope = append(envelope, '}')
	w.Header().Set("Content-Type", "application/json")
	_, _ = w.Write(envelope)
}

// The signed acquisition pin includes the exact host-selected connection,
// revision, model and effort. An unchanged receipt cannot be relabeled as an
// acquisition from a different registered profile. The caller never selects it.
func modelSourceEndpoint(choice backgroundModelChoice) string {
	raw, _ := json.Marshal(choice)
	return modelSourceEndpointPrefix + strings.TrimPrefix(sha256Digest(raw), "sha256:")
}

func deskModelAdapter() (string, map[string]string, error) {
	path, adapter, err := deskInputAdapter()
	if err != nil {
		return "", nil, err
	}
	adapter["name"] = "jpack-desk/model-answer"
	return path, adapter, nil
}

// RunModelSourceAdapter never calls a provider. It can only consume one operation
// admitted by this Desk process. Gateway signs the returned result at acquisition.
func RunModelSourceAdapter(input io.Reader, output io.Writer) error {
	raw, err := readBounded(input, 4096)
	var request modelSourceRequest
	if err != nil || decodeDataJSON(raw, &request) != nil || request.Tool != modelSourceTool {
		return errors.New("Invalid model acquisition request.")
	}
	var config modelSourceConfig
	env := os.Getenv(modelSourceEnvironment)
	if len(env) > 4096 || decodeDataJSON([]byte(env), &config) != nil || !config.valid() {
		return errors.New("The model acquisition adapter is unavailable.")
	}
	transport := &http.Transport{Proxy: nil}
	defer transport.CloseIdleConnections()
	client := http.Client{Transport: transport, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	r, err := http.NewRequest("POST", config.URL+"/answer", bytes.NewReader(raw))
	if err != nil {
		return err
	}
	r.Header.Set("Authorization", "Bearer "+config.Token)
	r.Header.Set("Content-Type", "application/json")
	response, err := client.Do(r)
	if err != nil {
		return errors.New("The model acquisition did not return. No replay was made.")
	}
	defer response.Body.Close()
	raw, err = readBounded(response.Body, backgroundModelResponseBytes+8192)
	if err != nil || response.StatusCode != 200 || !json.Valid(raw) || strings.Contains(string(raw), config.Token) {
		return errors.New("The admitted model acquisition did not complete. No replay was made.")
	}
	_, err = output.Write(raw)
	return err
}

// Give the admitted operation its whole shared model budget and bounded storage
// completion window before Gateway may terminate the adapter. The worker's own
// context may stop it earlier; this is not extra model allowance.
func modelSourceGatewayArgs(executable string) []string {
	timeout := backgroundModelTimeout + privateDataLockWait + time.Second
	return []string{"--source", modelSourceName + "=" + executable + " --model-source-adapter", "--source-shape", modelSourceName + "=mcp", "--source-timeout", modelSourceName + "=" + strconv.Itoa(int(timeout/time.Second)), "--source-env", modelSourceName + "=" + modelSourceEnvironment}
}

// registerModelSourceProfile is a trusted-host installation write. A feature's
// reviewed binding action must call it explicitly; it is never exposed through
// the browser's generic Runner proxy and does not dispatch a model operation.
func (s *Server) registerModelSourceProfile(ctx context.Context, choice backgroundModelChoice) (json.RawMessage, error) {
	if err := s.validateBackgroundModelConnection(ctx, choice); err != nil {
		return nil, err
	}
	if s.localGateway == nil {
		return nil, errors.New("The local model acquisition adapter is unavailable.")
	}
	pin, err := s.localGateway.ensure(s.assistant)
	if err != nil {
		return nil, err
	}
	s.localGateway.mu.Lock()
	broker := s.localGateway.modelSource
	s.localGateway.mu.Unlock()
	if broker == nil {
		return nil, errors.New("The local model acquisition adapter is unavailable.")
	}
	identity, _ := json.Marshal(map[string]any{"choice": choice, "adapter": broker.adapter, "authority": pin.Authority, "publicKey": pin.Signer.Public})
	id := "desk-answer-" + strings.TrimPrefix(sha256Digest(identity), "sha256:")[:32]
	profile := map[string]any{"id": id, "publicKey": pin.Signer.Public, "class": "generated", "source": modelSourceName, "authority": pin.Authority, "shape": "mcp", "adapter": broker.adapter, "endpoint": modelSourceEndpoint(choice), "tools": []string{modelSourceTool}, "research": map[string]any{"capabilities": []string{"answer"}, "model": choice.Model}}
	return s.inputRunner(ctx, "input-profiles", profile, "")
}
