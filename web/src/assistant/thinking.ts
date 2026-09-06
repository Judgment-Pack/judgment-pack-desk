/**
 * The thinking tier, normalized in the desk.
 *
 * ADR-0001: *"The tier maps to provider parameters in one desk-owned table, per
 * endpoint family, and the engine receives the normalized result. The slot has
 * five states, three of them measured: `off`, `on`, `ultra`, **"this model
 * always thinks"** and **"unavailable for this endpoint"**. No framework
 * supplies the last two; they are desk code whichever engine runs."*
 *
 * So this module is the whole of what an engine is allowed to know about
 * thinking: **one table**, one closed list of refusals, one lifecycle, and one
 * sentence per state. An engine reads members off the slot and puts them on the
 * wire; it never chooses a parameter, never decides what a 400 meant, and never
 * writes a line about the tier.
 *
 * **`off` is expressed by omission, everywhere.** Anthropic rejects
 * `{"type":"disabled"}` on the models that always think, and several
 * OpenAI-compatible endpoints answer 400 to `reasoning_effort: "none"`, so
 * *send nothing* is the only spelling of off that every endpoint accepts. That
 * is why "this model always thinks" is **detected from reasoning arriving with
 * the tier off** rather than from a refusal: with the tier off there is no
 * request for an endpoint to refuse.
 *
 * Ported from the bake-off's `none` prototype (`fixture/THINKING-SPEC.md`, and
 * `none/src/thinking.ts`), reduced to the two families this desk configures.
 */
import type { EndpointKind, ThinkingTier } from '../config/deskConfig'
import type { AssistantEvent, AssistantSession } from './engine'

/**
 * The five states, three of which a person may select.
 *
 * `always` and `unavailable` are reports and not settings: `deskConfig` admits
 * `off`, `on` and `ultra` and nothing else, deliberately, because a state the
 * desk **discovers** is not a state anybody can ask for.
 */
export type ThinkingState = 'off' | 'on' | 'ultra' | 'always' | 'unavailable'

/**
 * The endpoint's vocabulary. One slot tier, three spellings.
 *
 * The families are the desk's two — `openai-compatible` and `anthropic` — and
 * the second has two dialects because the depth moved between model
 * generations: current models take `thinking: {type: "adaptive"}` with the
 * depth in a **sibling** `output_config`, and 4.5-era models take a token
 * budget inside the thinking member and reject the adaptive spelling. There is
 * no capability endpoint on either API, so the probe is the first real request
 * and the fallback is this list.
 */
export type ThinkingDialect = 'openai' | 'anthropic-adaptive' | 'anthropic-enabled'

/** What one (family, tier) pair puts on the wire, and what that is called. */
export interface WireThinking {
  /** Top-level body members, merged into the request by whichever engine runs. */
  members: Record<string, unknown>
  /** The member a checker reads, so a leg can assert the parameter and not a shape. */
  expect: { path: string; value: unknown }
}

/**
 * **The table.** One place, both engines, every family.
 *
 * `null` for `off`, because off is omission. `high` and `xhigh` are the two
 * efforts the desk's two tiers mean; the Anthropic budgets are the prototype's
 * measured pair, 8000 for `on` and 16000 for `ultra`, both above the documented
 * 1024 floor and below the desk's `max_tokens`.
 */
export function wireFor(tier: ThinkingTier, dialect: ThinkingDialect): WireThinking | null {
  if (tier === 'off') return null
  const effort = tier === 'ultra' ? 'xhigh' : 'high'
  switch (dialect) {
    case 'openai':
      return {
        members: { reasoning_effort: effort },
        expect: { path: 'reasoning_effort', value: effort }
      }
    case 'anthropic-adaptive':
      return {
        members: { thinking: { type: 'adaptive' }, output_config: { effort } },
        expect: { path: 'thinking', value: { type: 'adaptive' } }
      }
    case 'anthropic-enabled': {
      const budget = tier === 'ultra' ? 16000 : 8000
      return {
        members: { thinking: { type: 'enabled', budget_tokens: budget } },
        expect: { path: 'thinking', value: { type: 'enabled', budget_tokens: budget } }
      }
    }
  }
}

/** The dialect a family is tried at first. */
export function firstDialect(family: EndpointKind): ThinkingDialect {
  return family === 'anthropic' ? 'anthropic-adaptive' : 'openai'
}

/**
 * The one fallback there is, or nothing.
 *
 * ADR-0001 puts "the dialect fallback between thinking spellings" in the desk
 * on every engine. It is **one** step: adaptive, then the token budget, then
 * the endpoint has no thinking this desk knows how to ask for.
 */
export function nextDialect(dialect: ThinkingDialect): ThinkingDialect | null {
  return dialect === 'anthropic-adaptive' ? 'anthropic-enabled' : null
}

