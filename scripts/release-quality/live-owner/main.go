// live-owner is a release measurement host, not an account or inference driver.
// With no command it prints usage and does not construct Desk or find a CLI.
package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"io/fs"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/Judgment-Pack/judgment-pack-desk/internal/codexbridge"
	"github.com/Judgment-Pack/judgment-pack-desk/internal/desk"
)

const maxControlBytes = 4096

var errControl = errors.New("owner control refused")

type options struct {
	ControlDir, ProjectDir, ConfigDir, AssetsDir, CodexBin, JpackBin string
	Model, Effort, Case1, Case2                                      string
	SetupTimeout                                                     time.Duration
}

func main() {
	if err := run(os.Args[1:], os.Stderr); err != nil {
		// No raw server, browser, native process or sign-in diagnostics escape.
		fmt.Fprintln(os.Stderr, "live-owner: refused or incomplete; inspect private owner evidence")
		os.Exit(1)
	}
}

func parseOptions(args []string, out io.Writer) (options, bool, error) {
	var o options
	if len(args) == 0 || (len(args) == 1 && args[0] == "--help") {
		fmt.Fprintln(out, "usage: live-owner serve --control-dir DIR --project-dir DIR --config-dir DIR --assets-dir DIR --codex-bin FILE --jpack-bin FILE --model MODEL --effort EFFORT --case1 ID --case2 ID --setup-timeout DURATION")
		return o, false, nil
	}
	if args[0] != "serve" {
		return o, false, errControl
	}
	f := flag.NewFlagSet("live-owner", flag.ContinueOnError)
	f.SetOutput(io.Discard)
	f.StringVar(&o.ControlDir, "control-dir", "", "private empty control directory")
	f.StringVar(&o.ProjectDir, "project-dir", "", "private scratch project")
	f.StringVar(&o.ConfigDir, "config-dir", "", "private empty Desk configuration directory")
	f.StringVar(&o.AssetsDir, "assets-dir", "", "explicit built assets")
	f.StringVar(&o.CodexBin, "codex-bin", "", "explicit native executable")
	f.StringVar(&o.JpackBin, "jpack-bin", "", "explicit Runtime executable")
	f.StringVar(&o.Model, "model", "", "explicit model, no substitution")
	f.StringVar(&o.Effort, "effort", "", "explicit effort")
	f.StringVar(&o.Case1, "case1", "", "first fixed corpus case identity")
	f.StringVar(&o.Case2, "case2", "", "second fixed corpus case identity")
	f.DurationVar(&o.SetupTimeout, "setup-timeout", 0, "separate explicit setup deadline")
	if f.Parse(args[1:]) != nil || len(f.Args()) != 0 || !identifier(o.Model) || !identifier(o.Case1) || !identifier(o.Case2) || o.Case1 == o.Case2 || o.SetupTimeout <= 0 || o.SetupTimeout > time.Hour {
		return o, false, errControl
	}
	switch o.Effort {
	case "none", "minimal", "low", "medium", "high", "xhigh", "max":
	default:
		return o, false, errControl
	}
	return o, true, nil
}

func run(args []string, out io.Writer) error {
	// Desk can execute itself for these normal adapters. Keep production dispatch;
	// this is not another provider loop. No gateway bundle is configured below.
	if len(args) == 1 {
		switch args[0] {
		case "--model-source-adapter":
			return desk.RunModelSourceAdapter(os.Stdin, os.Stdout)
		case "--input-source-adapter":
			return desk.RunInputSourceAdapter(os.Stdin, os.Stdout)
		case "--local-gateway-worker":
			return desk.RunLocalGatewayWorker(os.Stdin, os.Stdout)
		}
	}
	o, start, err := parseOptions(args, out)
	if err != nil || !start {
		return err
	}
	control, err := preparePaths(o)
	if err != nil {
		return errControl
	}
	defer control.Close()
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	return serve(ctx, o, control)
}

