/**
 * Which cached answers a change to the project, or a reconnect, makes stale.
 *
 * `McpProvider` cancels and invalidates the page's queries when the chassis
 * reports a file changed, and again after a reconnect: the runtime reads the
 * project on every call, so any answer may be out of date. A query that runs
 * only when the page asks for it is left alone, in flight or not. The
 * decision-record panel is one: it runs the runtime's `audit verify` when the
 * owner opens it or asks again, and an edit elsewhere in the project is
 * neither.
 */
import type { Query } from '@tanstack/react-query'

/** The `meta` of a query that runs only when the page asks for it. */
export const ON_REQUEST_ONLY: Readonly<Record<string, unknown>> = { onRequestOnly: true }

/** Whether a change to the project, or a reconnect, may cancel or rerun the query. */
export function followsTheProject(query: Query): boolean {
  return query.meta?.onRequestOnly !== true
}
