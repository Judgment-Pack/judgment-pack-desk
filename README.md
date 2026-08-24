# judgment-pack-desk

A local web desk for a Judgment Pack project. `jpack-desk` is one Go binary: it
serves a single-page application and relays JSON-RPC between that page and a
`jpack mcp` subprocess running in your project directory.

The browser is the MCP client. The Go program is a chassis, not a feature
server — it has no per-feature endpoints and parses none of the traffic it
carries, so the desk can show anything the runtime's tools expose without a
matching change on the Go side.

## What it shows

The first view is a pack browser:

- `/` lists the packs the project declares, via the runtime's `list_packs`.
- `/packs/:id` fetches one document with `get_pack` and renders it: the
  decision, outcomes, applicability, evidence requirements, rules, exceptions,
  escalation, sources and metadata — plus a **Raw JSON** tab holding the
  document exactly as it is on disk.

Conditions are shown as formatted JSON rather than paraphrased into English. A
paraphrase of a policy condition would be a claim about what the policy means,
and only the document gets to make that claim.

## Requirements

- Go 1.24 or newer
- Node 20.19+ or 22.12+ (for building the SPA)
- A `jpack` binary — the [judgment-pack runtime](https://github.com/Judgment-Pack/judgment-pack-runtime)

## Production mode

One binary with the SPA embedded:

```sh
npm --prefix web ci
npm --prefix web run build     # emits web/dist, which go:embed picks up
go build -o bin/jpack-desk .

./bin/jpack-desk --jpack /path/to/jpack /path/to/project
```

It prints the URL to open, including the session token:

```
judgment-pack desk
  project: /path/to/project
  runtime: /path/to/jpack
  open:    http://127.0.0.1:8791/?token=1f3c…
```

`projectDir` defaults to the current directory. `--jpack` defaults to `jpack`
on `PATH`, and `--port` defaults to `8791`.

## Development mode

Two processes: the chassis for the relay, Vite for hot reload.

```sh
# terminal 1 — chassis with a fixed token so the URL is stable across restarts
# (flags come before the project directory: Go stops parsing flags at the
# first positional argument)
go run . --dev-token dev --port 8791 --jpack /path/to/jpack /path/to/project

# terminal 2 — Vite dev server, proxying /ws to the chassis
npm --prefix web run dev
```

Then open <http://localhost:5173/?token=dev>.

Vite proxies `/ws` to `127.0.0.1:8791` (override with `JPACK_DESK_CHASSIS`).
Passing `--dev-token` is what additionally permits the Vite dev server's origin
— without it the chassis refuses the proxied upgrade, because the browser's
`Origin` is the dev server's and never matches the host it reaches the chassis
under.

To check a running chassis end to end with the desk's own client code:

```sh
npm --prefix web run smoke -- 'http://127.0.0.1:8791/?token=dev'
```

## Security model

The desk drives a runtime that reads your project. Three things bound it:

- **Loopback only.** The listener binds `127.0.0.1`. Nothing off the machine
  can reach it.
- **A session token.** A random token is generated at startup and printed in
  the URL. `/ws` requires it as `?token=`, compared in constant time. Static
  assets are served without it; the relay is the capability, and it is the
  thing that is gated.
- **An origin check.** A WebSocket upgrade whose `Origin` is not the origin the
  page was served from is refused. This is what stops a page on another site
  from opening a socket to your loopback and driving the runtime through your
  own browser — a token in a URL you have visited is not, by itself, protection
  against that. A request with no `Origin` at all is not from a browser, and
  the token is its authorization.

The chassis holds no credential, opens no outbound connection, and writes
nothing to the project. The runtime subprocess inherits the project directory
as its working directory and is killed when the socket that started it closes.

## How the relay works

- One WebSocket connection spawns one `jpack mcp` subprocess with `cwd` set to
  the project directory.
- Bytes cross verbatim: one JSON-RPC message per WebSocket text frame on the
  browser side, newline-delimited JSON on the stdio side.
- Closing the socket kills the subprocess.
- One message originates in the chassis rather than the runtime: a file watcher
  over the project tree sends a `desk/fileChanged` JSON-RPC notification
  carrying the changed path, and the page invalidates its caches on it.

## Layout

```
main.go              flags, embedded assets, HTTP server
internal/desk/
  server.go          routing, SPA fallback, token and origin checks
  relay.go           WebSocket ↔ `jpack mcp` subprocess
  watch.go           project-tree file watching
web/                 Vite + React + TypeScript SPA
  src/mcp/           the MCP client: transport, connection, queries
  src/routes/        pack list and pack detail
  src/components/    the semantic document view
```

## Tests

```sh
go test ./...
```

The relay's end-to-end tests need a runtime and a project, and skip without
them:

```sh
go build -C /path/to/judgment-pack-runtime -o "$PWD/bin/jpack" ./cmd/jpack
JPACK_PROJECT=/path/to/project go test ./...
```

`JPACK_BIN` overrides the runtime binary; otherwise `./bin/jpack` is used when
present.

## License

Apache-2.0. See [LICENSE](LICENSE).
