/**
 * A project's graphs, the findings and the plan (ADR-0011, section 3, row 1).
 *
 * `GET /api/graphs/findings` is the runtime's `experimental graph validate`
 * and `GET /api/graphs/plan?id=` its `experimental graph explain`, run by
 * Desk's Go. Each answers `{ answer }` where `answer` is what the runtime
 * printed. A graph is named by its configured id, never by a path.
 *
 * Nothing here judges a graph: `status` is the runtime's, and the page shows
 * it as the runtime's.
 */
import { deskFetch } from '../files/client'
import { msg } from '../i18n'

export type GraphDiagnostic = { code?: string; severity?: string; instancePath?: string; message?: string }

/** The members every graph payload may carry, shown as given. */
export type GraphLabels = {
  kind?: string
  experimental?: boolean
  label?: string
  rehearsal?: boolean
  conformanceClaimReference?: string
}

export type GraphFinding = {
  id: string
  path?: string
  rowsPath?: string
  graphId?: string
  graphVersion?: string
  graphSha256?: string
  status: string
  diagnostics?: GraphDiagnostic[]
}

export type GraphFindings = GraphLabels & {
  outputVersion?: string
  command?: string
  status: string
  formatVersion?: string
  configPath?: string
  configVersion?: string
  summary?: { total: number; passed: number; failed: number }
  graphs?: GraphFinding[]
  diagnostics?: GraphDiagnostic[]
}

export type GraphFeed = { from?: string; fact?: string; evidence?: string; onUnresolved?: string }
export type GraphStep = {
  order?: number
  node?: string
  pack?: string
  path?: string
  packId?: string
  packVersion?: string
  detail?: string
  feeds?: GraphFeed[]
}

export type GraphPlan = GraphLabels & {
  outputVersion?: string
  command?: string
  status: string
  formatVersion?: string
  configPath?: string
  graphPath?: string
  graphId?: string
  graphVersion?: string
  resultNode?: string
  steps?: GraphStep[]
  diagnostics?: GraphDiagnostic[]
}

const object = (value: unknown): value is Record<string, unknown> => !!value && typeof value === 'object' && !Array.isArray(value)

async function answerOf<T>(response: Response, command: string): Promise<T> {
  let body: unknown
  try { body = await response.json() } catch { /* The status is still an answer. */ }
  if (!response.ok) {
    const error = object(body) && typeof body.error === 'string' ? body.error : msg('The runtime could not be asked about this graph. Please try again.')
    throw new Error(error)
  }
  const answer = object(body) ? body.answer : undefined
  if (!object(answer) || typeof answer.status !== 'string' || answer.command !== command) {
    throw new Error(msg('The runtime could not be asked about this graph. Please try again.'))
  }
  return answer as T
}

export async function readGraphFindings(signal?: AbortSignal): Promise<GraphFindings> {
  return answerOf<GraphFindings>(await deskFetch('/api/graphs/findings', { signal }), 'experimental graph validate')
}

export async function readGraphPlan(id: string, signal?: AbortSignal): Promise<GraphPlan> {
  return answerOf<GraphPlan>(await deskFetch(`/api/graphs/plan?id=${encodeURIComponent(id)}`, { signal }), 'experimental graph explain')
}

const ROOT = /^(?:[\\/]|[A-Za-z]:[\\/])/
const IN_MESSAGE = /(^|[\s"(=[])((?:\/|[A-Za-z]:[\\/])[^\s"'()[\]]*)/g

/** A character the runtime prints as "?" in a sentence (`displayedPath` in Desk's Go). */
const displayed = (path: string) => path.replace(/[\p{Cc}\p{Cf}\p{Zl}\p{Zp}]/gu, '?')

/**
 * A path member as the page shows it (ADR-0011, section 2): a relative path as
 * given, and a path from a root, or a relative one holding such a path, as "…".
 * The page never learns where the owner keeps their files.
 */
export function shownPath(path: string): string {
  return ROOT.test(path) || shownMessage(path) !== path ? '…' : path
}

/**
 * A runtime sentence with no path from a root in it. `known` is the paths the
 * same payload gave in its path members: replaced whole, however many spaces
 * they hold, before the pattern takes a path up to its first space.
 */
export function shownMessage(message: string, known: (string | undefined)[] = []): string {
  let out = message
  for (const path of known.flatMap(path => path && ROOT.test(path) ? [path, displayed(path)] : []).sort((a, b) => b.length - a.length)) {
    out = out.split(path).join('…')
  }
  return out.replace(IN_MESSAGE, (_, before: string) => before + '…')
}
