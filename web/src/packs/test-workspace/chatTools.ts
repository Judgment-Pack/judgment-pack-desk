import {toolInputFailure} from '../../assistant/failures'
import type { HostTool, McpToolResult } from '../../assistant/engine'
import type { ChatAttachment } from '../../chat/store'
import { digestOf, jsonIdentity } from '../../research/checkCandidate'
import { additionFindings, currentCoverage, latestCoverage, missingProbes, missingTestsRequest } from './coverage'
import { inspectTestFactTypes, testMatrixContract, validateTestProposal, type TestProposal } from './proposals'
import type { TestSuite } from './model'

export interface TestChatTarget { owner: string; text: string; title: string; draft: boolean }
export interface TestChatPorts {
  chatId: string
  target: (owner: string | undefined, signal: AbortSignal) => Promise<TestChatTarget>
  suite: (owner: string) => Promise<TestSuite>
  call: (name: string, args: Record<string, unknown>) => Promise<McpToolResult>
  guidance: (text: string, signal: AbortSignal) => Promise<string>
  sources: () => ChatAttachment[]
  request: () => string
  model: () => string
  save: (owner: string, proposal: TestProposal, expected: TestSuite) => Promise<void>
}
const schema = (properties: Record<string, unknown>, required: string[] = []) => ({ type: 'object', properties, required, additionalProperties: false })
const ownerSchema = { type: 'string', minLength: 1, maxLength: 256, description: 'Configured pack ID or draft ID. Omit for the pack linked to this chat.' }
const result = (value: unknown, isError = false): McpToolResult => ({ ...(isError ? { isError: true } : {}), content: [{ type: 'text', text: JSON.stringify(value) }] })
export function testReviewHref(target: Pick<TestChatTarget, 'owner' | 'draft'>, chatId: string, proposalId: string) {
  const path = target.draft ? `/packs/drafts/${encodeURIComponent(target.owner)}` : `/packs/${encodeURIComponent(target.owner)}/matrix`
  return `${path}?${new URLSearchParams({ chat: chatId, testProposal: proposalId })}`
}