func identifier(s string) bool {
	if len(s) == 0 || len(s) > 128 {
		return false
	}
	for _, c := range s {
		if !(c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || strings.ContainsRune("-_.:/", c)) {
			return false
		}
	}
	return true
}

// Reject symlinks in every supplied path, including its ancestors. All mutable
// roots are distinct private directories; config/control must be fresh. No
// installed profile, PATH lookup or home-directory default is available.
func checkedPath(path string, directory, private bool) error {
	if !filepath.IsAbs(path) || filepath.Clean(path) != path {
		return errControl
	}
	for at := path; ; at = filepath.Dir(at) {
		info, err := os.Lstat(at)
		if err != nil || info.Mode()&os.ModeSymlink != 0 {
			return errControl
		}
		if at == path {
			if info.IsDir() != directory || (!directory && !info.Mode().IsRegular()) || (private && info.Mode().Perm()&0077 != 0) {
				return errControl
			}
			if private {
				st, ok := info.Sys().(*syscall.Stat_t)
				if !ok || int(st.Uid) != os.Getuid() {
					return errControl
				}
			}
			if !directory && info.Mode().Perm()&0111 == 0 {
				return errControl
			}
		}
		if at == filepath.Dir(at) {
			return nil
		}
	}
}

func preparePaths(o options) (*os.Root, error) {
	dirs := []string{o.ControlDir, o.ProjectDir, o.ConfigDir, o.AssetsDir}
	for i, path := range dirs {
		if err := checkedPath(path, true, i != 3); err != nil {
			return nil, err
		}
		for _, other := range dirs[:i] {
			if path == other || strings.HasPrefix(path, other+string(os.PathSeparator)) || strings.HasPrefix(other, path+string(os.PathSeparator)) {
				return nil, errControl
			}
		}
	}
	for _, path := range []string{o.ControlDir, o.ConfigDir} {
		entries, err := os.ReadDir(path)
		if err != nil || len(entries) != 0 {
			return nil, errControl
		}
	}
	for _, path := range []string{o.CodexBin, o.JpackBin} {
		if err := checkedPath(path, false, false); err != nil {
			return nil, err
		}
		for _, dir := range dirs[:3] {
			if strings.HasPrefix(path, dir+string(os.PathSeparator)) {
				return nil, errControl
			}
		}
	}
	info, err := os.Lstat(filepath.Join(o.AssetsDir, "index.html"))
	if err != nil || !info.Mode().IsRegular() {
		return nil, errControl
	}
	// Static serving must not follow an asset symlink outside this supplied
	// build. This is a finite read of the explicit assets, never home discovery.
	files, total := 0, int64(0)
	if err := filepath.WalkDir(o.AssetsDir, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil || entry.Type()&os.ModeSymlink != 0 {
			return errControl
		}
		if entry.IsDir() {
			return nil
		}
		info, err := entry.Info()
		if err != nil || !info.Mode().IsRegular() {
			return errControl
		}
		files++
		total += info.Size()
		if files > 10000 || total > 1<<30 {
			return errControl
		}
		return nil
	}); err != nil {
		return nil, errControl
	}
	return os.OpenRoot(o.ControlDir)
}

func writePrivate(root *os.Root, name string, value any) error {
	raw, err := json.Marshal(value)
	if err != nil || len(raw) > 65536 {
		return errControl // Never truncate evidence.
	}
	f, err := root.OpenFile(name+".tmp", os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return errControl
	}
	_, err = f.Write(append(raw, '\n'))
	if err == nil {
		err = f.Sync()
	}
	closeErr := f.Close()
	if err != nil || closeErr != nil {
		return errControl
	}
	if _, err = root.Lstat(name); !errors.Is(err, os.ErrNotExist) {
		return errControl
	}
	return root.Rename(name+".tmp", name)
}

