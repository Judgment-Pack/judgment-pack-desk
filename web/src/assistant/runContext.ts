import type { CallTool } from '@judgment-pack/harness/engine'
import type { RuntimeIdentity } from '@judgment-pack/harness/context/chat'
import { memoryRequest } from '@judgment-pack/harness/memory/client'
import { digestOf } from '../research/checkCandidate'
import { reusableRuntime, runtimeCheckIdentity } from '../research/retainedCheck'

/**
 * What the Desk tells the harness about a run beyond the run itself
 * (`ContextInput`, `@judgment-pack/harness` v0.2.0): the pinned Runtime's
 * identity and version, which key a Runtime-served read in the chat's cache,
 * and whether project memory is on, which decides whether the memory reserve
 * is left out of the input allowance.
 */

const identities = new WeakMap<object, Promise<RuntimeIdentity | undefined>>()

/**
 * The Runtime a connection reaches, by the digest of what `describe_runtime`
 * answers and its version, read once for the connection. **Nothing for a
 * build with no release version** (a development build, an unknown or 0.0.0
 * version, the rule that already holds a saved check to its runtime): a read
 * keyed by an identity that does not change when the build does would be
 * served stale, so the harness caches such a read for the run alone.
 */
export function runtimeIdentityOf(connection: object, call: CallTool): Promise<RuntimeIdentity | undefined> {
  let known = identities.get(connection)
  if (known === undefined) {
    known = (async () => {
      try {
        const identity = await runtimeCheckIdentity(call)
        if (!reusableRuntime(identity)) return undefined
        const version = (JSON.parse(identity) as { tool: { version: string } }).tool.version
        return { identity: await digestOf(identity), version }
      } catch {
        return undefined
      }
    })()
    identities.set(connection, known)
    void known.then((value) => { if (value === undefined) identities.delete(connection) })
  }
  return known
}

/**
 * The project's memory `use` setting, read as the run starts: `true` or
 * `false`, or nothing where it cannot be read (the harness then keeps the
 * reserve, so a server that adds memory never receives a request the harness
 * left no room for).
 */
export async function memoryInUse(signal?: AbortSignal): Promise<boolean | undefined> {
  try {
    const use = (await memoryRequest(undefined, undefined, undefined, signal)).content.settings.use
    return typeof use === 'boolean' ? use : undefined
  } catch {
    return undefined
  }
}
