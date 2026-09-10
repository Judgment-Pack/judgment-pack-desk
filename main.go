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
	"net"
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
		port     = flag.Int("port", 8791, "loopback TCP port to listen on; 0 lets the kernel choose one, and the printed URL names it")
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
	// **The runtime is resolved here, once, or the desk does not start.** A
	// name is looked up on PATH; a path must exist and be executable. Failing
	// per relay connection instead was a retry loop the page could not explain.
	runtimeBin, err := desk.ResolveRuntime(*jpackBin)
	if err != nil {
		return err
	}

	// **The listener first, and the port read off it.** The handoff cookie's
	// name carries the port, so the chassis has to be told which port it is
	// actually on — and with `--port 0` the flag does not know: the kernel
	// picks one when the socket binds. Binding here and serving this listener
	// is what makes `--port 0` a working desk rather than one whose cookie is
	// named for a port nothing is on.
	listener, err := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", *port))
	if err != nil {
		return err
	}
	defer listener.Close()
	bound := listener.Addr().(*net.TCPAddr).Port

	static, err := fs.Sub(embeddedWeb, "web/dist")
	if err != nil {
		return fmt.Errorf("locating embedded assets: %w", err)
	}

	// The **launch secret**: what `GET /launch` trades once for a single-use
	// handoff cookie, and what a script presents as `Authorization: Bearer`. It
	// is not the session, and it never rides on a request query — see
	// `internal/desk/session.go`.
	token := *devToken
	if token == "" {
		if token, err = desk.NewToken(); err != nil {
			return fmt.Errorf("generating the launch secret: %w", err)
		}
	}

	srv, err := desk.New(desk.Config{
		Root:     project,
		JpackBin: runtimeBin,
		// The port this listener binds, handed over because the handoff
		// cookie's name carries it: a cookie's origin has no port, so two
		// desks on one host would otherwise share one handoff.
		Port:    bound,
		Token:   token,
		Static:  static,
		DevMode: *devToken != "",
		Logger:  log.New(os.Stderr, "", log.LstdFlags),
	})
	if err != nil {
		return err
	}
	defer srv.Close()

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	addr := fmt.Sprintf("127.0.0.1:%d", bound)
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
		// **The launch path, not the page.** Opening this URL trades the secret
		// for a sixty-second, single-use handoff cookie and redirects to `/`,
		// so what ends up in the address bar is `/` and the secret is in no
		// later request. The page then exchanges that handoff for a session id
		// it holds itself. See `internal/desk/session.go`.
		fmt.Printf("judgment-pack desk\n  project: %s\n  runtime: %s\n  open:    http://%s/launch?secret=%s\n", absProject, runtimeBin, addr, token)
		if *devToken != "" {
			// **In dev mode the page to open is Vite's, not this one.** This
			// process serves whatever `web/dist` held when it was built, which
			// is stale the moment the page source changes; the dev server
			// serves the source and proxies `/launch`, `/ws` and `/api` here.
			// A person who opened the line above and met a page from an older
			// build is who these two lines are for.
			fmt.Printf("  dev:     this serves the bundle built into web/dist; for hot reload run\n"+
				"           JPACK_DESK_CHASSIS=http://%s npm --prefix web run dev\n"+
				"           and open http://localhost:5173/launch?secret=%s\n", addr, token)
		}
	}
	if err := httpSrv.Serve(listener); err != nil && !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	return nil
}
