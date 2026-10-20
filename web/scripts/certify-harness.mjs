#!/usr/bin/env node
// Run the harness package's certification on the Desk's own wiring, one adapter
// at a time: `HARNESS_SOURCE=<packages/harness> node scripts/certify-harness.mjs --adapter vercel|codex|deepagents`.
//
// It is the package's own `scripts/certify.mjs` over the Desk's test ground
// (`vitest.certification.config.mts`): the suite's checks of the recorded
// scenario and of the seal, with the legs of every other adapter left out, and
// then the adapter's own legs, by its kind:
//
// - `vercel`, a model engine: `engine vercel`, `engine vercel · thinking`,
//   `engine vercel · strict alternation`, `engine vercel · text tool calls`,
//   `engine vercel · scenarios` and `engine vercel · delegation`;
// - `codex`, an agent engine: `agent engine codex` (run from the adapter's own
//   test file) and `agent engine codex · scenarios`.
//
// **A run in which none of the adapter's own checks ran fails**, as the
// package's does: a name pattern that matches nothing passes in vitest, so the
// checks are counted from its JSON report. **The adapters are the registry's
// own**: the pair named here is read against `ENGINE_CAPABILITIES` in the
// package's `engines/index.ts` before anything runs, so an adapter added to the
// package fails this until the Desk's job names it.
import { spawnSync } from 'node:child_process'
import { mkdtempSync, readFileSync, rmSync } from 'node:fs'
import { tmpdir } from 'node:os'
import { dirname, join, resolve } from 'node:path'
import { fileURLToPath } from 'node:url'

const web = resolve(dirname(fileURLToPath(import.meta.url)), '..')
const source = process.env.HARNESS_SOURCE
if (!source) {
  console.error('certify-harness: HARNESS_SOURCE must name packages/harness of a checkout at the locked revision')
  process.exit(2)
}
const pkg = resolve(web, source)

/** The adapters the Desk certifies, and the kind of each. */
const ADAPTERS = { vercel: 'model', codex: 'agent', deepagents: 'model' }

const argument = (() => {
  const argv = process.argv.slice(2)
  for (let i = 0; i < argv.length; i++) {
    if (argv[i] === '--adapter') return argv[i + 1]
    if (argv[i].startsWith('--adapter=')) return argv[i].slice('--adapter='.length)
  }
  return undefined
})()
if (!Object.hasOwn(ADAPTERS, argument ?? '')) {
  console.error(`usage: certify-harness.mjs --adapter <${Object.keys(ADAPTERS).join('|')}>`)
  process.exit(2)
}
const adapter = argument
const kind = ADAPTERS[adapter]

// The registry the package declares, read from its source: the engines this job names must be exactly its own, kind for kind.
const registry = readFileSync(join(pkg, 'engines/index.ts'), 'utf8')
const declared = Object.fromEntries([...(/ENGINE_CAPABILITIES\s*=\s*\{([^}]*)\}/.exec(registry)?.[1] ?? '').matchAll(/(\w+)\s*:\s*'(\w+)'/g)].map((match) => [match[1], match[2]]))
if (JSON.stringify(Object.entries(declared).sort()) !== JSON.stringify(Object.entries(ADAPTERS).sort())) {
  console.error(`certify-harness: the package declares the engines ${JSON.stringify(declared)} and this job certifies ${JSON.stringify(ADAPTERS)}; name every engine the package has`)
  process.exit(1)
}

const vitest = resolve(web, 'node_modules/.bin/vitest')
const reports = mkdtempSync(join(tmpdir(), 'certify-harness-'))
const config = resolve(web, 'vitest.certification.config.mts')

function run(args, report, selectedConfig = config) {
  const result = spawnSync(vitest, ['run', '--maxWorkers=2', '--config', selectedConfig, ...args, '--reporter=default', '--reporter=json', `--outputFile.json=${report}`], {
    cwd: web,
    stdio: 'inherit',
    env: { ...process.env, HARNESS_SOURCE: pkg }
  })
  if (result.error) throw result.error
  return result.status ?? 1
}

const own = kind === 'agent'
  ? (top) => top === `agent engine ${adapter}` || top === `agent engine ${adapter} · scenarios`
  : (top) => [`engine ${adapter}`, `engine ${adapter} · thinking`, `engine ${adapter} · strict alternation`, `engine ${adapter} · text tool calls`, `engine ${adapter} · scenarios`, `engine ${adapter} · delegation`].includes(top)

function ownChecks(report) {
  let parsed
  try {
    parsed = JSON.parse(readFileSync(report, 'utf8'))
  } catch {
    return 0
  }
  let count = 0
  for (const file of parsed.testResults ?? []) {
    for (const test of file.assertionResults ?? []) {
      if (test.status === 'passed' && own(test.ancestorTitles?.[0] ?? '')) count++
    }
  }
  return count
}

// The legs of every other adapter are left out by their describe names.
const others = Object.keys(ADAPTERS).filter((id) => id !== adapter)
const leaveOut = `^(?!(agent )?engine (${others.join('|')})\\b)`
const runs = [['certification', '--testNamePattern', leaveOut]]
if (kind === 'agent') runs.push([`engines/${adapter}/${adapter}.test.ts`, '--testNamePattern', `^agent engine ${adapter}\\b`])

let status = 0
let checks = 0
try {
  // The package checks credential exclusion; this mandatory Desk row checks
  // what the actual configureHarness functions supply. An omitted row fails too.
  const credentials = join(reports, 'desk-credentials.json')
  const credentialCode = run(['src/assistant/harnessCredentialsCertification.test.ts'], credentials, resolve(web, 'vitest.config.ts'))
  const credentialReport = JSON.parse(readFileSync(credentials, 'utf8'))
  const credentialChecks = (credentialReport.testResults ?? []).flatMap((file) => file.assertionResults ?? []).filter((test) => test.status === 'passed' && test.title === 'certifies the Desk configureHarness credentials')
  if (credentialCode !== 0 || credentialChecks.length !== 1) status = credentialCode || 1
  for (const [index, args] of runs.entries()) {
    if (status !== 0) break
    console.log(`certify-harness ${adapter}: vitest run ${args.join(' ')}`)
    const report = join(reports, `run-${index}.json`)
    const code = run(args, report)
    if (code !== 0) status = code
    checks += ownChecks(report)
  }
} finally {
  rmSync(reports, { recursive: true, force: true })
}
if (checks === 0) {
  console.error(`certify-harness ${adapter}: no check of the ${kind} engine ${adapter} ran`)
  if (status === 0) status = 1
}
console.log(status === 0 ? `certify-harness ${adapter}: passed, ${checks} of the ${kind} engine's own checks among them` : `certify-harness ${adapter}: FAILED`)
process.exit(status)
