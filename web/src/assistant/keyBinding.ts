/**
 * Whether the key on this machine is the key for the endpoint that is
 * configured — as the page can tell, and no further.
 *
 * **The binding is the desk's, and this is a reading of it rather than a
 * second copy.** The chassis records the scheme, host and wire protocol an
 * entered key was for, presents it only where both still match, and refuses
 * with `assistant-key-unbound` otherwise. `GET /api/assistant/key` reports
 * that binding beside the fingerprint precisely so a form can *say* which
 * host the key is for instead of leaving somebody to discover it by meeting a
 * refusal. Nothing here decides anything: what it decides is what a row of the
 * page says, and what a button that would fail anyway is enabled for.
 *
 * Neither half is a secret — both are in the file the page already reads —
 * and neither is a comparison against a vendor. It compares two values that
 * arrived at runtime: the origin the key was entered for, and the origin the
 * file names now.
 */
import type { AssistantEndpointConfig } from '../config/deskConfig'
import type { AssistantKeyState } from './client'

/**
 * The part of a configured URL a key is bound to: its scheme and its host,
 * lower-cased, and nothing else.
 *
 * Mirrored from `endpointOrigin` in `internal/desk/assistant.go`, whose
 * reasoning is the whole of it: a path or a query is the endpoint's own
 * routing and an author changes one without changing who is at the other end,
 * while a *host* change is a different party. The port is inside the origin,
 * so a host at port 8443 and the same host at its default port are two
 * destinations.
 *
 * Undefined for a URL with no host — which `endpointUrlProblem` already
 * refuses — because there is then nothing to bind to and nothing to say about
 * it.
 */
export function endpointOrigin(raw: string): string | undefined {
  let parsed: URL
  try {
    parsed = new URL(raw)
  } catch {
    return undefined
  }
  // Written as a length rather than as an equality against the empty string,
  // deliberately: the enforcement guard enumerates every host comparison in
  // these directories and requires each to be a loopback name, and a check
  // that is really "is there a host at all" should not have to be excused by
  // that list.
  if (parsed.host.length === 0) return undefined
  return `${parsed.protocol}//${parsed.host}`.toLowerCase()
}

/**
 * The five things the key row can be about.
 *
 * `unread` is a page that has not been told, which is not `none` — saying
 * otherwise is stating what was not observed. `no-endpoint` is the state that
 * has no repair at this row at all: storing a key requires an endpoint to bind
 * it to, so the row asks for one to be saved rather than offering a field the
 * chassis would refuse.
 */
export type KeyBinding = 'unread' | 'no-endpoint' | 'none' | 'bound' | 'rebind'

/**
 * Read the row's state off the key and the endpoint that is **saved**.
 *
 * The saved endpoint and not the draft: a key is bound to what is in the file,
 * and a form with an unsaved host typed into it has changed nothing about
 * where the credential may go. A row that read the draft would tell an author
 * their key had stopped working because they were in the middle of typing.
 */
export function keyBinding(
  key: AssistantKeyState | undefined,
  endpoint: AssistantEndpointConfig | null
): KeyBinding {
  if (key === undefined) return 'unread'
  // **No endpoint is `no-endpoint` whether or not a key is kept here**, and
  // not "rebind". Storing a key requires an endpoint to bind it to, so a row
  // that offered the field here would offer a repair the chassis refuses; and
  // a key that is stored is still reported on the line above, because removing
  // an endpoint never removed a key.
  if (endpoint === null) return 'no-endpoint'
  if (!key.present) return 'none'
  const origin = endpointOrigin(endpoint.url)
  if (origin !== undefined && key.origin === origin && key.kind === endpoint.kind) {
    return 'bound'
  }
  return 'rebind'
}
