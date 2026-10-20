import type { EndpointCapabilities, EndpointKind } from '@judgment-pack/harness/contract'
import type { AssistantEndpointConfig, CapabilityFacts, EndpointCapabilityRecord } from '../config/deskConfig'
import { connectionsServing } from './connectionRecords'

/**
 * What the Desk tells the harness about one endpoint and model, beyond its
 * family: members of the capability record of `@judgment-pack/harness`
 * (`capabilities.ts`, the contract's `capability.json`), among them the
 * context window and output reserve, whether the endpoint takes tool calls
 * natively and in parallel, whether it is shown images, the stream member its
 * reasoning arrives in, where the execution state goes (`statePlacement`) and
 * whether it keeps a strict turn order (`strictAlternation`).
 *
 * **From the connection, never from the model's name.** A connection holds what
 * an endpoint probe found (`probe`, with the time) and what its owner says
 * instead (`override`), member by member; the answer is the finding with each
 * override laid over it. A member nothing states is left out, and the harness
 * completes the record from its family's default (`FAMILY_CAPABILITIES`): native
 * and parallel tool calls, the family's reasoning dialect, the Gemini schema
 * subset on Gemini alone, images shown to Anthropic and Gemini models and
 * rendered as text for an OpenAI-compatible one. The assistant profile's
 * per-model window and output cap (`context.models`) stay the last word,
 * through the harness's `contextPolicy`, as before.
 *
 * An endpoint whose chat template refuses two user turns in a row, or a user
 * text after a tool result, is one whose owner overrides `strictAlternation`
 * with `true`; until then such an endpoint refuses the second request of a run,
 * and the run reports it as the endpoint's refusal.
 */

/** The record a connection states: the probe's finding, then the owner's overrides over it, member by member. */
export function effectiveFacts(record: EndpointCapabilityRecord | undefined): CapabilityFacts {
  if (record === undefined) return {}
  const { at: _at, ...found } = record.probe ?? { at: '' }
  return { ...found, ...(record.override ?? {}) }
}

/** The smallest output reserve a window may carry (the harness's floor), and the most one is taken to be where only the window is stated. */
const OUTPUT_FLOOR = 1024
const OUTPUT_DEFAULT_CEILING = 8192

/**
 * The harness's capability members for one connection's endpoint: the facts it
 * states, in the harness's own shape. The window and the output reserve go
 * together (the harness refuses one without the other), so a window stated alone
 * is given a reserve of an eighth of it, between the floor and 8,192, and an
 * output reserve stated alone is left out.
 */
export function capabilitiesOf(endpoint: Pick<AssistantEndpointConfig, 'capabilities'> | null | undefined): EndpointCapabilities {
  const facts = effectiveFacts(endpoint?.capabilities)
  const record: Record<string, unknown> = {}
  if (facts.contextWindow !== undefined) {
    record.contextWindowTokens = facts.contextWindow
    record.outputTokens = facts.outputReserve ?? Math.min(OUTPUT_DEFAULT_CEILING, Math.max(OUTPUT_FLOOR, Math.floor(facts.contextWindow / 8)))
  }
  for (const flag of ['nativeToolCalls', 'parallelToolCalls', 'vision', 'strictAlternation'] as const) if (facts[flag] !== undefined) record[flag] = facts[flag]
  if (facts.reasoningKind !== undefined) record.reasoning = { kind: facts.reasoningKind, ...(facts.reasoningIgnored === undefined ? {} : { ignored: facts.reasoningIgnored }) }
  if (facts.reasoningField !== undefined || facts.wholeJsonAnswer !== undefined) {
    record.streamingQuirks = { reasoningField: facts.reasoningField ?? null, wholeJsonAnswer: facts.wholeJsonAnswer ?? false }
  }
  if (facts.toolSchemas !== undefined) record.toolSchemas = facts.toolSchemas
  if (facts.statePlacement !== undefined) record.statePlacement = facts.statePlacement
  for (const key of Object.keys(record)) if (record[key] === undefined) delete record[key]
  return record as EndpointCapabilities
}

/**
 * The host's answer for one endpoint and model: what the one connection that
 * serves that model on that wire states. **Nothing where two connections serve
 * it and state different things**, because the harness asks by family and model
 * and cannot tell them apart; a run is bound to its own connection and carries
 * that connection's record on its session (`bindExecution`), which is exact.
 */
export function endpointCapabilities(endpoint: { family: EndpointKind; model: string }): EndpointCapabilities {
  const answers = connectionsServing(endpoint.family, endpoint.model).map((config) => capabilitiesOf(config.endpoint))
  const [first, ...rest] = answers
  if (first === undefined || rest.some((other) => JSON.stringify(other) !== JSON.stringify(first))) return {}
  return first
}

/**
 * The Desk's answer where the harness asks for an image as text, for a model
 * whose record says it is not shown images: the Desk has no render adapter for
 * images yet, so it refuses, and the harness puts a line saying the image was
 * not shown in its place. Nothing the Desk offers a run carries an image today.
 */
export const IMAGES_NOT_RENDERED = 'this desk does not render images as text'

export async function renderImageToText(): Promise<string> {
  throw new Error(IMAGES_NOT_RENDERED)
}
