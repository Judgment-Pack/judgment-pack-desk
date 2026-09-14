/** Keyless integration proof: actual Vercel adapter + actual jpack MCP + scripted model. */
import assert from 'node:assert/strict'
import { createHash } from 'node:crypto'
import { mkdtemp, readFile, readdir, rename, writeFile } from 'node:fs/promises'
import { tmpdir } from 'node:os'
import { join, resolve } from 'node:path'
import { Client } from '@modelcontextprotocol/sdk/client/index.js'
import { StdioClientTransport } from '@modelcontextprotocol/sdk/client/stdio.js'
import scenario from '../src/assistant/conformance/scenario.json'
import { loadEngine } from '../src/assistant/engines'
import { allowedTools, gateTransport } from '../src/assistant/toolGate'
import { normalize } from '../src/assistant/thinking'
import type { CallTool, McpToolResult, ModelCall } from '../src/assistant/engine'
import { checkCandidate } from '../src/assistant/authoring/checkCandidate'
import { engineTurn } from '../src/assistant/authoring/engineTurn'
import { newAuthoringRun, runAuthoring, type AuthoringCase, type AuthoringCheckpoint } from '../src/assistant/authoring/run'

const binary = process.argv[2]
if (!binary) throw new Error('Usage: npm run authoring:proof -- /absolute/path/to/jpack [report.json]')
const directory = await mkdtemp(join(tmpdir(), 'jpack-authoring-proof-'))
const project = join(directory, 'project')
await import('node:fs/promises').then(fs => fs.mkdir(project))
const checkpointPath = join(directory, 'checkpoint.json')
const permitted = ['get_schema', 'list_examples', 'get_example', 'validate', 'experimental_evaluate']
const client = new Client({ name: 'jpack-authoring-proof', version: '1' })
let rewrites = 0
const transport = gateTransport(new StdioClientTransport({ command: resolve(binary), args: ['mcp'], cwd: project }), {
  allowed: allowedTools(permitted), onGuardrail: notice => { if (notice.action === 'rewrote') rewrites++ }
})
const outcome = (outcomeId: string) => ({ kind: 'outcome', outcomeId, reasons: [], handoff: { state: 'none' } })
const unresolved = (reason: string) => ({ kind: 'unresolved', reasons: [reason], handoff: { state: 'requested', triggeredBy: [reason] } })
const row = (id: string, amount: string | undefined, receipt: boolean, expectedDisposition: unknown): AuthoringCase => ({
  id, facts: { claim: { type: 'expense-reimbursement', ...(amount === undefined ? {} : { amount }), receiptAttached: receipt } },
  expectedDisposition, expectationSource: 'Synthetic expense policy and explicit missing-fact expectation, fixed before execution.'
})
const cases = [row('small', '10', false, outcome('approve')), row('boundary-50', '50', false, outcome('approve')),
  row('no-receipt', '75', false, outcome('decline')), row('receipt', '75', true, outcome('approve')),
  row('boundary-1000', '1000', true, outcome('approve')), row('above-ceiling', '1000.01', true, unresolved('exception-escalation'))]
const additional = row('missing-amount', undefined, true, unresolved('unknown'))
const seed = structuredClone(scenario.documents.DRAFT_V2)
type Pack = typeof seed

/** Repairs are selected from feedback carried in the request, never a call counter. */
function nextCandidate(context: { candidate: string | null; feedback: { valid: boolean; cases: { id: string; passed: boolean }[] } | null }): Pack {
  const draft: Pack = context.candidate ? JSON.parse(context.candidate) : structuredClone(seed)
  if (!context.candidate) {
    delete (draft.decision as { question?: string }).question
    ;(draft.rules[0].when as { operator: string }).operator = 'less-than'
    draft.rules[2].outcome = 'approve'
    draft.exceptions[0].when.operator = 'greater-than-or-equal'
  } else if (!context.feedback?.valid) draft.decision.question = seed.decision.question
  else {
    const failed = new Set(context.feedback.cases.filter(item => !item.passed).map(item => item.id))
    if (failed.has('boundary-50')) (draft.rules[0].when as { operator: string }).operator = 'less-than-or-equal'
    else if (failed.has('no-receipt')) draft.rules[2].outcome = 'decline'
    else if (failed.has('boundary-1000')) draft.exceptions[0].when.operator = 'greater-than'
  }
  return draft
}

