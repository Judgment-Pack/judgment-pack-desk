/**
 * The Admin form's draft, and the object it writes.
 *
 * **Nothing here is a control and nothing here renders.** It is the arithmetic
 * between what an author typed and the `assistant` object the shared decoder
 * accepts, kept apart from the form so that the one property that matters can
 * be asserted without a render: **the written object is composed by naming its
 * members, never by spreading a draft**. A spread is how a member nobody
 * declared reaches a file — and the member this desk fears is a credential.
 *
 * The endpoint's four members and the two settings beside them are exactly
 * `AssistantConfig`, so a draft is a flat record of six fields and there is
 * no shape here the schema does not already have.
 */
import {
  ASSISTANT_KINDS,
  ASSISTANT_THINKING,
  ASSISTANT_TOOLS,
  type AssistantConfig,
  type AssistantTool,
  type EndpointKind,
  type ThinkingTier
} from '../config/deskConfig'

/**
 * What the form holds while it is being edited.
 *
 * **`engine` is not on it, and the form never writes one.** The built-in engine
 * was withdrawn and the slot has one member, so there is nothing to choose; the
 * schema still decodes the member, with a migration, for a file that names it
 * (see `engineValue` in `deskConfig.ts`). A file that carries `engine` keeps it
 * until its next write, and that write drops it — the composed `assistant`
 * object replaces the one in the file whole.
 */
export interface EndpointDraft {
  kind: EndpointKind
  url: string
  /**
   * The **default** of the set below, and `''` where nothing is enabled.
   *
   * A string throughout, because that is what a control holds; `assistantWrite`
   * is where `''` becomes the null the schema spells.
   */
  model: string
  /** The enabled set, in the order the boxes were ticked over the file's own. */
  models: string[]
  tools: AssistantTool[]
  thinking: ThinkingTier
}

/**
 * What each wire protocol is called on the page.
 *
 * The `kind` values are the file's and are shown as they are written wherever
 * a value is *reported*; these are what a person chooses between, because
 * `openai-compatible` is a spelling and not a name.
 */
export const KIND_LABEL: Readonly<Record<EndpointKind, string>> = {
  'openai-compatible': 'OpenAI-compatible',
  anthropic: 'Anthropic',
  gemini: 'Google Gemini'
}

/**
 * The address each protocol's own reference documents as its base, offered as
 * a starting point.
 *
 * **A default put into an editable field, and never a destination this desk
 * holds.** Nothing reads these back, compares an endpoint to them, or treats
 * an endpoint at one of them differently from an endpoint anywhere else —
 * which is the claim `enforcement.test.ts` (4) holds, and it is untouched by
 * a prefill. What this saves is an author retyping the base the README already
 * names, and every one of them is replaced by typing over it.
 *
 * Each is the base that carries the protocol's own path and no more of it:
 * `/models` and `/chat/completions` for the OpenAI-compatible wire (so, the
 * `/v1`), `/v1/messages` for Anthropic (so, the origin), `/v1beta/models` for
 * the native Gemini wire (so, the origin). The desk appends the path its
 * protocol prescribes and never guesses a version segment.
 *
 * `enforcement.test.ts` (5) admits these three literals **only as values of
 * this table and only in this module**, so a vendor address cannot appear
 * anywhere else in the page's source.
 */
export const PREFILLED_URL: Readonly<Record<EndpointKind, string>> = {
  'openai-compatible': 'https://api.openai.com/v1',
  anthropic: 'https://api.anthropic.com',
  gemini: 'https://generativelanguage.googleapis.com'
}

/**
 * The draft an effective configuration starts the form at.
 *
 * A desk that has configured nothing gets every tool on, because the tool list
 * is required and `[]` means an assistant that may call nothing — a form that
 * opened on the empty list would offer that as the default, which is a
 * capability decision made by a blank field rather than by a person.
 */
export function draftFrom(config: AssistantConfig): EndpointDraft {
  const endpoint = config.endpoint
  return {
    kind: endpoint?.kind ?? 'openai-compatible',
    url: endpoint?.url ?? PREFILLED_URL['openai-compatible'],
    // The draft holds a string throughout, and `''` is what "nothing chosen"
    // looks like in a text field; `assistantWrite` is where it becomes the
    // null the schema spells.
    model: endpoint?.model ?? '',
    // **The file's own set, and never a list from anywhere else.** A form that
    // opened on what an endpoint happened to list would show a set nobody
    // enabled, under a Save that would write it.
    models: endpoint?.models ?? [],
    tools:
      endpoint === null
        ? [...ASSISTANT_TOOLS]
        : ASSISTANT_TOOLS.filter((tool) => endpoint.tools.includes(tool)),
    thinking: config.thinking
  }
}

/**
 * What a read of the file the form is seeded from is, as one string.
 *
 * The form re-seeds from the configuration whenever this changes **and the
 * author has typed nothing** — the answer to a read that had not arrived at
 * first render, and to a Save that landed. It deliberately does not re-seed
 * over an edit in progress: that is what makes Reload after a stale write keep
 * the values somebody typed.
 */
export function seedOf(config: AssistantConfig): string {
  return JSON.stringify(draftFrom(config))
}

