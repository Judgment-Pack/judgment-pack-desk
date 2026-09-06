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
 * The tokens the desk leaves for the model's **answer**, beyond any thinking
 * budget.
 *
 * The built-in Anthropic provider asks for this many by default, and it is
 * enough for a whole pack and its explanation. It is here rather than only
 * there because the enabled dialect's budget and the request's maximum are one
 * decision — see the table.
 */
export const RESPONSE_TOKENS = 4096

/**
 * **The table.** One place, both engines, every family.
 *
 * `null` for `off`, because off is omission. `high` and `xhigh` are the two
 * efforts the desk's two tiers mean.
 *
 * **The enabled dialect sets two numbers, and it has to.** Anthropic's
 * `budget_tokens` must fit *below* the request's `max_tokens` — the budget is
 * spent out of the maximum — so a table that chose a budget of 8000 while the
 * provider asked for `max_tokens: 4096` produced a request every endpoint
 * requiring that dialect refuses. Both numbers are therefore this table's, per
 * tier: the budget, and the budget plus the desk's own response allowance. The
 * adaptive dialect needs neither, because there is no budget in it.
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
        members: {
          thinking: { type: 'enabled', budget_tokens: budget },
          // Strictly above the budget, or the endpoint refuses the request.
          max_tokens: budget + RESPONSE_TOKENS
        },
        expect: { path: 'thinking', value: { type: 'enabled', budget_tokens: budget } }
      }
    }
  }
}

/**
 * Every member the table can add to a request in order to **ask for thinking**.
 *
 * A closed list, because it is what a request is stripped of when the slot
 * degrades: a request that both asks for thinking and no longer carries a
 * signed block it was given is a request an endpoint may refuse outright.
 * `max_tokens` is deliberately not here — it is a member the protocol requires
 * on every Anthropic request, and a maximum left larger than the degraded
 * session needs is legal and harmless, where a *missing* one is not a request
 * at all.
 */
export const TIER_MEMBERS: readonly string[] = ['reasoning_effort', 'thinking', 'output_config']

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
 * The member names a thinking refusal may be **about**, and the whole of them.
 *
 * A closed list, and it is one half of the test below. `max_tokens` is not on
 * it: the desk raises the maximum *because* it asked for a budget, but a
 * refusal about `max_tokens` alone is a refusal about the size of an answer and
 * not about thinking.
 */
const THINKING_MEMBERS: readonly string[] = [
  'thinking',
  'reasoning_effort',
  'output_config',
  'budget_tokens'
]

/** Every name this desk actually sent, one level down as well as at the top. */
function namesSent(members: Record<string, unknown>): string[] {
  const names: string[] = []
  for (const [name, value] of Object.entries(members)) {
    if (!names.includes(name)) names.push(name)
    if (value !== null && typeof value === 'object' && !Array.isArray(value)) {
      for (const inner of Object.keys(value as Record<string, unknown>)) {
        if (!names.includes(inner)) names.push(inner)
      }
    }
  }
  return names.filter((name) => THINKING_MEMBERS.includes(name))
}

/**
 * Is this refusal about the thinking parameter this desk sent, or about the
 * request at large?
 *
 * **Two conditions, and both are required.** A 400 — and only a 400, because
 * that is the status every provider documents for a member it will not take —
 * whose message **names a member this desk actually added**, out of a closed
 * list of the members that ask for thinking at all.
 *
 * The prose alone is not enough, and that was the defect: `Unsupported
 * parameter` and `Extra inputs are not permitted` are the sentences an endpoint
 * writes about *any* member, so `400 Unsupported parameter: temperature`
 * degraded the tier, said "thinking is unavailable for this endpoint", and hid
 * a real failure behind it. A refusal that names none of the desk's own
 * thinking members is an ordinary model error and is reported as one.
 *
 * **Nothing here quotes the body beyond matching it.** The sentence a person
 * reads is the desk's, with the status in it.
 */
