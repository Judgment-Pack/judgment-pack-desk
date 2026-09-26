import type { AssistantEvent } from '../assistant/engine'
import { digestOf, type CandidateCheck } from './checkCandidate'

/** Observed rehearsals are history, never independently established expectations. */
export interface RuntimeProbe {
  documentDigest: string
  at: string
  facts: unknown
  evidence?: unknown
  disposition: unknown
  handoffTarget?: unknown
}
const object = (value: unknown): value is Record<string, unknown> => value !== null && typeof value === 'object' && !Array.isArray(value)
const bounded = (value: unknown) => JSON.stringify(value)?.length <= 65536
const parse = (value: unknown): unknown => typeof value === 'string' ? JSON.parse(value) : value
export async function runtimeProbes(events: readonly AssistantEvent[], at: string): Promise<RuntimeProbe[]> {
  const calls = new Map<string, Record<string, unknown>>()
  const rows: RuntimeProbe[] = []
  for (const event of events) {
    if (event.type === 'tool_call' && event.name === 'experimental_evaluate' && object(event.args)) calls.set(event.callId ?? event.name, event.args)
    if (event.type !== 'tool_result' || event.name !== 'experimental_evaluate') continue
    const key = event.callId ?? event.name, args = calls.get(key)
    calls.delete(key)
    if (!args || event.isError) continue
    try {
      const result = event.structured ?? JSON.parse(event.text)
      // A prose report or a refused call cannot stand in for a runtime rehearsal.
      if (!object(result) || result.status !== 'evaluated' || result.rehearsal !== true || !object(result.disposition)) continue
      const document = parse(args.pack), facts = parse(args.facts), evidence = parse(args.evidence)
      if (!object(document) || !object(facts) || !bounded(facts) || !bounded(result.disposition) || (evidence !== undefined && !bounded(evidence)) || (result.handoffTarget !== undefined && !bounded(result.handoffTarget))) continue
      const documentDigest = await digestOf(JSON.stringify(document, null, 2))
      rows.push({ documentDigest, at, facts, disposition: result.disposition, ...(evidence === undefined ? {} : { evidence }), ...(result.handoffTarget === undefined ? {} : { handoffTarget: result.handoffTarget }) })
    } catch { /* An incomplete or non-JSON call is not an observed rehearsal. */ }
  }
  return rows.slice(-64)
}
export function readProbes(value: unknown): RuntimeProbe[] {
  if (value === undefined) return []
  if (!Array.isArray(value) || value.length > 64) throw new Error('Invalid saved runtime rehearsals')
  return value.map(row => {
    if (!object(row) || !/^[a-f0-9]{64}$/.test(String(row.documentDigest)) || typeof row.at !== 'string' || !Number.isFinite(Date.parse(row.at)) || !object(row.facts) || !object(row.disposition) || !bounded(row.facts) || !bounded(row.disposition) || (row.evidence !== undefined && !bounded(row.evidence)) || (row.handoffTarget !== undefined && !bounded(row.handoffTarget))) throw new Error('Invalid saved runtime rehearsals')
    return { documentDigest: String(row.documentDigest), at: row.at, facts: row.facts, disposition: row.disposition, ...(row.evidence === undefined ? {} : { evidence: row.evidence }), ...(row.handoffTarget === undefined ? {} : { handoffTarget: row.handoffTarget }) }
  })
}
/** Display-only history: decode never puts this in candidate.check. */
export function readPreviousCheck(value: unknown): CandidateCheck | undefined {
  if (value === undefined) return undefined
  if (!object(value) || !/^[a-f0-9]{64}$/.test(String(value.documentDigest)) || typeof value.valid !== 'boolean' || !Array.isArray(value.diagnostics) || !Array.isArray(value.cases) || value.cases.some(row => !object(row) || typeof row.id !== 'string' || typeof row.passed !== 'boolean' || (row.refused !== undefined && typeof row.refused !== 'string'))) throw new Error('Invalid saved draft check')
  return { documentDigest: String(value.documentDigest), valid: value.valid, diagnostics: value.diagnostics, cases: value.cases }
}
