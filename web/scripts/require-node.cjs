// Refuse an old Node with a sentence, not a stack trace.
//
// Vite and the toolchain need Node 22 (package.json "engines"), and npm does
// not enforce engines on a script run: a machine whose PATH reaches a system
// Node first fails inside Vite's first `import` with "Unexpected token {",
// which names nothing. This runs before `dev` and `build` — as CommonJS, so
// the old Node can at least execute it — and says what to do.
const major = Number(process.versions.node.split('.')[0])
if (!(major >= 22)) {
  process.stderr.write(
    `judgment-pack desk needs Node 22 or newer; this shell runs Node ${process.versions.node}.\n` +
      'Run `nvm use` in the repository (its .nvmrc says 22), or put Node 22 first on PATH.\n'
  )
  process.exit(1)
}