/** Test design is a capability of the existing chat. Only reviewed saves change cases. */
export function testChatTools(ports: TestChatPorts): HostTool[] {
  const snapshots = new Map<string, { target: TestChatTarget; suite: TestSuite; digest: string; coverageRunId?: string }>()
  return [
    { name: 'read_pack_tests', delegation: 'read', description: 'Read a pack, its saved cases, observed results and runtime test-authoring instructions. Required before proposing test cases. Does not run tests or change a pack.', inputSchema: schema({ owner: ownerSchema, coverageRunId: { type: 'string', description: 'For Design missing tests, the coverage run named by the user.' } }), execute: async (args, signal) => {
      const target = await ports.target(typeof args.owner === 'string' ? args.owner : undefined, signal)
      const suite = await ports.suite(target.owner)
      const digest = await digestOf(target.text)
      const currentRun=latestCoverage(suite,digest)
      let coverage = currentRun ? JSON.stringify({runId:currentRun.id,missingProbes:missingProbes(currentRun),instruction:'Consider these probes when design is requested; explain infeasible or policy-dependent coverage. Do not generate cases for ordinary diagnostic questions.'}) : 'Coverage has not been checked for this exact pack and suite. Consider normal decisions, exceptions, missing evidence, boundaries and fallthrough from the start. Do not infer expectations from observations.'
      if (args.coverageRunId !== undefined) {
        const run = suite.runs.find(run => run.id === args.coverageRunId)
        if (!run || !currentCoverage(run, suite.cases, digest)) throw toolInputFailure('Coverage changed. Run the current suite before designing from these gaps.')
        coverage = missingTestsRequest(run)
      }
      const contract = await testMatrixContract(ports.call, signal)
      const guidance = await ports.guidance(target.text, signal)
      let factChecks: unknown = []
      if (suite.cases.length) {
        try { factChecks = await inspectTestFactTypes(target.text, suite.cases.map(c => c.row), ports.call, signal) }
        catch (e) { signal.throwIfAborted(); factChecks = {status:'unavailable', message:(e as Error).message} }
      }
      signal.throwIfAborted()
      snapshots.set(target.owner, { target, suite, digest, ...(typeof args.coverageRunId === 'string' ? { coverageRunId: args.coverageRunId } : {}) })
      return result({ owner: target.owner, title: target.title, pack: target.text, packDigest: digest, contract, runtimeGuidance: guidance, factChecks,
        instruction: 'Answer the current question. Propose cases only when requested, using propose_test_cases, never a pack proposal fence. Do not execute tests as part of design. Expected results come from requirements, not observed outputs. JPS ordered comparisons require decimal strings for operands AND facts; JSON numbers are not comparable. Preserve intentional wrong-type tests and missing, null, boolean and zero distinctions. Do not convert pack thresholds to JSON numbers. Ask about ambiguous policy with ask_user. Every proposal needs review before saving.',
        cases: suite.cases.map(c => ({ row: c.row, rationale: c.rationale, intentionalFactTypes: c.intentionalFactTypes ?? [] })),
        recentResults: suite.runs.slice(-3).map(run => ({ at: run.at, matchesCurrentPack: run.packDigest === digest, report: run.report, trial: run.trial, error: run.error })),
        previousQuestions: suite.proposals?.at(-1)?.attempts.at(-1)?.unknowns ?? [],
        sources: [...new Map([...ports.sources(), ...suite.cases.flatMap(c => c.sources)].map(source => [source.id,source])).values()].map(source => ({ id: source.id, name: source.name, text: source.text, document: source.document })), coverage })
    } },
    { name: 'propose_test_cases', description: 'Retain a validated test-case proposal in this session for human review. Does not save cases, modify a pack, or run tests. Read read_pack_tests first. Use this for a requested proposal, not for diagnostic questions.', inputSchema: schema({ owner: ownerSchema, document: { type: 'string', maxLength: 1048576, description: 'JSON test matrix, matrixVersion 3. Each row has id, facts, exactly one complete expectedDisposition or expectedErrorClass, and optional focus. Desk sourceMappings and intentionalFactTypes may be included at the root. intentionalFactTypes is an array of {caseId, path, reason, within?} explaining deliberate wrong-type tests only; path is relative to facts, within is the collection pointer when reported by runtime. Do not use it to hide accidental encoding.' }, summary: { type: 'string', maxLength: 4000 }, unknowns: { type: 'array', maxItems: 20, items: { type: 'string', minLength: 1, maxLength: 2000 } } }, ['document', 'summary']), execute: async (args, signal) => {
      const target = await ports.target(typeof args.owner === 'string' ? args.owner : undefined, signal)
      const snapshot = snapshots.get(target.owner)
      if (!snapshot || snapshot.target.text !== target.text) throw toolInputFailure('Read the current pack and test suite with read_pack_tests before proposing cases.')
      if (typeof args.document !== 'string' || new TextEncoder().encode(args.document).length > 1048576 || typeof args.summary !== 'string' || !args.summary.trim() || args.summary.length > 4000) throw toolInputFailure('Provide a test matrix up to 1 MiB and a brief explanation.')
      const unknowns = args.unknowns ?? []
      if (!Array.isArray(unknowns) || unknowns.length > 20 || unknowns.some(q => typeof q !== 'string' || !q.trim() || q.length > 2000)) throw toolInputFailure('Provide nonempty policy questions.')
      const document: unknown = JSON.parse(args.document)
      const sources = structuredClone([...new Map([...ports.sources(), ...snapshot.suite.cases.flatMap(c => c.sources)].map(source => [source.id,source])).values()])
      const findings = await validateTestProposal(document, sources, ports.call, signal, target.text)
      if (snapshot.coverageRunId) findings.push(...additionFindings(document, snapshot.suite.cases.map(c => c.id)))
      if (findings.length) return result({ status: 'invalid', findings, message: 'No proposal retained. Correct accidental representation errors while preserving the intended values and expectations. Keep deliberately wrong-type inputs and explain them in intentionalFactTypes. Ask the user when policy is unresolved.' }, true)
      const current = await ports.target(target.owner, signal)
      if (current.text !== target.text) throw toolInputFailure('The pack changed during validation. Read it again before proposing cases.')
      const proposal: TestProposal = { id: crypto.randomUUID(), chatId: ports.chatId, at: new Date().toISOString(), request: ports.request(), packDigest: snapshot.digest, model: ports.model(), contractVersion: '1', sources,
        caseSnapshots: Object.fromEntries(snapshot.suite.cases.map(c => [c.id, jsonIdentity(c)])),
        ...(snapshot.coverageRunId ? { additionsOnly: true, coverageRunId: snapshot.coverageRunId } : {}),
        state: 'ready', message: 'The proposal is ready for review. No changes have been saved.',
        attempts: [{ document, unknowns, message: args.summary.trim(), findings: [] }] }
      signal.throwIfAborted()
      await ports.save(target.owner, proposal, snapshot.suite)
      return result({ status: 'needs-review', proposalId: proposal.id, reviewUrl: testReviewHref(target, ports.chatId, proposal.id), message: proposal.message })
    } },
  ]
}
