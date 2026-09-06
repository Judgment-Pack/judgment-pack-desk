/**
 * The engine registry: one lazily loaded chunk per certified engine.
 *
 * ADR-0001 makes the engine a slot and certification the price of shipping one:
 * an engine is here only once it has passed the desk's conformance session,
 * which is `assistant/conformance/` and runs in CI against **this list**. So
 * adding an adapter is one line here plus its module — and the suite certifies
 * it or the suite goes red.
 *
 * **`vercel` is not here yet.** It is the ADR's default and this build carries
 * no adapter for it, so a desk configured for it runs `builtin` and the tab
 * says so in one line. The alternative — refusing to run — would make a
 * default nobody typed into a desk that does nothing.
 *
 * The loaders are `import()` so a session downloads one chunk. The release
 * grows by the sum of certified engines; a session does not.
 */
import type { AssistantEngine } from '../../config/deskConfig'
import type { Engine } from '../engine'

/**
 * The engines this build carries, in the order the conformance suite runs them.
 *
 * The one list the suite reads. A `vercel` entry added without an adapter
 * fails to load rather than falling through to something else.
 */
export const CERTIFIED_ENGINES = ['builtin'] as const
export type CertifiedEngine = (typeof CERTIFIED_ENGINES)[number]

/** One engine's chunk, by id. */
export type EngineLoaders = Record<string, () => Promise<Engine>>

const LOADERS: EngineLoaders = {
  builtin: async () => (await import('./builtin')).builtin
}

/** Whether this build carries an adapter for an id a `desk.json` may name. */
export function isCertified(id: AssistantEngine): id is CertifiedEngine {
  return (CERTIFIED_ENGINES as readonly string[]).includes(id)
}

/**
 * The id a session will actually run, and what to say where it is not the
 * configured one.
 *
 * `substituted` is a sentence for the pane rather than a boolean, because the
 * only useful thing to render is which engine ran and why it was not the one
 * the file named.
 */
export function resolveEngine(configured: AssistantEngine): {
  id: CertifiedEngine
  substituted?: string
} {
  if (isCertified(configured)) return { id: configured }
  return {
    id: 'builtin',
    substituted: `${configured} is not certified in this build; running builtin`
  }
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
