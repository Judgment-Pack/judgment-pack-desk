package desk

// Local processing is a Desk-owned companion, never a binary chosen by a
// project or downloaded at runtime. Its address and public pin are process
// facts, not edits to desk.json. An explicitly configured gateway always wins.
import (
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

const GatewayRevision = "e649f098b1143de69fb5bc8558f2686a4934acd9"
const localAuthority = "gateway:desk-local"

// LocalGatewayStatus carries public, effective settings only. The signing seed
// remains in the existing credential custody root and never reaches the page.
type LocalGatewayStatus struct {
	Status  string           `json:"status"`
	Gateway *localGatewayPin `json:"gateway,omitempty"`
	Problem string           `json:"problem,omitempty"`
}
type localGatewayPin struct {
	URL       string      `json:"url"`
	Authority string      `json:"authority"`
	Signer    localSigner `json:"signer"`
}
type localSigner struct {
	Algorithm string `json:"algorithm"`
	Public    string `json:"public"`
}

type localGateway struct {
	mu                 sync.Mutex
	closed             bool
	bundle, executable string
	cmd                *exec.Cmd
	input              io.WriteCloser
	done               chan struct{}
	pin                *localGatewayPin
	retryAfter         time.Time
	problem            error
}

func (g *localGateway) close() {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.closed = true
	g.stop()
}
func (g *localGateway) stop() {
	if g.input != nil {
		_ = g.input.Close()
		g.input = nil
	}
	if g.done != nil {
		select {
		case <-g.done:
		case <-time.After(8 * time.Second):
			_ = g.cmd.Process.Kill()
			<-g.done
		}
	}
	g.cmd, g.done, g.pin = nil, nil, nil
}

func verifyGatewayBundle(dir string) error {
	manifestFile, err := os.Open(filepath.Join(dir, "gateway-bundle.json"))
	if err != nil {
		return errors.New("local processing components are missing; install the complete Desk bundle")
	}
	defer manifestFile.Close()
	raw, err := readBounded(manifestFile, 8192)
	if err != nil {
		return errors.New("local processing manifest is too large or unreadable")
	}
	var manifest struct {
		Revision string            `json:"revision"`
		Files    map[string]string `json:"files"`
	}
	if json.Unmarshal(raw, &manifest) != nil || manifest.Revision != GatewayRevision {
		return errors.New("local processing components do not match this Desk build")
	}
	for _, name := range []string{"gateway", "adapter-document", "gateway-connections", "adapter-drive", "adapter-gmail"} {
		file, err := os.Open(filepath.Join(dir, executableName(name)))
		if err != nil {
			return err
		}
		stat, err := file.Stat()
		if err != nil || !stat.Mode().IsRegular() {
			file.Close()
			return errors.New("local processing component is not a regular file")
		}
		hash := sha256.New()
		_, err = io.Copy(hash, file)
		file.Close()
		if err != nil || hex.EncodeToString(hash.Sum(nil)) != manifest.Files[executableName(name)] {
			return errors.New("local processing component checksum does not match the bundle")
		}
	}
	return nil
}

// Serialize first use across Desk processes. Atomic replacement is allowed only
// for the first creation under this lock. A missing or changed established seed
// is an error, never a reason to rotate the trust root silently.
func localIdentity(store *assistantStore) (string, error) {
	lock, err := store.root.OpenFile(".local-gateway-identity.lock", os.O_CREATE|os.O_RDWR|openNoFollow|openNonBlocking, 0600)
	if err != nil {
		return "", err
	}
	defer lock.Close()
	info, err := lock.Stat()
	if err != nil {
		return "", err
	}
	if err = ownerOnlyFile("gateway identity lock", info.Mode()); err != nil {
		return "", err
	}
	if err = ownedByUs("gateway identity lock", info); err != nil {
		return "", err
	}
	deadline := time.Now().Add(3 * time.Second)
	for {
		if err = lockPrivateData(lock, true); err == nil {
			break
		}
		if time.Now().After(deadline) {
			return "", errors.New("local gateway identity is busy")
		}
		time.Sleep(20 * time.Millisecond)
	}
	marker, markerErr := readPrivateData(store.root, "local-gateway-identity.json", 256)
	if markerErr != nil && !errors.Is(markerErr, os.ErrNotExist) {
		return "", markerErr
	}
	raw, err := readPrivateData(store.secrets, "local-gateway.seed", 65)
	if errors.Is(err, os.ErrNotExist) {
		if markerErr == nil {
			return "", errors.New("local gateway signing key is missing; restore the original key to keep saved documents verifiable")
		}
		if _, historyErr := store.root.Lstat("local-gateway"); !errors.Is(historyErr, os.ErrNotExist) {
			return "", errors.New("local gateway signing identity is missing but receipt storage exists; restore the original protected settings")
		}
		seed := make([]byte, ed25519.SeedSize)
		if _, err = rand.Read(seed); err != nil {
			return "", err
		}
		raw = []byte(hex.EncodeToString(seed) + "\n")
		if err = writePrivateData(store.secrets, "local-gateway.seed", raw); err != nil {
			return "", err
		}
	} else if err != nil {
		return "", err
	}
	seed, err := hex.DecodeString(strings.TrimSuffix(string(raw), "\n"))
	if err != nil || len(seed) != ed25519.SeedSize {
		return "", errors.New("local gateway signing key is invalid; restore the original key")
	}
	public := hex.EncodeToString(ed25519.NewKeyFromSeed(seed).Public().(ed25519.PublicKey))
	expected, _ := json.Marshal(localSigner{Algorithm: "ed25519", Public: public})
	if markerErr == nil && string(marker) != string(expected) {
		return "", errors.New("local gateway signing identity changed; restore the original key")
	}
	if markerErr != nil {
		if err = writePrivateData(store.root, "local-gateway-identity.json", expected); err != nil {
			return "", err
		}
	}
	return public, nil
}

func (g *localGateway) ensure(store *assistantStore) (*localGatewayPin, error) {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.closed {
		return nil, errors.New("local processing is stopped")
	}
	if !store.usable() {
		return nil, store.problem
	}
	if g.done != nil {
		select {
		case <-g.done:
			g.stop()
		default:
			return g.pin, nil
		}
	}
	if time.Now().Before(g.retryAfter) {
		return nil, g.problem
	}
	pin, err := g.start(store)
	if err != nil {
		g.stop()
		g.problem = err
		g.retryAfter = time.Now().Add(5 * time.Second)
		return nil, err
	}
	g.pin = pin
	g.problem = nil
	return pin, nil
}
func (g *localGateway) start(store *assistantStore) (*localGatewayPin, error) {
	if err := verifyGatewayBundle(g.bundle); err != nil {
		return nil, err
	}
	public, err := localIdentity(store)
	if err != nil {
		return nil, err
	}
	// Each process owns a separate append-only registry. Saved document proofs
	// retain their sealed registry entries, so reopening them needs the stable
	// public pin, not a running instance's historic HTTP endpoint.
	root, err := openPrivateDataRoot(filepath.Join(store.dir, "local-gateway"), true)
	if err != nil {
		return nil, err
	}
	defer root.Close()
	run, err := randomStagingName("run-")
	if err != nil {
		return nil, err
	}
	if err = root.Mkdir(run, 0700); err != nil {
		return nil, err
	}
	options := localWorkerOptions{Bundle: g.bundle, Dir: filepath.Join(store.dir, "local-gateway", run), Seed: filepath.Join(store.dir, "secrets", "local-gateway.seed"), Public: public, ConnectionsDir: filepath.Join(store.dir, "gateway-connections")}
	g.cmd = exec.Command(g.executable, "--local-gateway-worker")
	g.input, err = g.cmd.StdinPipe()
	if err != nil {
		return nil, err
	}
	output, err := g.cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}
	g.cmd.Stderr = io.Discard // A companion must not print document content or a seed to Desk's logs.
	if err = g.cmd.Start(); err != nil {
		return nil, err
	}
	g.done = make(chan struct{})
	go func(cmd *exec.Cmd, done chan struct{}) { _ = cmd.Wait(); close(done) }(g.cmd, g.done)
	if err = json.NewEncoder(g.input).Encode(options); err != nil {
		return nil, err
	}
	type result struct {
		Pin *localGatewayPin
		Err error
	}
	ready := make(chan result, 1)
	go func() {
		var pin localGatewayPin
		err := json.NewDecoder(io.LimitReader(output, 4096)).Decode(&pin)
		ready <- result{&pin, err}
	}()
	select {
	case reply := <-ready:
		if reply.Err != nil {
			return nil, errors.New("local processing could not start; check the installed Desk bundle")
		}
		if reply.Pin.Authority != localAuthority || reply.Pin.Signer.Public != public || reply.Pin.Signer.Algorithm != "ed25519" || !strings.HasPrefix(reply.Pin.URL, "http://127.0.0.1:") {
			return nil, errors.New("local processing returned an unexpected identity")
		}
		return reply.Pin, nil
	case <-time.After(10 * time.Second):
		return nil, errors.New("local processing did not become ready")
	}
}

