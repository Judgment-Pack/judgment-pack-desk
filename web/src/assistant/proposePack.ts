import type { HostTool } from '@judgment-pack/harness/engine'
import { PROPOSE_PACK_SCHEMA, PROPOSE_PACK_TOOL } from '@judgment-pack/harness/engines/contract'

/**
 * The pack proposal as a host tool (agent#6, `@judgment-pack/harness` v0.1.0):
 * offered by the surfaces where a run proposes a pack, the pack pane and the
 * chat, under the name and with the input schema the package exports (the
 * contract's `tool-offer.json#/$defs/proposePack`).
 *
 * The engine reads the call itself, with the contract's reader, and delivers
 * the proposal as the `proposal` event a fenced one becomes; the Desk reviews it
 * as it reviews every proposal, and nothing is written. So `execute` is never
 * reached, and refuses if it is. The fenced block stays the fallback, and the
 * critic is never offered the tool.
 *
 * Its effect is `proposal`: Plan mode offers no proposal tool, a delegated task
 * is never offered it (no `delegation`), and its result is not framed as
 * retrieved data.
 */
export function proposePackTool(): HostTool {
  return {
    name: PROPOSE_PACK_TOOL,
    effect: 'proposal',
    description:
      'Deliver the pack proposal for the person to review: the proposed pack as a JSON object, and what the policy leaves open. Nothing is written or saved; the person reviews the proposal.',
    inputSchema: PROPOSE_PACK_SCHEMA,
    execute: async () => {
      throw new Error(`${PROPOSE_PACK_TOOL} is read by the engine; the Desk does not run it`)
    }
  }
}

/** The host tools with `propose_pack` added, once, where a run may propose a pack. */
export function withProposePack(hostTools: readonly HostTool[]): HostTool[] {
  return hostTools.some((tool) => tool.name === PROPOSE_PACK_TOOL) ? [...hostTools] : [...hostTools, proposePackTool()]
}
