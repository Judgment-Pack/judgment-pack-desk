/** Canonical Desk copy for state, logs, and errors. Never depends on the UI locale.
 * Marking the source also lets catalogue extraction find deferred messages.
 * Translate with systemMessage only when displaying Desk-authored copy.
 */
export function sourceMessage(source: string, values: Record<string, unknown> = {}): string {
  return source.replace(/\{\{(\w+)\}\}/g, (token, name: string) =>
    Object.hasOwn(values, name) ? String(values[name]) : token)
}
