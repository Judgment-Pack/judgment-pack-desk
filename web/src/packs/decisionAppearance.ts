import type { PackDocument } from '../mcp/types'
import { isRecord } from './document/MisshapenMember'
import { sourceMessage } from '../i18n/source'

export const DECISION_COLORS = ['blue', 'violet', 'cyan', 'rose', 'amber', 'green', 'indigo', 'slate'] as const
export type DecisionColor = typeof DECISION_COLORS[number]
export const DECISION_MEANINGS = ['categorical', 'proceed', 'review', 'hold', 'handoff', 'neutral'] as const
export type DecisionMeaning = typeof DECISION_MEANINGS[number]
export interface DecisionAppearance { color: DecisionColor; meaning: DecisionMeaning }
export type OutcomeAppearances = Readonly<Record<string, DecisionAppearance>>
export interface PresentationDocument { version: 1; packs: Record<string, { outcomes: Record<string, DecisionAppearance> }> }
export const PRESENTATION_FILE = 'jpack-presentation.json'
export const PRESENTATION_LIMIT = 500_000
export function emptyPresentation(): PresentationDocument { return { version: 1, packs: {} } }
const keys = (value: Record<string, unknown>, names: string[]) => Object.keys(value).every(key => names.includes(key))
export function decodePresentation(value: unknown): PresentationDocument {
  const invalid = () => new Error(sourceMessage('Decision appearance could not be read. The saved file has not been changed.'))
  if (!isRecord(value) || value.version !== 1 || !keys(value, ['version', 'packs']) || !isRecord(value.packs)) throw invalid()
  for (const [id, pack] of Object.entries(value.packs)) {
    if (!id || !isRecord(pack) || !keys(pack, ['outcomes']) || !isRecord(pack.outcomes)) throw invalid()
    for (const [id, appearance] of Object.entries(pack.outcomes)) {
      if (!id || !isRecord(appearance) || !keys(appearance, ['color', 'meaning']) || !DECISION_COLORS.includes(appearance.color as DecisionColor) || !DECISION_MEANINGS.includes(appearance.meaning as DecisionMeaning)) throw invalid()
    }
  }
  return value as unknown as PresentationDocument
}
function hash(value: string): number {
  let n = 2166136261
  for (const char of value) n = Math.imul(n ^ char.codePointAt(0)!, 16777619)
  return n >>> 0
}
/** IDs, never names or conditions, choose categorical colors. Sorted allocation
 * avoids repeats within the palette and survives renaming/reordering. An explicit
 * save freezes all current assignments so future additions keep existing colors. */
export function outcomeAppearances(document: PackDocument, saved: OutcomeAppearances = {}): OutcomeAppearances {
  const ids = [...new Set((Array.isArray(document.outcomes) ? document.outcomes : []).filter(item => typeof item?.id === 'string').map(item => item.id))].sort()
  const result: Record<string, DecisionAppearance> = Object.fromEntries(ids.filter(id => Object.hasOwn(saved, id)).map(id => [id, saved[id]!]))
  const used = new Set(Object.values(saved).map(item => item.color))
  for (const id of ids) {
    if (Object.hasOwn(result, id)) continue
    const start = hash(JSON.stringify([document.id, id])) % DECISION_COLORS.length
    const color = Array.from({ length: DECISION_COLORS.length }, (_, offset) => DECISION_COLORS[(start + offset) % DECISION_COLORS.length]!).find(color => !used.has(color)) ?? DECISION_COLORS[start]!
    Object.defineProperty(result, id, { value: { color, meaning: 'categorical' }, enumerable: true, configurable: true, writable: true })
    used.add(color)
  }
  return result
}
export const meaningColor: Record<Exclude<DecisionMeaning, 'categorical'>, DecisionColor> = { proceed: 'green', review: 'amber', hold: 'rose', handoff: 'violet', neutral: 'slate' }
export function decisionColor(appearance: DecisionAppearance): DecisionColor { return appearance.meaning === 'categorical' ? appearance.color : meaningColor[appearance.meaning] }
export function decisionAccent(appearance: DecisionAppearance): string { return `var(--decision-${decisionColor(appearance)})` }
export function lookupAppearance(appearances: OutcomeAppearances, id: unknown): DecisionAppearance | undefined { return typeof id === 'string' && Object.hasOwn(appearances, id) ? appearances[id] : undefined }
