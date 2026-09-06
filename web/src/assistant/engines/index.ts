/**
 * The engine registry: one lazily loaded chunk per certified engine.
 *
 * ADR-0001 makes the engine a slot and certification the price of shipping one:
 * an engine is here only once it has passed the desk's conformance session,
 * which is `assistant/conformance/` and runs in CI against **this list**. So
 * adding an adapter is one line here plus its module — and the suite certifies
 * it or the suite goes red.
 *
 * **Every engine a `desk.json` may name is certified in this build.** `LOADERS`
 * is declared as a total map over `AssistantEngine`, so an id added to the
 * decoder's closed list without an adapter is a compile error rather than a
 * setting that appears to grant something and quietly runs something else. The
 * substitution this used to do — a desk configured for `vercel` running
 * `builtin` and a line in the tab saying so — is gone with the reason for it.
 *
 * The loaders are `import()` so a session downloads one chunk. The release
 * grows by the sum of certified engines; a session does not.
 */
import type { AssistantEngine } from '../../config/deskConfig'
import type { Engine } from '../engine'

/**
 * The engines this build carries, in the order the conformance suite runs them.
 *
 * The one list the suite reads.
 */
export const CERTIFIED_ENGINES = ['builtin', 'vercel'] as const satisfies readonly AssistantEngine[]
export type CertifiedEngine = (typeof CERTIFIED_ENGINES)[number]

/** One engine's chunk, by id. */
export type EngineLoaders = Record<string, () => Promise<Engine>>

/**
 * **Total over `AssistantEngine`, deliberately.** A declared engine with no
 * chunk here does not compile.
 */
const LOADERS: Record<AssistantEngine, () => Promise<Engine>> = {
  builtin: async () => (await import('./builtin')).builtin,
  vercel: async () => (await import('./vercel')).vercel
}

/**
 * Load one engine's chunk.
 *
 * `loaders` exists for the **conformance session's certification fixtures**,
 * and for nothing else. Those fixtures are engines the desk would never ship —
 * one reaches for the network as it loads, one after it has finished — and the
 * point of them is to be loaded down the path a certified engine takes, under
 * the same seal, rather than by a test import that proves a different route. It
 * defaults to this build's own table, so the page has one registry and nothing
 * a `desk.json` can name reaches anything else.
 */
export async function loadEngine(
  id: string,
  loaders: EngineLoaders = LOADERS
): Promise<Engine> {
  const load = loaders[id]
  if (load === undefined) throw new Error(`no engine chunk is registered for ${id}`)
  return load()
}