func (s *Server) localGatewayStatus(data []byte) *LocalGatewayStatus {
	if s.localGateway == nil {
		return nil
	}
	if data != nil {
		d := decodeDeskFile(data)
		if d.refused() {
			return nil
		}
		if d.Research != nil && d.Research.gateway != nil {
			return &LocalGatewayStatus{Status: "external"}
		}
	}
	pin, err := s.localGateway.ensure(s.assistant)
	if err != nil {
		return &LocalGatewayStatus{Status: "unavailable", Problem: err.Error()}
	}
	return &LocalGatewayStatus{Status: "ready", Gateway: pin}
}
func (s *Server) localResearch(documents *documentSourceConfig) (researchGateway, error) {
	if s.localGateway == nil {
		return researchGateway{}, withCode(CodeResearchUnconfigured, errors.New("no research gateway is configured: research.gateway is absent or null"))
	}
	pin, err := s.localGateway.ensure(s.assistant)
	if err != nil {
		return researchGateway{}, withCode(CodeResearchUnconfigured, err)
	}
	gateway := researchGateway{managedLocal: true, url: pin.URL, authority: pin.Authority, signerPublic: pin.Signer.Public, maxRequestBytes: maxResearchBody}
	if documents == nil {
		gateway.maxRequestBytes = 32 << 20
		gateway.maxFileBytes = 16 << 20
	} else if documents.enabled {
		gateway.maxRequestBytes = min(documents.maxRequestBytes, 32<<20)
		gateway.maxFileBytes = documents.maxFileBytes
	}
	return gateway, nil
}

// LocalGatewayBundleDir resolves only beside the executable, never in the
// current project or on PATH. A complete installation puts companions here.
func LocalGatewayBundleDir() string {
	name, err := os.Executable()
	if err != nil {
		return ""
	}
	return filepath.Dir(name)
}

// PrepareDeskConfigBase creates the usual .config (or XDG config base) on first
// launch, provided its parent already exists and passes custody validation. It
// does not create a home directory or repair permissions on existing ancestors.
func PrepareDeskConfigBase(configDir string) error {
	if !filepath.IsAbs(configDir) || !custodyChecked {
		return errors.New("no supported settings directory")
	}
	base := filepath.Dir(configDir)
	if _, err := os.Lstat(base); err == nil {
		return nil
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	if err := checkAncestors(filepath.Dir(base)); err != nil {
		return err
	}
	return ensureOwnedDirectory(filepath.Dir(base), filepath.Base(base))
}
