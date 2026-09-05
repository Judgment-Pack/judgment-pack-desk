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

const LOADERS: Record<CertifiedEngine, () => Promise<Engine>> = {
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

/** Load one engine's chunk. */
export async function loadEngine(id: CertifiedEngine): Promise<Engine> {
  return LOADERS[id]()
}
