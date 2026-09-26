import type { TestProposal } from './proposals'
import type { ChatAttachment } from '../../chat/store'
import type { PackDraft } from '../drafts/model'
import type { PackTest } from '../../mcp/types'
import { digestOf, jsonIdentity } from '../../research/checkCandidate'
import type { RuntimeProbe } from '../../research/runtimeProbes'

export type ObjectValue = Record<string, unknown>
export const object = (v: unknown): v is ObjectValue =>
  v !== null && typeof v === 'object' && !Array.isArray(v)
export interface CaseRow extends ObjectValue {
  id: string
  facts: unknown
  evidenceAvailability?: unknown
  expectedDisposition?: unknown
  expectedHandoffTarget?: unknown
  expectedErrorClass?: string
  expectedErrorPhase?: string
  origin?: string
  focus?: string
}
export interface TestCase {
  id: string
  name: string
  revision: number
  row: CaseRow
  sources: ChatAttachment[]
  sourceMappings: Record<string, string>
  origin: 'manual' | 'ai' | 'draft' | 'import'
  rationale: string
}
export interface TestRun {
  id: string
  at: string
  packDigest: string
  packText: string
  packVersion: string
  cases: TestCase[]
  report?: PackTest
  trial?: RuntimeProbe
  evaluation?: unknown
  error?: string
  origin: 'draft' | 'tests'
}
export interface TestSuite {
  deleted?: string[]
  revision: number
  cases: TestCase[]
  runs: TestRun[]
  recovered: string[]
  research?: unknown
  proposals?: TestProposal[]
  messages?: { role: 'user' | 'assistant'; text: string; at: string; proposalId?: string }[]
}
export interface TestStore {
  version: 1
  suites: Record<string, TestSuite>
}
export const emptySuite = (): TestSuite => ({ revision: 0, cases: [], runs: [], recovered: [] })
export function decodeStore(v: unknown): TestStore {
  if (!object(v) || v.version !== 1 || !object(v.suites) || Object.keys(v.suites).length > 512)
    throw Error('Test storage cannot be read. The saved data has not been changed.')
  for (const s of Object.values(v.suites)) {
    if (
      !object(s) ||
      !Number.isSafeInteger(s.revision) ||
      !Array.isArray(s.cases) ||
      !Array.isArray(s.runs) ||
      !Array.isArray(s.recovered)
    )
      throw Error('Invalid saved test suite.')
    const ids = new Set<string>()
    for (const c of s.cases) {
      if (
        !object(c) ||
        typeof c.id !== 'string' ||
        ids.has(c.id) ||
        !object(c.row) ||
        c.row.id !== c.id ||
        typeof c.name !== 'string' ||
        !Number.isSafeInteger(c.revision) ||
        !Array.isArray(c.sources) ||
        !object(c.sourceMappings) ||
        typeof c.rationale !== 'string' ||
        c.sources.some(
          (f) =>
            !object(f) ||
            typeof f.id !== 'string' ||
            typeof f.name !== 'string' ||
            typeof f.text !== 'string',
        )
      )
        throw Error('Invalid saved case.')
      ids.add(c.id)
    }
    if (s.cases.length > 256 || s.recovered.some((k) => typeof k !== 'string'))
      throw Error('Invalid saved test suite.')
    if (
      s.messages !== undefined &&
      (!Array.isArray(s.messages) ||
        s.messages.some(
          (m) =>
            !object(m) ||
            !['user', 'assistant'].includes(String(m.role)) ||
            typeof m.text !== 'string' ||
            typeof m.at !== 'string' ||
            (m.proposalId !== undefined && typeof m.proposalId !== 'string'),
        ))
    )
      throw Error('Invalid test conversation.')
    if (
      s.proposals !== undefined &&
      (!Array.isArray(s.proposals) ||
        s.proposals.some(
          (p) =>
            !object(p) ||
            typeof p.id !== 'string' ||
            typeof p.at !== 'string' ||
            typeof p.request !== 'string' ||
            typeof p.packDigest !== 'string' ||
            typeof p.model !== 'string' ||
            (p.additionsOnly !== undefined && typeof p.additionsOnly !== 'boolean') ||
            (p.coverageRunId !== undefined && typeof p.coverageRunId !== 'string') ||
            p.contractVersion !== '1' ||
            !object(p.caseSnapshots) ||
            Object.values(p.caseSnapshots).some((v) => typeof v !== 'string') ||
            (p.savedCases !== undefined &&
              (!object(p.savedCases) || Object.values(p.savedCases).some((v) => typeof v !== 'string'))) ||
            !Array.isArray(p.sources) ||
            p.sources.some(
              (f) =>
                !object(f) ||
                typeof f.id !== 'string' ||
                typeof f.name !== 'string' ||
                typeof f.text !== 'string',
            ) ||
            !['generating', 'checking', 'correcting', 'ready', 'blocked', 'stopped', 'answered'].includes(
              String(p.state),
            ) ||
            typeof p.message !== 'string' ||
            !Array.isArray(p.attempts) ||
            p.attempts.length > 3 ||
            (p.state === 'ready' && p.attempts.length === 0) ||
            p.attempts.some(
              (a) =>
                !object(a) ||
                !Object.hasOwn(a, 'document') ||
                typeof a.message !== 'string' ||
                !Array.isArray(a.unknowns) ||
                a.unknowns.some((q) => typeof q !== 'string') ||
                !Array.isArray(a.findings) ||
                a.findings.some(
                  (f) =>
                    !object(f) ||
                    typeof f.code !== 'string' ||
                    typeof f.path !== 'string' ||
                    typeof f.message !== 'string',
                ),
            ),
        ))
    )
      throw Error('Invalid saved test proposal history.')
    for (const r of s.runs)
      if (
        !object(r) ||
        typeof r.id !== 'string' ||
        typeof r.packDigest !== 'string' ||
        typeof r.packText !== 'string' ||
        typeof r.at !== 'string' ||
        !Number.isFinite(Date.parse(r.at)) ||
        !Array.isArray(r.cases) ||
        r.cases.some((c) => !object(c) || typeof c.id !== 'string' || !object(c.row))
      )
        throw Error('Invalid test history.')
  }
  return v as unknown as TestStore
}
export function newCase(): TestCase {
  const id = 'case-' + crypto.randomUUID()
  return {
    id,
    name: '',
    revision: 0,
    row: { id, facts: {} },
    sources: [],
    sourceMappings: {},
    origin: 'manual',
    rationale: '',
  }
}
export function executable(c: TestCase): boolean {
  return c.row.expectedDisposition !== undefined || !!c.row.expectedErrorClass
}
export function matrix(cases: TestCase[]) {
  return {
    matrixVersion: '3',
    cases: cases.filter(executable).map((c) => ({
      ...c.row,
      id: c.id,
      focus: c.rationale || c.row.focus,
      origin: c.row.origin ?? c.origin,
    })),
  }
}
const rowFields = new Set([
  'id',
  'origin',
  'facts',
  'evidenceAvailability',
  'supportedExtensions',
  'expectedDisposition',
  'expectedHandoffTarget',
  'expectedErrorClass',
  'expectedErrorPhase',
  'focus',
  'specSection',
  'cites',
])
export function importMatrix(value: unknown, origin: TestCase['origin'] = 'import'): TestCase[] {
  if (!object(value) || !Array.isArray(value.cases) || value.cases.length > 256)
    throw Error('Choose a matrix JSON document with up to 256 cases.')
  if (value.matrixVersion !== undefined && !['1', '2', '3'].includes(String(value.matrixVersion)))
    throw Error('This test matrix version is not supported.')
  const ids = new Set<string>()
  return value.cases.map((raw, index) => {
    const path = `/cases/${index}`
    if (!object(raw)) throw Error(`${path}: case must be an object.`)
    if (typeof raw.id !== 'string' || !raw.id.trim()) throw Error(`${path}/id: supply a nonempty case ID.`)
    if (ids.has(raw.id)) throw Error(`${path}/id: duplicate case ID "${raw.id}".`)
    if (!Object.hasOwn(raw, 'facts')) throw Error(`${path}/facts: supply the facts document.`)
    const unsupported = Object.keys(raw).filter((k) => !rowFields.has(k))
    if (unsupported.length)
      throw Error(
        `${path}: unsupported fields ${unsupported.join(', ')}. Use focus for the case name and rationale.`,
      )
    if ((raw.expectedDisposition !== undefined) === (raw.expectedErrorClass !== undefined))
      throw Error('Each case must declare one expected disposition or expected error class.')
    if (raw.expectedErrorClass !== undefined && typeof raw.expectedErrorClass !== 'string')
      throw Error('Expected error class must be text.')
    ids.add(raw.id)
    return {
      id: raw.id,
      name: typeof raw.focus === 'string' ? raw.focus : raw.id,
      revision: 1,
      row: raw as CaseRow,
      sources: [],
      sourceMappings: {},
      origin,
      rationale: typeof raw.focus === 'string' ? raw.focus : '',
    }
  })
}
export function expectedLabel(row: CaseRow): string {
  if (row.expectedErrorClass) return `Refusal: ${row.expectedErrorClass}`
  if (!object(row.expectedDisposition)) return 'Not set'
  return String(row.expectedDisposition.outcomeId ?? row.expectedDisposition.kind ?? 'Not set')
}
export function latestResult(suite: TestSuite, c: TestCase, digest: string) {
  const run = [...suite.runs].reverse().find((r) => r.cases.some((x) => x.id === c.id))
  if (!run) return { label: 'Not run', status: 'not-run' }
  const saved = run.cases.find((x) => x.id === c.id)!
  if (run.packDigest !== digest || saved.revision !== c.revision || jsonIdentity(saved) !== jsonIdentity(c))
    return { label: 'Needs rerun', status: 'stale', run }
  if (run.trial) return { label: 'Exploratory', status: 'exploratory', run }
  if (run.error) return { label: 'Could not run', status: 'error', run }
  const result = run.report?.packs?.flatMap((p) => p.rows ?? []).find((r) => r.id === c.id)
  return {
    label:
      result?.status === 'passed'
        ? 'Passed'
        : result?.status === 'mismatch'
          ? 'Mismatch'
          : (result?.status ?? 'Not run'),
    status: result?.status ?? 'not-run',
    run,
  }
}
/** Recover exact recorded trials once. Never turn observed answers into expectations. */
export async function recoverDraft(suite: TestSuite, draft: PackDraft): Promise<TestSuite> {
  const token = 'draft:' + draft.id + ':' + draft.generation
  const state = draft.checkpoint.state
  // Restored checkpoints intentionally clear computed digests. Recompute from retained bytes.
  const candidates = await Promise.all(
    state.candidates.map(async (c) => ({ ...c, digest: await digestOf(c.text) })),
  )
  const marker =
    token +
    ':' +
    (await digestOf(
      jsonIdentity({
        probes: state.probes,
        cases: state.cases,
        checks: candidates.map((c) => [c.digest, c.check, c.previousCheck]),
      }),
    ))
  if (suite.recovered.includes(marker)) return suite
  const next = structuredClone(suite)
  for (const [i, probe] of (state.probes ?? []).entries()) {
    const id = `${token}:probe:${i}:${probe.documentDigest}`
    if (next.runs.some((r) => r.id === id)) continue
    const candidate = candidates.find((c) => c.digest === probe.documentDigest)
    next.runs.push({
      id,
      at: probe.at,
      packDigest: probe.documentDigest,
      packText: candidate?.text ?? '',
      packVersion: String((candidate?.document as ObjectValue)?.version ?? 'Draft'),
      cases: [],
      trial: probe,
      origin: 'draft',
    })
  }
  for (const row of state.cases) {
    if (next.deleted?.includes(row.id) || next.cases.some((c) => c.id === row.id)) continue
    const input: CaseRow = {
      id: row.id,
      facts: row.facts,
      expectedDisposition: row.expectedDisposition,
      ...(row.evidenceAvailability === undefined ? {} : { evidenceAvailability: row.evidenceAvailability }),
      ...(row.expectedHandoffTarget === undefined
        ? {}
        : { expectedHandoffTarget: row.expectedHandoffTarget }),
    }
    next.cases.push({
      id: row.id,
      name: row.id,
      revision: 1,
      row: input,
      sources: draft.documents ?? [],
      sourceMappings: {},
      origin: 'draft',
      rationale: row.rationale + ' [' + row.expectationSource + ']',
    })
  }
  for (const candidate of candidates) {
    const check = candidate.check ?? candidate.previousCheck
    if (!check?.cases.length) continue
    const id = token + ':check:' + check.documentDigest + ':' + (await digestOf(jsonIdentity(check.cases)))
    if (next.runs.some((r) => r.id === id)) continue
    // Old checks did not retain their input snapshots. Keep the report historical,
    // never bind a current case's edited inputs to that past result.
    const snapshots: TestCase[] = []
    next.runs.push({
      id,
      at: draft.updatedAt,
      packDigest: check.documentDigest,
      packText: check.documentDigest === candidate.digest ? candidate.text : '',
      packVersion: String((candidate.document as ObjectValue)?.version ?? 'Draft'),
      cases: snapshots,
      origin: 'draft',
      report: {
        status: check.cases.every((r) => r.passed) ? 'passed' : 'mismatch',
        summary: {
          total: check.cases.length,
          passed: check.cases.filter((r) => r.passed).length,
          mismatched: check.cases.filter((r) => !r.passed).length,
        },
        packs: [
          {
            id: draft.id,
            status: check.cases.every((r) => r.passed) ? 'passed' : 'mismatch',
            summary: {
              total: check.cases.length,
              passed: check.cases.filter((r) => r.passed).length,
              mismatched: check.cases.filter((r) => !r.passed).length,
            },
            rows: check.cases.map((r) => ({
              id: r.id,
              status: r.refused ? 'refused' : r.passed ? 'passed' : 'mismatch',
              expected: JSON.stringify(object(r.expected) ? r.expected.disposition : r.expected),
              actual: JSON.stringify(object(r.actual) ? r.actual.disposition : r.actual),
              detail: r.refused,
            })),
          },
        ],
      },
    })
  }
  next.research = { sources: draft.checkpoint.sources, unknowns: state.unknowns }
  next.recovered.push(marker)
  return next
}
export function pointerGet(root: unknown, path: string): unknown {
  if (path === '') return root
  if (!path.startsWith('/')) return undefined
  return path
    .slice(1)
    .split('/')
    .map((s) => s.replace(/~1/g, '/').replace(/~0/g, '~'))
    .reduce<unknown>(
      (v, k) => ((object(v) || Array.isArray(v)) && Object.hasOwn(v, k) ? (v as ObjectValue)[k] : undefined),
      root,
    )
}
export function pointerSet(root: unknown, path: string, value: unknown): unknown {
  if (!path) return value === undefined ? {} : value
  if (!path.startsWith('/')) throw Error('Use an absolute JSON pointer.')
  const keys = path
    .slice(1)
    .split('/')
    .map((s) => s.replace(/~1/g, '/').replace(/~0/g, '~'))
  if (keys.some((k) => ['__proto__', 'constructor', 'prototype'].includes(k)))
    throw Error('This field name must be edited in JSON.')
  const result = object(root) || Array.isArray(root) ? structuredClone(root) : {}
  let at = result as ObjectValue
  for (let i = 0; i < keys.length - 1; i++) {
    const key = keys[i]!
    if (!object(at[key]) && !Array.isArray(at[key])) at[key] = {}
    at = at[key] as ObjectValue
  }
  if (Array.isArray(at) && value === undefined)
    throw Error('Edit array elements in JSON; removing an index would change the remaining input positions.')
  if (value === undefined) delete at[keys.at(-1)!]
  else at[keys.at(-1)!] = value
  return result
}
export function factFields(doc: unknown): { path: string; label: string; type: string }[] {
  const fields = new Map<string, Set<string>>()
  function walk(v: unknown) {
    if (Array.isArray(v)) v.forEach(walk)
    else if (object(v)) {
      if (v.op === 'fact' && typeof v.path === 'string') {
        const types = fields.get(v.path) ?? new Set<string>()
        // `in` supplies candidate values, not the type of the fact itself.
        const values = v.operator === 'in' && Array.isArray(v.value) ? v.value : [v.value]
        for (const value of values)
          if (value !== undefined && value !== null) types.add(Array.isArray(value) ? 'array' : typeof value)
        fields.set(v.path, types)
      }
      Object.values(v).forEach(walk)
    }
  }
  walk(doc)
  return [...fields].map(([path, types]) => ({
    path,
    label: path
      .split('/')
      .filter(Boolean)
      .map((x) =>
        x
          .replace(/~1/g, '/')
          .replace(/~0/g, '~')
          .replace(/([a-z])([A-Z])/g, '$1 $2')
          .replace(/[-_]/g, ' '),
      )
      .join(' · '),
    type: types.size === 1 ? [...types][0]! : 'json',
  }))
}

