package desk

// Release discovery is fixed to this repository. Installing is available only
// through an operator-owned managed installation, never in a source checkout.
import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sync"
	"time"

	"github.com/Judgment-Pack/judgment-pack-desk/internal/releaseplan"
)

const deskReleasesURL = "https://api.github.com/repos/Judgment-Pack/judgment-pack-desk/releases/latest"
const deskReleasePage = "https://github.com/Judgment-Pack/judgment-pack-desk/releases"

var stableReleaseTag = regexp.MustCompile(`^v(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)$`)

type updateService struct {
	mu          sync.Mutex
	root        string
	python      string
	development bool
	cached      map[string]any
	checked     time.Time
	cancel      context.CancelFunc
}

func newUpdateService(development bool) *updateService {
	u := &updateService{development: development, cached: map[string]any{}}
	if !development {
		root := os.Getenv("JPACK_DESK_INSTALL_ROOT")
		executable, _ := os.Executable()
		executable, _ = filepath.EvalSymlinks(executable)
		expected := filepath.Join(root, "releases", releaseplan.Version, "jpack-desk")
		info, err := os.Lstat(root)
		script := filepath.Join(root, "desk-update.py")
		file, ferr := os.Lstat(script)
		if filepath.IsAbs(root) && executable == expected && err == nil && info.IsDir() && info.Mode().Perm() == 0700 && ownedByUs("installation", info) == nil && ferr == nil && file.Mode().IsRegular() && file.Mode().Perm()&0077 == 0 && ownedByUs("updater", file) == nil {
			if python, err := exec.LookPath("python3"); err == nil {
				u.root, u.python = root, python
			}
		}
	}
	return u
}

// Managed installations check at startup and daily even when no browser is open.
// The launcher applies updates only after the prior managed process has exited.
func (u *updateService) start() {
	if u.root == "" {
		return
	}
	ctx, cancel := context.WithCancel(context.Background())
	u.cancel = cancel
	go func() {
		for {
			_, _ = u.status(ctx, "check")
			timer := time.NewTimer(24 * time.Hour)
			select {
			case <-ctx.Done():
				timer.Stop()
				return
			case <-timer.C:
			}
		}
	}()
}

func fetchLatestDeskRelease(ctx context.Context, client *http.Client) (map[string]any, error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, deskReleasesURL, nil)
	if err != nil {
		return nil, err
	}
	request.Header.Set("Accept", "application/vnd.github+json")
	request.Header.Set("User-Agent", "Judgment-Pack-Desk-update-check")
	response, err := client.Do(request)
	if err != nil {
		return nil, err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return nil, errors.New("release service unavailable")
	}
	raw, err := io.ReadAll(io.LimitReader(response.Body, (2<<20)+1))
	if err != nil || len(raw) > 2<<20 {
		return nil, errors.New("invalid release response")
	}
	var doc struct {
		Tag        string `json:"tag_name"`
		Draft      bool   `json:"draft"`
		Prerelease bool   `json:"prerelease"`
	}
	if json.Unmarshal(raw, &doc) != nil || !stableReleaseTag.MatchString(doc.Tag) || doc.Draft || doc.Prerelease {
		return nil, errors.New("invalid stable release")
	}
	return map[string]any{"version": doc.Tag[1:], "url": deskReleasePage + "/tag/" + doc.Tag}, nil
}

func (u *updateService) status(ctx context.Context, action string) (map[string]any, error) {
	if !u.mu.TryLock() {
		return nil, errors.New("another update operation is running")
	}
	defer u.mu.Unlock()
	if u.root != "" {
		allowed := map[string]bool{"status": true, "check": true, "stage": true, "cancel": true, "auto-on": true, "auto-off": true}
		if !allowed[action] {
			return nil, errors.New("unsupported update operation")
		}
		timeout := 30 * time.Second
		if action == "stage" {
			timeout = 10 * time.Minute
		}
		run, cancel := context.WithTimeout(ctx, timeout)
		defer cancel()
		cmd := exec.CommandContext(run, u.python, filepath.Join(u.root, "desk-update.py"), "--root", u.root, action)
		// Helper output is bounded and never contains credentials or archive bytes.
		output := &boundedUpdateOutput{}
		cmd.Stdout = output
		if err := cmd.Run(); err != nil {
			return nil, errors.New("update operation failed; the active installation is unchanged")
		}
		var fresh map[string]any
		if json.Unmarshal(output.raw, &fresh) != nil {
			return nil, errors.New("invalid updater response")
		}
		u.cached = fresh
	} else if action == "check" {
		// Bound repeated clicks without reporting a failed check as current.
		if time.Since(u.checked) >= time.Minute {
			checked, cancel := context.WithTimeout(ctx, 20*time.Second)
			defer cancel()
			latest, err := fetchLatestDeskRelease(checked, &http.Client{Timeout: 20 * time.Second, CheckRedirect: func(_ *http.Request, _ []*http.Request) error { return http.ErrUseLastResponse }})
			u.checked = time.Now()
			u.cached["checkedAt"] = u.checked.Unix()
			if err != nil {
				u.cached["error"] = "Release check failed. The installed version is unchanged."
			} else {
				delete(u.cached, "error")
				u.cached["latest"] = latest
			}
		}
	} else if action != "status" {
		return nil, errors.New("use a managed installation to prepare updates")
	}
	result := make(map[string]any)
	for key, value := range u.cached {
		result[key] = value
	}
	result["managed"] = u.root != ""
	result["development"] = u.development
	result["installedVersion"] = releaseplan.Version
	result["components"] = releaseplan.Locked.Components
	result["releasePage"] = deskReleasePage
	return result, nil
}

type boundedUpdateOutput struct{ raw []byte }

func (b *boundedUpdateOutput) Write(p []byte) (int, error) {
	if len(b.raw)+len(p) > 2<<20 {
		return 0, errors.New("oversized updater response")
	}
	b.raw = append(b.raw, p...)
	return len(p), nil
}

func (s *Server) handleUpdates(w http.ResponseWriter, r *http.Request) {
	if !s.guard(w, r) {
		return
	}
	action := "status"
	if r.Method == http.MethodPost {
		var input struct {
			Action string `json:"action"`
		}
		decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1024))
		decoder.DisallowUnknownFields()
		if decoder.Decode(&input) != nil || decoder.Decode(&struct{}{}) != io.EOF {
			writeJSONCoded(w, 400, CodeBadRequest, "invalid update request")
			return
		}
		action = input.Action
		switch action {
		case "check", "stage", "cancel", "auto-on", "auto-off":
		default:
			writeJSONCoded(w, 400, CodeBadRequest, "unknown update operation")
			return
		}
	}
	out, err := s.updates.status(r.Context(), action)
	if err != nil {
		writeJSONCoded(w, 409, CodeBadRequest, err.Error())
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(out)
}
