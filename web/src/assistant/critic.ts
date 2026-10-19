import type { AssistantEvent } from '@judgment-pack/harness/engine'
import type { CriticFigures } from '@judgment-pack/harness/refutation'

/**
 * What the critic's pass said about the cases the Desk declared to it, and what
 * it cost (`@judgment-pack/harness` v0.2.0): the critique event's `cases`,
 * `incomplete` and `figures`, kept as a record with the response so a reload
 * shows the same account. **A critique with any uncompared case is incomplete,
 * and an incomplete critique is never read as "not refuted".**
 */
export interface CriticRecord {
  /** Declared cases the critic (or the harness, for it) compared with the runtime's outcome. */
  compared: number
  /** Of those, the ones whose outcome differs from the expected one. */
  disagreed: number
  /** The ids of the declared cases nothing compared. */
  incomplete: string[]
  figures?: CriticFigures
  /** The model the run itself used, so a critic on another model is said to be. */
  runModel?: string
}

type Critique = Extract<AssistantEvent, { type: 'critique' }>

/** The record a critique event gives, or nothing where it carries no declared case and no figures. */
export function criticRecord(event: Critique, runModel?: string): CriticRecord | undefined {
  const cases = event.cases ?? []
  const incomplete = event.incomplete ?? []
  if (cases.length === 0 && incomplete.length === 0 && event.figures === undefined) return undefined
  return {
    compared: cases.length,
    disagreed: cases.filter((item) => !item.agrees).length,
    incomplete: incomplete.slice(0, 64).map((id) => String(id).slice(0, 200)),
    ...(event.figures === undefined ? {} : { figures: event.figures }),
    ...(runModel === undefined ? {} : { runModel })
  }
}

const object = (value: unknown): value is Record<string, unknown> => value !== null && typeof value === 'object' && !Array.isArray(value)
const count = (value: unknown, max = Number.MAX_SAFE_INTEGER): value is number => Number.isSafeInteger(value) && Number(value) >= 0 && Number(value) <= max
const FIGURE_MEMBERS = ['model', 'unit', 'requests', 'harnessCalls', 'inputTokens', 'outputTokens', 'reasoningTokens', 'cacheReadTokens', 'cacheWriteTokens']

/** The critic's figures, read closed. */
export function validCriticFigures(value: unknown): value is CriticFigures {
  return object(value) && Object.keys(value).every((key) => FIGURE_MEMBERS.includes(key)) &&
    typeof value.model === 'string' && value.model.length > 0 && value.model.length <= 200 &&
    (value.unit === 'model-requests' || value.unit === 'agent-runs') && count(value.requests, 1000) &&
    ['harnessCalls', 'inputTokens', 'outputTokens', 'reasoningTokens', 'cacheReadTokens', 'cacheWriteTokens'].every((key) => value[key] === undefined || count(value[key]))
}

/** A saved critic record, read closed. */
export function validCriticRecord(value: unknown): value is CriticRecord {
  return object(value) && Object.keys(value).every((key) => ['compared', 'disagreed', 'incomplete', 'figures', 'runModel'].includes(key)) &&
    count(value.compared, 200) && count(value.disagreed, 200) && Number(value.disagreed) <= Number(value.compared) &&
    Array.isArray(value.incomplete) && value.incomplete.length <= 64 && value.incomplete.every((id) => typeof id === 'string' && id.length <= 200) &&
    (value.figures === undefined || validCriticFigures(value.figures)) &&
    (value.runModel === undefined || (typeof value.runModel === 'string' && value.runModel.length <= 200))
}