/** Optional history written with a Research handover; older records may not have it. */
export async function recoverResearchRecord(
  suite: TestSuite,
  record: unknown,
  key: string,
): Promise<TestSuite> {
  if (
    !object(record) ||
    record.researchRecordVersion !== '1' ||
    !Array.isArray(record.testHistory) ||
    typeof record.recordedAt !== 'string' ||
    !Number.isFinite(Date.parse(record.recordedAt))
  )
    return suite
  let next = suite
  for (const [index, entry] of record.testHistory.entries()) {
    if (
      !object(entry) ||
      typeof entry.text !== 'string' ||
      !object(entry.check) ||
      !Array.isArray(entry.check.cases) ||
      entry.check.cases.some((c) => !object(c) || typeof c.id !== 'string' || typeof c.passed !== 'boolean')
    )
      continue
    const digest = await digestOf(entry.text)
    if (entry.check.documentDigest !== digest) continue
    // Only display the old report. This is not a fresh runtime check or current case result.
    const draft = {
      id: key + ':' + index,
      generation: 1,
      updatedAt: typeof record.recordedAt === 'string' ? record.recordedAt : new Date(0).toISOString(),
      documents: [],
      checkpoint: {
        sources: [],
        state: {
          cases: [],
          probes: [],
          unknowns: [],
          candidates: [
            { text: entry.text, digest, document: JSON.parse(entry.text), previousCheck: entry.check },
          ],
        },
      },
    } as unknown as PackDraft
    next = await recoverDraft(next, draft)
  }
  return next
}
