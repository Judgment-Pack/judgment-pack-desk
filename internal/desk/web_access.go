package desk

// Desk owns this opt-in, not a pack, prompt, or source page. It controls only
// the verified local gateway and never changes an explicitly external gateway.
import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"time"
)

const webPolicyEnv = "JPACK_WEB_REDIRECT_POLICIES"
const webAccessFile = "web-access.json"
const groundingPolicy = "google-grounding"

type webAccessConfig struct {
	Version         int  `json:"version"`
	GoogleGrounding bool `json:"googleGrounding"`
}
type webAccessStatus struct {
	Supported bool   `json:"supported"`
	Enabled   bool   `json:"enabled"`
	Managed   bool   `json:"managed"`
	Local     bool   `json:"local"`
	Revision  string `json:"revision"`
}

// nil leaves an operator environment untouched. A saved false explicitly
// clears the inherited value. An operator environment takes precedence and
// makes the control read-only, including when explicitly set to empty.
func webPolicyValue(store *assistantStore) (*string, []byte, error) {
	if value, managed := os.LookupEnv(webPolicyEnv); managed {
		return &value, nil, nil
	}
	raw, err := readPrivateData(store.root, webAccessFile, 1024)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil, nil
	}
	if err != nil {
		return nil, nil, err
	}
	var config webAccessConfig
	if decodeDataJSON(raw, &config) != nil || config.Version != 1 {
		return nil, nil, errors.New("invalid web access settings")
	}
	value := ""
	if config.GoogleGrounding {
		value = groundingPolicy
	}
	return &value, raw, nil
}

// This reads a verified companion's local declaration. It neither starts the
// gateway nor makes a network request or reads a page.
func readWebCapabilities(ctx context.Context, bundle, connectionsDir string, value *string) (bool, bool, error) {
	if err := verifyGatewayBundle(bundle); err != nil {
		return false, false, err
	}
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, filepath.Join(bundle, executableName("gateway-connections")), "--source-capabilities")
	cmd.Env = append(os.Environ(), "JPACK_CONNECTIONS_DIR="+connectionsDir)
	if value != nil {
		cmd.Env = append(cmd.Env, webPolicyEnv+"="+*value)
	}
	output := &cappedBuffer{limit: 32 << 10}
	cmd.Stdout = output
	cmd.Stderr = io.Discard
	cmd.WaitDelay = time.Second
	if cmd.Run() != nil || output.exceeded() {
		return false, false, errors.New("web capabilities unavailable")
	}
	var caps struct {
		Version int `json:"version"`
		Sources []struct {
			ID               string `json:"id"`
			RedirectPolicies struct {
				Supported []string `json:"supported"`
				Enabled   []string `json:"enabled"`
			} `json:"redirectPolicies"`
		} `json:"sources"`
	}
	if decodeDataJSON(output.Bytes(), &caps) != nil || caps.Version != 1 {
		return false, false, errors.New("unsupported web capabilities")
	}
	for _, source := range caps.Sources {
		if source.ID == "web" {
			return slices.Contains(source.RedirectPolicies.Supported, groundingPolicy), slices.Contains(source.RedirectPolicies.Enabled, groundingPolicy), nil
		}
	}
	return false, false, nil
}

func (s *Server) webAccessStatus(ctx context.Context) (webAccessStatus, error) {
	status := webAccessStatus{}
	if s.localGateway == nil {
		return status, nil
	}
	_, raw, err := s.readDeskFile()
	if err != nil {
		return status, err
	}
	if raw != nil {
		d := decodeDeskFile(raw)
		if d.refused() {
			return status, errors.New("desk settings unavailable")
		}
		if d.Research != nil && d.Research.gateway != nil {
			return status, nil
		}
	}
	status.Local = true
	value, saved, err := webPolicyValue(s.assistant)
	if err != nil {
		return status, err
	}
	_, status.Managed = os.LookupEnv(webPolicyEnv)
	status.Supported, status.Enabled, err = readWebCapabilities(ctx, s.localGateway.bundle, platformPath(s.assistant.dir, "gateway-connections"), value)
	if err != nil {
		return status, err
	}
	// Bind the editor to both saved bytes and capabilities. A changed bundle,
	// operator override or competing save requires a fresh review.
	revision, _ := json.Marshal([]any{saved, status.Supported, status.Enabled, status.Managed, value, GatewayRevision})
	digest := sha256.Sum256(revision)
	status.Revision = hex.EncodeToString(digest[:])
	return status, nil
}

func (s *Server) handleWebAccess(w http.ResponseWriter, r *http.Request) {
	if !s.guard(w, r) || s.refuseUnusableStore(w) {
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	if r.URL.RawQuery != "" {
		writeJSONCoded(w, 400, CodeBadRequest, "web access settings carry no query")
		return
	}
	// Share the lifecycle lock with ensure and restart: two Apply requests
	// cannot both accept one revision or stop a newly started replacement.
	if s.localGateway != nil {
		s.localGateway.mu.Lock()
		defer s.localGateway.mu.Unlock()
	}
	// A held directory lock also serializes another Desk process sharing
	// these settings. Reading the form never starts a gateway.
	lock, err := s.assistant.root.Open(".")
	if err != nil {
		writeJSONCoded(w, 503, "web-access-unavailable", "Web access settings could not be loaded.")
		return
	}
	defer lock.Close()
	if err = waitPrivateDataLock(r.Context(), lock, r.Method != http.MethodGet); err != nil {
		writeJSONCoded(w, 409, CodeStale, "Web access settings changed. Refresh and review them before applying.")
		return
	}
	status, err := s.webAccessStatus(r.Context())
	if err != nil {
		writeJSONCoded(w, 503, "web-access-unavailable", "Web access settings could not be loaded.")
		return
	}
	if r.Method == http.MethodGet {
		writeJSON(w, 200, status)
		return
	}
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, 4096))
	if err != nil {
		writeJSONCoded(w, 413, CodeTooLarge, "web access request too large")
		return
	}
	var request struct {
		Enabled       *bool  `json:"enabled"`
		Revision      string `json:"revision"`
		RestartWarned bool   `json:"restartWarned"`
	}
	if _, ok := catalogMembers(body, "enabled", "revision", "restartWarned"); !ok || decodeDataJSON(body, &request) != nil || request.Enabled == nil || !request.RestartWarned {
		writeJSONCoded(w, 400, CodeBadRequest, "review the web access settings and restart warning before applying")
		return
	}
	if !status.Local || !status.Supported || status.Managed {
		writeJSONCoded(w, 409, "web-access-managed", "Web access is not configurable on this gateway.")
		return
	}
	if request.Revision != status.Revision {
		writeJSONCoded(w, 409, CodeStale, "Web access settings changed. Refresh and review them before applying.")
		return
	}
	if status.Enabled != *request.Enabled {
		raw, _ := json.Marshal(webAccessConfig{Version: 1, GoogleGrounding: *request.Enabled})
		if err = writePrivateData(s.assistant.root, webAccessFile, raw); err != nil {
			writeJSONCoded(w, 500, "web-access-save-failed", "Web access settings could not be saved.")
			return
		}
		// Persist first, then stop. No start and no read retry. The next explicit
		// use loads the saved policy even if Desk is restarted in the meantime.
		s.localGateway.stop()
		s.localGateway.retryAfter = time.Time{}
	}
	status, err = s.webAccessStatus(r.Context())
	if err != nil {
		writeJSONCoded(w, 503, "web-access-unavailable", "Web access settings could not be loaded.")
		return
	}
	writeJSON(w, 200, status)
}