/**
 * Choose a wire protocol, prefilling the URL where the author has not written
 * one of their own.
 *
 * "Has not written one of their own" is exactly two cases: an empty field, and
 * a field still holding the previous kind's prefill. Anything else is somebody's
 * own address and is left alone — a picker that overwrote it would lose the
 * URL on a mis-click with no way back.
 */
export function withKind(draft: EndpointDraft, kind: EndpointKind): EndpointDraft {
  const typed = draft.url.trim()
  const offered = typed === '' || typed === PREFILLED_URL[draft.kind]
  return { ...draft, kind, url: offered ? PREFILLED_URL[kind] : draft.url }
}

/**
 * Enable or disable one model, keeping a default the set can actually have.
 *
 * **A non-empty set with no default is a configuration the decoder refuses by
 * name**, so the two move together: ticking the first model chooses it, and
 * unticking the default moves the default to what is left — or to `''` where
 * nothing is. A form that let the two drift apart would compose a file its own
 * reader rejects, out of two clicks that each looked reasonable.
 *
 * An id already in the set is not added twice, which is the same rule the
 * decoder holds the member to.
 */
export function withModel(draft: EndpointDraft, id: string, enabled: boolean): EndpointDraft {
  const models = enabled
    ? draft.models.includes(id)
      ? draft.models
      : [...draft.models, id]
    : draft.models.filter((each) => each !== id)
  const model = models.includes(draft.model) ? draft.model : (models[0] ?? '')
  return { ...draft, models, model }
}

/**
 * Make one enabled model the default.
 *
 * **Only a member of the set may be it.** The radio is offered on enabled rows
 * alone, and this is the second layer under that: a default nothing enabled is
 * the state the decoder refuses, and a setter that could reach it would be a
 * control composing that file.
 */
export function withDefaultModel(draft: EndpointDraft, id: string): EndpointDraft {
  if (!draft.models.includes(id)) return draft
  return { ...draft, model: id }
}

/** Turn one tool on or off, keeping the closed list's own order. */
export function withTool(
  draft: EndpointDraft,
  tool: AssistantTool,
  granted: boolean
): EndpointDraft {
  const wanted = new Set(draft.tools)
  if (granted) wanted.add(tool)
  else wanted.delete(tool)
  return { ...draft, tools: ASSISTANT_TOOLS.filter((name) => wanted.has(name)) }
}

/**
 * The `assistant` object this form sends, composed **by naming every member**.
 *
 * There is no spread of the draft here and there must never be one. The draft
 * is page state — it is merged, re-seeded and passed through setters — and a
 * spread would write whatever it happened to be carrying into the one file on
 * this machine that names where a credential is presented. The chassis decodes
 * the composed file and would refuse a key-shaped member, so a spread is not a
 * hole; it is a request this desk should never have made, and the difference
 * is the difference between a rule and a backstop.
 *
 * **`tools` is derived from the closed list rather than copied from the
 * draft**, for the same reason and with a second benefit: the written order is
 * the schema's order whatever order the checkboxes were clicked in, so a file
 * rewritten with no change to the choice is byte-identical.
 */
export function assistantWrite(draft: EndpointDraft): unknown {
  return {
    endpoint: {
      url: draft.url.trim(),
      kind: draft.kind,
      // **Null and not the empty string.** "No model chosen yet" is a state the
      // schema has and `""` is a value it refuses, so a form that wrote the
      // empty string would compose a file its own reader rejects — on the very
      // first save, which is the one that has to work before a list can be
      // asked for.
      model: draft.model.trim() === '' ? null : draft.model.trim(),
      // **The set, named like every other member and trimmed like the default.**
      // What the decoder accepts is the trimmed id, so a set written untrimmed
      // would be a form showing one string and writing another.
      models: draft.models.map((id) => id.trim()),
      tools: ASSISTANT_TOOLS.filter((tool) => draft.tools.includes(tool))
    },
    thinking: draft.thinking
  }
}

/**
 * The other state the slot has: **None**, written as the schema spells it.
 *
 * `assistant.endpoint` is one nullable field, and until this existed the form
 * could not write the null — clearing the fields sent an object the decoder
 * refuses, so a desk that had configured an endpoint could only get back to
 * None through the generic file editor. The page describes None as one of the
 * three deployment states, so a page that cannot reach it is a page describing
 * something it does not offer.
 *
 * **`thinking` survives**, because it says *how* an assistant would run and not
 * whether there is one — the schema allows it beside a null endpoint for
 * exactly that reason, and a removal that reset it would be discarding a
 * decision nobody asked about.
 */
export function assistantWithoutEndpoint(draft: EndpointDraft): unknown {
  return { endpoint: null, thinking: draft.thinking }
}

/**
 * What each thinking tier is called on the page.
 *
 * The `thinking` values are the file's and are shown as they are written
 * wherever a value is *reported*; these are what a person chooses between,
 * because `ultra` is a spelling and not an amount.
 */
export const TIER_LABEL: Readonly<Record<ThinkingTier, string>> = {
  off: 'off',
  on: 'standard',
  ultra: 'deep'
}

/** The tier options, in the order the closed list declares them. */
export const TIER_OPTIONS = ASSISTANT_THINKING.map((tier) => ({
  value: tier,
  label: TIER_LABEL[tier]
}))

/** The kinds, in the order the closed list declares them. */
export const KIND_OPTIONS = ASSISTANT_KINDS.map((kind) => ({
  value: kind,
  label: KIND_LABEL[kind]
}))