let modelCalls = 0
let toolCalls = 0
const modelCall: ModelCall = async (_suffix, request) => {
  const body = JSON.parse(request.body)
  const text = body.messages.find((message: { role: string }) => message.role === 'user').content as string
  const context = JSON.parse(text.split('AUTHORING ITERATION\n')[1].split('\nPropose a corrected candidate.')[0])
  const candidate = nextCandidate(context)
  const results = body.messages.filter((message: { role: string }) => message.role === 'tool')
  const returned = results.length ? JSON.parse(results[0].content) : null
  const validation = returned?.structuredContent ?? (returned?.content ? JSON.parse(returned.content[0].text) : returned)
  const invalid = validation?.status === 'invalid'
  const n = results.length
  let name: string | undefined
  let args: Record<string, unknown> = {}
  if (n === 0) { name = 'validate'; args = { document: JSON.stringify(candidate) } }
  else if (!invalid && n <= cases.length) {
    name = 'experimental_evaluate'
    // Omit rehearsal deliberately: the real transport gate must rewrite it.
    args = { pack: JSON.stringify(candidate), facts: JSON.stringify(cases[n - 1].facts) }
  }
  modelCalls++
  return Response.json({ id: `proof-${modelCalls}`, object: 'chat.completion', created: 0, model: 'scripted-author',
    choices: [{ index: 0, finish_reason: name ? 'tool_calls' : 'stop', message: name ?
      { role: 'assistant', content: null, tool_calls: [{ id: `call-${n}`, type: 'function', function: { name, arguments: JSON.stringify(args) } }] } :
      { role: 'assistant', content: '```json\n' + JSON.stringify({ proposal: { kind: 'create', document: candidate, unknowns: [] } }) + '\n```' } }],
    usage: { prompt_tokens: 1, completion_tokens: 1, total_tokens: 2 } })
}

try {
  await client.connect(transport)
  const runtime = client.getServerVersion()
  const binaryDigest = createHash('sha256').update(await readFile(binary)).digest('hex')
  const runtimeIdentity = `${runtime?.name} ${runtime?.version} sha256:${binaryDigest}`
  const tools = (await client.listTools()).tools.filter(tool => permitted.includes(tool.name))
  const prompt = await client.getPrompt({ name: 'author_pack', arguments: { description: scenario.policy } })
  const instructions = prompt.messages.map(message => message.content.type === 'text' ? message.content.text : '').join('\n')
  const callTool: CallTool = async (name, args) => {
    toolCalls++
    return await client.callTool({ name, arguments: args }) as McpToolResult
  }
  const engine = await loadEngine('vercel')
  const propose = engineTurn(engine, { prompt: instructions, testPrompt: '', tools, callTool,
    model: { family: 'openai-compatible', model: 'scripted-author', call: modelCall }, thinking: normalize('off', 'openai-compatible') })
  let interruption = new AbortController()
  let interrupted = false
  const save = async (checkpoint: AuthoringCheckpoint) => {
    await writeFile(checkpointPath + '.tmp', JSON.stringify(checkpoint))
    await rename(checkpointPath + '.tmp', checkpointPath)
    if (!interrupted && checkpoint.revisions.length === 2 && checkpoint.stage === 'draft') {
      interrupted = true
      interruption.abort()
    }
  }
  const ports = { propose, save,
    check: (document: string, rows: AuthoringCase[], signal: AbortSignal) => checkCandidate(document, rows, callTool, runtimeIdentity, signal),
    review: async (context: AuthoringCheckpoint) => ({
      cases: context.cases.some(item => item.id === additional.id) ? [] : [additional], questions: [],
      summary: 'Scripted independent challenge: verify missing amount remains unresolved.'
    })
  }
  const initial = newAuthoringRun('expense-proof', 'unsaved', scenario.policy, cases)
  const before = await readdir(project)
  const first = await runAuthoring(initial, ports, { baseline: 'unsaved', runtimeIdentity, maxRevisions: 10, signal: interruption.signal })
  assert.equal(first.status, 'interrupted', first.detail)
  const persisted: AuthoringCheckpoint = JSON.parse(await readFile(checkpointPath, 'utf8'))
  assert.equal(persisted.revisions.length, 2)
  interruption = new AbortController()
  const final = await runAuthoring(persisted, ports, { baseline: 'unsaved', runtimeIdentity, maxRevisions: 10, signal: interruption.signal })
  assert.equal(final.status, 'ready', final.detail)
  assert.equal(final.revisions.length, 5)
  assert.equal(final.cases.length, 7)
  assert.deepEqual(final.cases.slice(0, cases.length), cases)
  assert(final.revisions.at(-1)?.check?.cases.every(item => item.passed))
  assert.deepEqual(await readdir(project), before)
  assert(modelCalls > 20, `Expected a long task; observed ${modelCalls} model requests`)
  assert(rewrites > 0)
  const report = { proofVersion: 1, model: 'Scripted responses through the installed Vercel adapter; not a live-model quality benchmark.',
    runtime: runtimeIdentity, revisions: final.revisions.map((revision, index) => ({ revision: index + 1, digest: revision.digest,
      valid: revision.check?.valid, passed: revision.check?.cases.filter(item => item.passed).length,
      failed: revision.check?.cases.filter(item => !item.passed).map(item => item.id) })),
    modelCalls, toolCalls, rehearsalRewrites: rewrites, restoredFromDisk: true, finalStatus: final.status,
    finalTestCases: final.cases.length, projectFilesUnchanged: true, checkpointPath }
  if (process.argv[3]) await writeFile(resolve(process.argv[3]), JSON.stringify(report, null, 2) + '\n')
  console.log(JSON.stringify(report, null, 2))
} finally {
  await client.close()
}