export function unsupportedThinking(
  status: number,
  message: string,
  members: Record<string, unknown>
): boolean {
  if (status !== 400) return false
  return namesSent(members).some((name) => message.includes(name))
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
 * How many consecutive turns an **inference from absence** takes.
 *
 * A refusal is the endpoint telling the desk something and is believed at once.
 * "This model always thinks" and "this endpoint has no thinking" inferred from
 * what a single turn did or did not carry are not that: they are one
 * observation promoted to a session-wide capability. Two consecutive turns
 * agreeing is the smallest thing that is not one turn — and where the endpoint
 * really has no thinking, two is one extra request.
 */
export const PERMANENCE = 2

/**
 * Whether **this** block's signature came back shorter than it went out.
 *
 * `vercel/ai#19663`: an Anthropic thinking signature split across two stream
 * events is truncated, and what is carried back on the next turn is one half of
 * it. Real Anthropic sends one `signature_delta`; a re-chunking proxy might
 * not, and a malformed thinking block is a request the endpoint refuses.
 *
 * **One block against the one signature it was given, and not against every
 * signature ever seen.** The comparison used to be a search over the whole
 * ledger, so a later block whose own signature was legitimately shorter — and
 * happened to be a prefix of an earlier one — was thrown away as a fragment.
 * Production signatures make that collision unlikely; the predicate is what has
 * to establish truncation, and a global membership test does not.
 *
 * The rule is a **strict** prefix or suffix: a signature carried back whole is
 * equal to what was sent and is not truncated.
 */
export function isTruncatedSignature(sent: string, carried: string): boolean {
  if (carried === '' || sent === '' || sent === carried) return false
  return sent.length > carried.length && (sent.startsWith(carried) || sent.endsWith(carried))
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
  /**
   * Reasoning arrived in the turn now in progress. Says nothing on its own.
   *
   * The **turn** is what carries evidence about an endpoint, not a passage, so
   * this only marks; `turnEnded` is where a state can change.
   */
  sawReasoning(): void
  /**
   * One model turn finished, and whether it carried an answer of its own.
   *
   * **This is where permanence is decided, and it takes more than one turn.**
   * A single observation is a turn, not a capability: one unsolicited passage
   * at tier off used to label a model "always thinks" for the session, and one
   * quiet first turn at tier on used to strip the parameter from every request
   * after it — so a tool-only turn, an endpoint's one-off omission or a gateway
   * hiccup became a conclusion about the endpoint. A **400 naming the member**
   * is immediate, because that is the endpoint saying so; an inference from
   * *absence* is not.
   *
   * `hadText` is whether the turn produced an answer rather than only a tool
   * call: a turn that said nothing but called a tool is no evidence that an
   * endpoint will not reason, so it is not counted either way.
   */
  turnEnded(hadText: boolean): AssistantEvent | null
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
  /** Reasoning seen in the turn now in progress. */
  let reasonedThisTurn = false
  /** Consecutive off-tier turns that reasoned, and on-tier answers that did not. */
  let reasoningRun = 0
  let quietRun = 0

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
      if (!unsupportedThinking(status, message, current.members)) {
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
    sawReasoning() {
      reasonedThisTurn = true
    },
    turnEnded(hadText) {
      const reasoned = reasonedThisTurn
      reasonedThisTurn = false
      // **A turn that only called a tool neither counts nor resets, in either
      // tier.** It is not evidence that an endpoint will not reason, and it is
      // not evidence that it will: several endpoints reason about the answer
      // they are composing and say nothing while they are fetching. The rule
      // was written for the on-tier counter and applied only there, so at tier
      // off a reasoning answer, a tool call and a second reasoning answer never
      // reached "always" — the tool call in the middle wiped the count.
      if (!hadText) return null
      if (tier === 'off') {
        // An answer that returned no reasoning is counter-evidence, so the run
        // starts again: "always" means every answer, not one of them.
        reasoningRun = reasoned ? reasoningRun + 1 : 0
        if (always || reasoningRun < PERMANENCE) return null
        always = true
        return notice('always', '')
      }
      if (unavailable) return null
      if (reasoned) {
        quietRun = 0
        return null
      }
      quietRun += 1
      if (quietRun < PERMANENCE) return null
      unavailable = true
      return notice(
        'unavailable',
        `${PERMANENCE} consecutive turns carried an answer and no reasoning block`
      )
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
