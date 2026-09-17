/** Canonical Desk copy for state, logs, and errors. Never depends on the UI locale.
 * Marking the source also lets catalogue extraction find deferred messages.
 * Translate with systemMessage only when displaying Desk-authored copy.
 */
import plurals from './plurals.json'

export function sourceMessage(source: string, values: Record<string, unknown> = {}): string {
  const template = values.count === 1 && Object.hasOwn(plurals, source)
    ? plurals[source as keyof typeof plurals] : source
  return template.replace(/\{\{(\w+)\}\}/g, (token, name: string) =>
    Object.hasOwn(values, name) ? String(values[name]) : token)
}
