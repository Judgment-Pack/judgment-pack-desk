import type { EndpointCapabilities, EndpointKind } from '@judgment-pack/harness/contract'

/**
 * What the Desk tells the harness about one endpoint and model, beyond its
 * family: members of the capability record of `@judgment-pack/harness`
 * (`capabilities.ts`, the contract's `capability.json`), among them where the
 * execution state goes (`statePlacement`) and whether the endpoint keeps a
 * strict turn order (`strictAlternation`).
 *
 * **Nothing, today.** The Desk has no endpoint probe and its endpoint form
 * records no capability (agent#4 leaves both to the host), so every endpoint
 * runs on its family's default record, `FAMILY_CAPABILITIES`: native and
 * parallel tool calls, the family's reasoning dialect, the Gemini schema subset
 * on Gemini alone, images shown to Anthropic and Gemini models and rendered as
 * text for an OpenAI-compatible one, and the state trailing the history, or at
 * the end of the system text for Anthropic with a thinking tier on. The
 * assistant profile's per-model window and output cap (`context.models`) stay
 * the last word, through the harness's `contextPolicy`, as before.
 *
 * An endpoint whose chat template refuses two user turns in a row, or a user
 * text after a tool result, answers `{ strictAlternation: true }` here once the
 * Desk can tell; until then such an endpoint refuses the second request of a
 * run, and the run reports it as the endpoint's refusal.
 */
export function endpointCapabilities(_endpoint: { family: EndpointKind; model: string }): EndpointCapabilities {
  return {}
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