/**
 * The documented refusals, as a **closed list**.
 *
 * From the providers' own error references and the fixture's degrade shape. It
 * is closed on purpose: an open reading of an endpoint's body — a search for
 * "thinking" anywhere in it — would let an endpoint's prose about a document
 * turn a real failure into a silent degrade.
 *
 * **Nothing here quotes the body beyond matching it.** The sentence a person
 * reads is the desk's, with the status in it; the endpoint's own words are
 * matched against these patterns and against the member names this desk
 * actually sent, and go no further.
 */
const UNSUPPORTED: readonly RegExp[] = [
  /unsupported parameter/i,
  /unknown parameter/i,
  /unrecognized (?:request )?argument/i,
  /extra inputs are not permitted/i,
  /is not supported for this model/i,
  /adaptive thinking is not supported/i,
  /does not support .{0,40}reasoning[_ ]effort/i
]

/**
 * Is this refusal about the thinking parameter this desk sent, or about the
 * request at large?
 *
 * A 400 or a 422, one of the documented patterns **or** the name of a member
 * this desk actually added — never a match against the request as a whole.
 */
export function unsupportedThinking(
  status: number,
  message: string,
  members: readonly string[]
): boolean {
  if (status !== 400 && status !== 422) return false
  if (UNSUPPORTED.some((pattern) => pattern.test(message))) return true
  return members.some((member) => message.includes(member))
}

/**
 * **The ruling this chunk carries, as one boolean.**
 *
 * ADR-0001's open questions include *"whether the refutation pass should still
 * run its `validate` check on a degraded endpoint (every prototype silently
 * dropped it; a ruling is needed before the tier chunk merges)"*.
 *
 * The default built here is **true**: the pass runs. Its value is the
 * runtime's checks over the proposed document, not the model's thinking, and a
 * `validate` costs one call. The alternative — the pass is skipped where the
 * endpoint degraded — is this constant set to `false` and nothing else, and
 * the tab's line then says the pass did not run.
 *
 * The maintainer has not ruled. Until they do, this is a default and the PR
 * that carries it says so in its first paragraph.
 */
export const REFUTE_ON_A_DEGRADED_ENDPOINT = true

/**
 * A signature that came back shorter than it went out.
 *
 * `vercel/ai#19663`: an Anthropic thinking signature split across two stream
 * events is truncated, and what is carried back on the next turn is one half of
 * it. Real Anthropic sends one `signature_delta`; a re-chunking proxy might
 * not, and a malformed thinking block is a request the endpoint refuses.
 *
 * The rule is a **strict** prefix or suffix: a signature carried back whole is
 * equal to what was received and is not truncated, and a signature this desk
 * never saw is somebody else's and is not this desk's business.
 */
export function isTruncatedSignature(received: readonly string[], carried: string): boolean {
  if (carried === '') return false
  return received.some(
    (whole) =>
      whole !== carried && whole.length > carried.length &&
      (whole.startsWith(carried) || whole.endsWith(carried))
  )
}

/**
 * The sentence for each state, and the whole of what the desk says about the
 * tier.
 *
 * Exported because the tab's status line, the event stream and the state
 * reader below all have to agree — a second spelling of "this did not happen"
 * is how a reader learns to skip the line.
 */
export const THINKING_ALWAYS =
  'this model always thinks: no thinking parameter was sent at tier "off" and the endpoint ' +
  'returned reasoning anyway'

export const THINKING_UNAVAILABLE = 'thinking is unavailable for this endpoint'

/** The desk's own notice, with the reason after it. */
export function thinkingNotice(state: 'always' | 'unavailable', reason: string): string {
  const head = state === 'always' ? THINKING_ALWAYS : THINKING_UNAVAILABLE
  return reason === '' ? `${head}.` : `${head}: ${reason}`
}

/** One line for the tab, naming the tier the file asked for and the real state. */
export function thinkingLine(tier: ThinkingTier, state: ThinkingState): string {
  if (state === 'always') return `thinking ${tier} · this model always thinks`
  if (state === 'unavailable') return `thinking ${tier} · unavailable for this endpoint`
  return `thinking ${state}`
}

/**
 * The state a finished run reached, read off its own events.
 *
 * The pane sees events and nothing else, so the state it shows is derived from
 * them rather than remembered somewhere a second reader could disagree with.
 */
export function stateFromEvents(
  tier: ThinkingTier,
  events: readonly AssistantEvent[]
): ThinkingState {
  const notices = events.filter(
    (event): event is Extract<AssistantEvent, { type: 'thinking_unavailable' }> =>
      event.type === 'thinking_unavailable'
  )
  if (notices.some((notice) => notice.detail.startsWith(THINKING_ALWAYS))) return 'always'
  if (notices.length > 0) return 'unavailable'
  return tier
}

