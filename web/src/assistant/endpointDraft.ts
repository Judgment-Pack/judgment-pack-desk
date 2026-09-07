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
  ASSISTANT_ENGINES,
  ASSISTANT_KINDS,
  ASSISTANT_THINKING,
  ASSISTANT_TOOLS,
  type AssistantConfig,
  type AssistantEngine,
  type AssistantTool,
  type EndpointKind,
  type ThinkingTier
} from '../config/deskConfig'
import { firstDialect, wireFor } from './thinking'

/** What the form holds while it is being edited. */
export interface EndpointDraft {
  kind: EndpointKind
  url: string
  model: string
  tools: AssistantTool[]
  engine: AssistantEngine
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
  gemini: 'Gemini'
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
    url: endpoint?.url ?? '',
    model: endpoint?.model ?? '',
    tools:
      endpoint === null
        ? [...ASSISTANT_TOOLS]
        : ASSISTANT_TOOLS.filter((tool) => endpoint.tools.includes(tool)),
    engine: config.engine,
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
      model: draft.model.trim(),
      tools: ASSISTANT_TOOLS.filter((tool) => draft.tools.includes(tool))
    },
    engine: draft.engine,
    thinking: draft.thinking
  }
}

/** The engine options, in the order the closed list declares them. */
export const ENGINE_OPTIONS = ASSISTANT_ENGINES.map((engine) => ({
  value: engine,
  label: engine
}))

/** The tier options, in the order the closed list declares them. */
export const TIER_OPTIONS = ASSISTANT_THINKING.map((tier) => ({
  value: tier,
  label: tier
}))

/** The kinds, in the order the closed list declares them. */
export const KIND_OPTIONS = ASSISTANT_KINDS.map((kind) => ({
  value: kind,
  label: KIND_LABEL[kind]
}))

/**
 * What a tier puts on the wire for this endpoint's family, **read off the
 * desk's own table**.
 *
 * Derived rather than restated: a sentence typed out beside the picker would
 * be a second copy of `thinking.ts` that nothing keeps in step, and the first
 * time the table changed the form would describe the release before it. What
 * a reader sees is the members `wireFor` actually returns for the family's
 * first dialect, so a change to the table changes this line.
 */
export function tierSays(family: EndpointKind, tier: ThinkingTier): string {
  const wire = wireFor(tier, firstDialect(family))
  if (wire === null) return 'nothing at all goes on the wire'
  return JSON.stringify(wire.members)
}

/**
 * The sentence beside the tier picker that is **not** derived, because it is
 * about where the numbers came from rather than what they are.
 *
 * Only the Gemini wire carries one, and only because only that wire takes a
 * token budget: the allowed range is per model and the API reference states
 * none that holds across the family, so the two budgets are this desk's choice
 * inside a documented field. A model whose range excludes one answers 400 and
 * the desk falls back to the level spelling once. The other two families put a
 * named effort on the wire and have no number to account for.
 */
export function tierProvenance(family: EndpointKind): string | undefined {
  if (family !== 'gemini') return undefined
  return (
    'The two budgets are this desk’s choice inside a documented field, not a range quoted ' +
    'from anywhere: the allowed range is per model. A model whose range excludes one answers ' +
    'with a refusal and the desk asks again in the other spelling, once.'
  )
}

/**
 * What each engine is, and what the SDK-backed one cannot do.
 *
 * **Both limits are measured and declared elsewhere in this repository**, and
 * they are here because they are the two reasons an author might choose
 * `builtin` for a Gemini endpoint. Neither is a guess about a vendor: each is
 * a behaviour this desk's own suite pins, and each is stated in the README
 * beside the measurement.
 */
export const ENGINE_SAYS: Readonly<Record<AssistantEngine, string[]>> = {
  vercel: [
    'The default. It shows the model the tool schemas the runtime served, narrowed where the ' +
      'SDK declares one narrower — the tab says “narrowed” when it does, and on the Gemini ' +
      'wire list_examples arrives with no parameters at all.',
    'It cannot carry an empty signed thought part back across a tool turn on the Gemini wire, ' +
      'so a session that meets one degrades once and says so.'
  ],
  builtin: [
    'A fallback that adds nothing to what this desk already ships, and has neither limit above: ' +
      'it shows the runtime’s own schemas and echoes a model’s turn exactly as it arrived.'
  ]
}
