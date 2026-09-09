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
		devToken = flag.String("dev-token", "", "fixed launch secret for local development; also permits the Vite dev-server origin. Leave empty in normal use so a random secret is generated.")
		open     = flag.Bool("print-url", true, "print the launch URL at startup")
	)
	flag.Usage = func() {
		fmt.Fprintf(flag.CommandLine.Output(), "usage: jpack-desk [flags] [projectDir]\n\nWithout projectDir, the desk opens the project named by project.file in this machine's\ndesk configuration file, and the current directory where that names none.\n\nflags:\n")
		flag.PrintDefaults()
	}
	flag.Parse()

	// **Which project, decided and pinned before anything is built for it.**
	// An argument wins; without one the desk-level file's `project.file` names
	// it, validated against this host before it is honoured; with neither, the
	// current directory, exactly as it always was. A configured default this
	// host cannot open refuses the launch rather than falling through.
	//
	// What comes back is the **descriptor**, not a name for one: validating a
	// pathname and then re-resolving it to open is a window in which the
	// directory checked is not the directory served. See `desk.OpenProject`.
	project, err := desk.OpenProject(flag.Arg(0), desk.DeskConfigDirFor(""))
	if err != nil {
		return err
	}
	absProject := project.Dir()

	static, err := fs.Sub(embeddedWeb, "web/dist")
	if err != nil {
		return fmt.Errorf("locating embedded assets: %w", err)
	}

	// The **launch secret**: what `GET /launch` trades once for a session
	// cookie, and what a script presents as `Authorization: Bearer`. It is not
	// the session, and it never rides on a request query — see
	// `internal/desk/session.go`.
	token := *devToken
	if token == "" {
		if token, err = desk.NewToken(); err != nil {
			return fmt.Errorf("generating the launch secret: %w", err)
		}
	}

	srv, err := desk.New(desk.Config{
		Root:     project,
		JpackBin: *jpackBin,
		Token:    token,
		Static:   static,
		DevMode:  *devToken != "",
		Logger:   log.New(os.Stderr, "", log.LstdFlags),
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
		// **The launch path, not the page.** Opening this URL exchanges the
		// secret for the `jpack-desk-session` cookie and redirects to `/`, so
		// what ends up in the address bar is `/` and the secret is in no
		// history entry, no `Referer` and no later request.
		fmt.Printf("judgment-pack desk\n  project: %s\n  runtime: %s\n  open:    http://%s/launch?secret=%s\n", absProject, *jpackBin, addr, token)
	}
	if err := httpSrv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	return nil
}
