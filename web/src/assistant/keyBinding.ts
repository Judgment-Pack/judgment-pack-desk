/**
 * What the key row is about — read off the desk's verdict, never computed here.
 *
 * **The binding is the chassis' and this is a *reading* of it.** The chassis
 * records the scheme, host and wire protocol an entered key was for, presents
 * it only there, and refuses with `assistant-key-unbound` otherwise;
 * `GET /api/assistant/key` reports that binding, the origin currently
 * configured, and its own `bound` verdict, so a form can *say* which host the
 * key is for instead of leaving somebody to discover it by meeting a refusal.
 *
 * **There is no comparison in this module, and there must never be one again.**
 * There was: `new URL(url).host`, which drops an explicit `:443` where Go's
 * `url.Parse` keeps it — so a key stored for a host and a configuration naming
 * the same host with its default port written out showed here as bound while
 * the relay sent nothing. Two implementations of one rule is one too many, and
 * the one that decides has to be the one that presents the credential. What is
 * left is a mapping from the desk's answer to the five things a row can say.
 */
import type { AssistantKeyState } from './client'

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
 * Read the row's state off the key answer, and off **nothing else**.
 *
 * It used to consult the page's copy of the configuration first, to tell "no
 * endpoint" from "one is configured" — and that copy is exactly the thing that
 * can be missing. A desk-level read that answered with a refusal resolves to
 * the built-in defaults, so a page whose configuration could not be read said
 * "save an endpoint first" while a perfectly good key read beside it named the
 * endpoint *and* carried this desk's verdict about it. The row was overruling
 * the chassis with an absence it had not established.
 *
 * So every state comes from the answer: an empty `configuredOrigin` is the
 * desk saying it is configured for nowhere, `bound` is its verdict about the
 * pair, and `present` is whether there is a key at all. The saved endpoint is
 * still what all of that is about — the desk computes it from the file, not
 * from anything the form is holding — which is why a host typed and not saved
 * never moves this row.
 */
export function keyBinding(key: AssistantKeyState | undefined): KeyBinding {
  if (key === undefined) return 'unread'
  // **Nowhere to bind to is `no-endpoint` whether or not a key is kept here**,
  // and not "rebind". Storing a key requires an endpoint to bind it to, so a
  // row that offered the field here would offer a repair the chassis refuses;
  // and a key that is stored is still reported on the line above, because
  // removing an endpoint never removed a key.
  if (key.configuredOrigin === '') return 'no-endpoint'
  if (!key.present) return 'none'
  return key.bound ? 'bound' : 'rebind'
}