func readPrivate(root *os.Root, name string) ([]byte, error) {
	info, err := root.Lstat(name)
	if err != nil {
		return nil, err
	}
	st, ok := info.Sys().(*syscall.Stat_t)
	if !ok || int(st.Uid) != os.Getuid() || !info.Mode().IsRegular() || info.Mode().Perm()&0077 != 0 || info.Size() > maxControlBytes {
		return nil, errControl
	}
	f, err := root.Open(name)
	if err != nil {
		return nil, errControl
	}
	defer f.Close()
	opened, err := f.Stat()
	if err != nil || !os.SameFile(info, opened) {
		return nil, errControl
	}
	raw, err := io.ReadAll(io.LimitReader(f, maxControlBytes+1))
	if err != nil || len(raw) > maxControlBytes {
		return nil, errControl
	}
	return raw, nil
}

// Commands are flat closed records: reject duplicate keys as well as unknown
// fields/trailing data. Input is host control, never an authorization grant.
func decodeCommand(raw []byte) (command, error) {
	var c command
	if len(raw) > maxControlBytes {
		return c, errControl
	}
	d := json.NewDecoder(bytes.NewReader(raw))
	if token, err := d.Token(); err != nil || token != json.Delim('{') {
		return c, errControl
	}
	seen := map[string]bool{}
	for d.More() {
		token, err := d.Token()
		key, ok := token.(string)
		if err != nil || !ok || seen[key] {
			return c, errControl
		}
		seen[key] = true
		var value any
		if d.Decode(&value) != nil {
			return c, errControl
		}
	}
	if _, err := d.Token(); err != nil {
		return c, errControl
	}
	d = json.NewDecoder(bytes.NewReader(raw))
	d.DisallowUnknownFields()
	if d.Decode(&c) != nil || d.Decode(new(any)) != io.EOF {
		return c, errControl
	}
	return c, nil
}

func serve(ctx context.Context, o options, control *os.Root) error {
	listener, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		return errControl
	}
	token, err := desk.NewToken()
	if err != nil {
		listener.Close()
		return errControl
	}
	sink := codexbridge.NewAccountingSink()
	s, err := desk.New(desk.Config{ProjectDir: o.ProjectDir, DeskConfigDir: o.ConfigDir,
		Static: os.DirFS(o.AssetsDir), Port: listener.Addr().(*net.TCPAddr).Port, Token: token,
		CodexBin: o.CodexBin, JpackBin: o.JpackBin, CodexAccounting: sink,
		LocalAccess: false, DevMode: false, Logger: log.New(io.Discard, "", 0)})
	if err != nil {
		listener.Close()
		return errControl
	}
	httpServer := &http.Server{Handler: s, ReadHeaderTimeout: 5 * time.Second, IdleTimeout: 30 * time.Second}
	served := make(chan error, 1)
	go func() { served <- httpServer.Serve(listener) }()
	host := newOwner(o, sink.Reports(), sink.Dropped, func() error {
		// Real Server owns native clients and reaps them; HTTP close alone does not.
		serverErr := s.Close()
		httpErr := httpServer.Close()
		serveErr := <-served
		if serverErr != nil || httpErr != nil || (serveErr != nil && !errors.Is(serveErr, http.ErrServerClosed)) {
			return errControl
		}
		return nil
	})
	configuration, _ := json.Marshal(o)
	digest := sha256.Sum256(configuration)
	descriptor := struct {
		BaseURL                     string `json:"baseUrl"`
		SetupToken                  string `json:"setupToken"`
		ConfigurationSHA256         string `json:"configurationSha256"`
		SetupTimeoutMS              int64  `json:"setupTimeoutMs"`
		CaseTimeoutMS               int64  `json:"caseTimeoutMs"`
		AggregateInferenceTimeoutMS int64  `json:"aggregateInferenceTimeoutMs"`
	}{"http://" + listener.Addr().String(), token, hex.EncodeToString(digest[:]), o.SetupTimeout.Milliseconds(), caseTimeout.Milliseconds(), aggregateTimeout.Milliseconds()}
	if writePrivate(control, "owner.json", descriptor) != nil {
		host.close("output_refused")
		host.finish()
		return errControl
	}
	return host.control(ctx, control)
}