/** The normalized result the session carries: the tier, its wire, its state. */
export interface NormalizedThinking {
  tier: ThinkingTier
  /** What the first request carries, or null at `off`. */
  wire: WireThinking | null
  /** Where the slot starts. `always` and `unavailable` are only ever reached later. */
  state: ThinkingState
}

/**
 * The tier, normalized once, where the session is bound.
 *
 * Both bind sites — the run hook and the conformance session — call this, so
 * an engine is handed the same object on the page and in CI.
 */
export function normalize(tier: ThinkingTier, family: EndpointKind): NormalizedThinking {
  return { tier, wire: wireFor(tier, firstDialect(family)), state: tier }
}

/** What a refused request means, decided here and never in an engine. */
export type ThinkingRefusal =
  /** Not about the tier: the engine reports the failure as it always would. */
  | { kind: 'other' }
  /** Try the same request again at the next dialect. Nothing is said. */
  | { kind: 'retry' }
  /**
   * The endpoint has no thinking. The request is retried plain, and the notice
   * is emitted **once for the session** — `event` is null where the desk has
   * already said it, so an engine cannot announce a degrade twice by being
   * asked twice.
   */
  | { kind: 'degrade'; event: AssistantEvent | null }

/**
 * One session's thinking slot: the table, the fallback and the degrade,
 * together, and each of them **once**.
 *
 * Held by whichever engine runs, constructed from the normalized result the
 * desk put on the session. Every transition is a method that returns the event
 * to emit, or null where the desk has already said it: an engine cannot
 * announce a degrade twice because it is not the thing counting.
 */
export interface ThinkingSlot {
  readonly tier: ThinkingTier
  /** The current state, which only ever moves away from the tier. */
  state(): ThinkingState
  /** The members this request carries, or null. */
  members(): Record<string, unknown> | null
  /** What the endpoint's refusal meant. */
  refused(status: number, message: string): ThinkingRefusal
  /** Reasoning arrived. Returns the `always` notice the first time it matters. */
  reasoned(): AssistantEvent | null
  /**
   * A turn finished with the tier on and no reasoning in it.
   *
   * ADR-0001's second unmeasurable state: "no thinking block after the first
   * turn". Returns the notice the first time, and null afterwards.
   */
  silent(): AssistantEvent | null
  /** A carried signature came back short. Degrades, once, with the reason. */
  truncated(reason: string): AssistantEvent | null
  /** Whether the refutation pass runs, under the ruling above. */
  runsRefutation(): boolean
}

export function openThinking(session: Pick<AssistantSession, 'thinking' | 'model'>): ThinkingSlot {
  const tier = session.thinking.tier
  let dialect = firstDialect(session.model.family)
  let unavailable = false
  let always = false
  let announced = false
  /** True once the first model turn has been accounted for, either way. */
  let firstTurnDone = false

  const notice = (state: 'always' | 'unavailable', reason: string): AssistantEvent | null => {
    if (announced) return null
    announced = true
    return { type: 'thinking_unavailable', detail: thinkingNotice(state, reason) }
  }

  const wire = () => (tier === 'off' || unavailable ? null : wireFor(tier, dialect))

  return {
    tier,
    state: () => (always ? 'always' : unavailable ? 'unavailable' : tier),
    members: () => wire()?.members ?? null,
    refused(status, message) {
      const current = wire()
      if (current === null) return { kind: 'other' }
      if (!unsupportedThinking(status, message, Object.keys(current.members))) {
        return { kind: 'other' }
      }
      const next = nextDialect(dialect)
      if (next !== null) {
        // **The fallback is silent, because it is not a degrade.** The desk
        // asked in one spelling and asks again in the other; the session still
        // thinks, and a line about it would be a line about this desk's
        // vocabulary rather than about the endpoint.
        dialect = next
        return { kind: 'retry' }
      }
      unavailable = true
      // The status, and the desk's own sentence. The endpoint's body is
      // matched against a closed list and never quoted past it.
      return {
        kind: 'degrade',
        event: notice('unavailable', `the endpoint answered ${status} to the tier parameter`)
      }
    },
    reasoned() {
      firstTurnDone = true
      if (tier !== 'off' || always) return null
      always = true
      return notice('always', '')
    },
    silent() {
      if (firstTurnDone) return null
      firstTurnDone = true
      if (tier === 'off' || unavailable) return null
      unavailable = true
      return notice('unavailable', 'the first turn carried no reasoning block')
    },
    truncated(reason) {
      if (unavailable) return null
      unavailable = true
      return notice('unavailable', reason)
    },
    runsRefutation() {
      if (tier === 'off') return false
      return REFUTE_ON_A_DEGRADED_ENDPOINT || (!unavailable && !always)
    }
  }
}
