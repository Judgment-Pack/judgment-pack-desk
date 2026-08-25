// Command jpack-desk serves a local web desk for a Judgment Pack project.
//
// It is a generic chassis, not a feature server: it serves an embedded
// single-page application and relays JSON-RPC between that page and a
// `jpack mcp` subprocess. The browser is the MCP client; every capability the
// desk shows comes from a tool the runtime already exposes, so a new runtime
// tool needs no new endpoint here.
package main

import (
	"context"
	"embed"
	"errors"
	"flag"
	"fmt"
	"io/fs"
	"log"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"github.com/Judgment-Pack/judgment-pack-desk/internal/desk"
)

// The built SPA. `all:` keeps files whose names begin with `_` or `.`, which
// the asset pipeline can emit. web/dist/.gitkeep is committed so that a fresh
// clone builds before `npm run build` has ever run; the server reports the
// missing index.html as a build instruction rather than a bare 404.
//
//go:embed all:web/dist
var embeddedWeb embed.FS

func main() {
	if err := run(); err != nil {
		fmt.Fprintf(os.Stderr, "jpack-desk: %v\n", err)
		os.Exit(1)
	}
}

func run() error {
	var (
		port     = flag.Int("port", 8791, "loopback TCP port to listen on")
		jpackBin = flag.String("jpack", "jpack", "path to the judgment-pack runtime binary")
		devToken = flag.String("dev-token", "", "fixed session token for local development; also permits the Vite dev-server origin. Leave empty in normal use so a random token is generated.")
		open     = flag.Bool("print-url", true, "print the tokened URL at startup")
	)
	flag.Usage = func() {
		fmt.Fprintf(flag.CommandLine.Output(), "usage: jpack-desk [flags] [projectDir]\n\nprojectDir defaults to the current directory.\n\nflags:\n")
		flag.PrintDefaults()
	}
	flag.Parse()

	projectDir := "."
	if flag.NArg() > 0 {
		projectDir = flag.Arg(0)
	}
	absProject, err := filepath.Abs(projectDir)
	if err != nil {
		return fmt.Errorf("resolving project directory %q: %w", projectDir, err)
	}
	if info, err := os.Stat(absProject); err != nil {
		return fmt.Errorf("project directory %q: %w", absProject, err)
	} else if !info.IsDir() {
		return fmt.Errorf("project directory %q is not a directory", absProject)
	}

	static, err := fs.Sub(embeddedWeb, "web/dist")
	if err != nil {
		return fmt.Errorf("locating embedded assets: %w", err)
	}

	token := *devToken
	if token == "" {
		if token, err = desk.NewToken(); err != nil {
			return fmt.Errorf("generating session token: %w", err)
		}
	}

	srv, err := desk.New(desk.Config{
		ProjectDir: absProject,
		JpackBin:   *jpackBin,
		Token:      token,
		Static:     static,
		DevMode:    *devToken != "",
		Logger:     log.New(os.Stderr, "", log.LstdFlags),
	})
	if err != nil {
		return err
	}
	defer srv.Close()

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	addr := fmt.Sprintf("127.0.0.1:%d", *port)
	httpSrv := &http.Server{
		Addr:    addr,
		Handler: srv,
		// ReadHeaderTimeout only. A WriteTimeout would be wrong here: it applies
		// to the whole connection, and /ws is a long-lived WebSocket the relay
		// holds open for the life of a session — a write deadline would sever
		// every relay on a timer. Header reading happens before any upgrade, so
		// bounding it costs the relay nothing and closes the one slow-client
		// window that does not need a hijack.
		//
		// The stalled-client hazard on /api is handled where it actually is:
		// the write mutex is released before the response is encoded, so a
		// client that stops reading holds only its own request.
		ReadHeaderTimeout: 10 * time.Second,
	}

	go func() {
		<-ctx.Done()
		shutdown, cancel := context.WithTimeout(context.Background(), desk.ShutdownGrace)
		defer cancel()
		_ = httpSrv.Shutdown(shutdown)
	}()

	if *open {
		fmt.Printf("judgment-pack desk\n  project: %s\n  runtime: %s\n  open:    http://%s/?token=%s\n", absProject, *jpackBin, addr, token)
	}
	if err := httpSrv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	return nil
}
