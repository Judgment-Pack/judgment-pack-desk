/**
 * The desk-owned tier table, its one fallback, and the five states.
 *
 * Everything here is the **desk's** rather than either engine's: the table, the
 * closed list of refusals, the dialect fallback, and the two states a tier
 * cannot express. The engines' own suites measure what reaches the wire; this
 * measures what the desk decided to put there.
 */
import { describe, expect, it } from 'vitest'
import {
  REFUTE_ON_A_DEGRADED_ENDPOINT,
  RESPONSE_TOKENS,
  THINKING_ALWAYS,
  THINKING_UNAVAILABLE,
  firstDialect,
  isTruncatedSignature,
  nextDialect,
  normalize,
  openThinking,
  stateFromEvents,
  thinkingLine,
  unsupportedThinking,
  wireFor
} from './thinking'
import type { AssistantEvent, AssistantSession } from './engine'
import type { EndpointKind, ThinkingTier } from '../config/deskConfig'

const slot = (tier: ThinkingTier, family: EndpointKind = 'openai-compatible') =>
  openThinking({
    thinking: normalize(tier, family),
    model: { family, model: 'a-model', call: async () => new Response('{}') }
  } as Pick<AssistantSession, 'thinking' | 'model'>)

describe('the table, per family and tier', () => {
  it('expresses off by omission on every dialect', () => {
    expect(wireFor('off', 'openai')).toBeNull()
    expect(wireFor('off', 'anthropic-adaptive')).toBeNull()
    expect(wireFor('off', 'anthropic-enabled')).toBeNull()
  })

  it('sends reasoning_effort on the OpenAI-compatible wire', () => {
    expect(wireFor('on', 'openai')!.members).toEqual({ reasoning_effort: 'high' })
    expect(wireFor('ultra', 'openai')!.members).toEqual({ reasoning_effort: 'xhigh' })
    expect(wireFor('on', 'openai')!.expect).toEqual({ path: 'reasoning_effort', value: 'high' })
  })

  it('sends the adaptive spelling first, with the depth in its sibling', () => {
    // The depth lives in `output_config`, not in the thinking member: that is
    // the current models' shape and it is why there are two dialects at all.
    expect(wireFor('on', 'anthropic-adaptive')!.members).toEqual({
      thinking: { type: 'adaptive' },
      output_config: { effort: 'high' }
    })
    expect(wireFor('ultra', 'anthropic-adaptive')!.members).toEqual({
      thinking: { type: 'adaptive' },
      output_config: { effort: 'xhigh' }
    })
  })

  it('falls back to a token budget, with the maximum that budget requires', () => {
    // **Two numbers, one decision.** Anthropic spends the thinking budget out
    // of `max_tokens`, so a table that chose a budget of 8000 while the request
    // asked for 4096 produced a request every enabled-dialect endpoint refuses.
    expect(wireFor('on', 'anthropic-enabled')!.members).toEqual({
      thinking: { type: 'enabled', budget_tokens: 8000 },
      max_tokens: 8000 + RESPONSE_TOKENS
    })
    expect(wireFor('ultra', 'anthropic-enabled')!.members).toEqual({
      thinking: { type: 'enabled', budget_tokens: 16000 },
      max_tokens: 16000 + RESPONSE_TOKENS
    })
    // Strictly above, on every tier, which is what the endpoint enforces.
    for (const tier of ['on', 'ultra'] as const) {
      const members = wireFor(tier, 'anthropic-enabled')!.members as {
        thinking: { budget_tokens: number }
        max_tokens: number
      }
      expect(members.max_tokens, tier).toBeGreaterThan(members.thinking.budget_tokens)
    }
  })

  it('leaves the maximum alone on the dialect that has no budget', () => {
    expect(Object.keys(wireFor('on', 'anthropic-adaptive')!.members).sort()).toEqual([
      'output_config',
      'thinking'
    ])
    expect(Object.keys(wireFor('on', 'openai')!.members)).toEqual(['reasoning_effort'])
  })

  it('starts each family at its own dialect and offers exactly one fallback', () => {
    expect(firstDialect('anthropic')).toBe('anthropic-adaptive')
    expect(firstDialect('openai-compatible')).toBe('openai')
    expect(nextDialect('anthropic-adaptive')).toBe('anthropic-enabled')
    expect(nextDialect('anthropic-enabled')).toBeNull()
    expect(nextDialect('openai')).toBeNull()
  })

  it('normalizes the tier into the wire the family takes', () => {
    expect(normalize('on', 'anthropic')).toEqual({
      tier: 'on',
      wire: wireFor('on', 'anthropic-adaptive'),
      state: 'on'
    })
    expect(normalize('off', 'openai-compatible')).toEqual({ tier: 'off', wire: null, state: 'off' })
  })
})

describe('the closed list of refusals', () => {
  const members = ['reasoning_effort']

  it('reads only 400 and 422', () => {
    expect(unsupportedThinking(500, 'Unsupported parameter: reasoning_effort', members)).toBe(false)
    expect(unsupportedThinking(400, 'Unsupported parameter: reasoning_effort', members)).toBe(true)
    expect(unsupportedThinking(422, 'Unsupported parameter: reasoning_effort', members)).toBe(true)
  })

  it('matches each documented pattern', () => {
    for (const message of [
      'Unsupported parameter: reasoning_effort',
      'Unknown parameter: thinking',
      'Unrecognized request argument supplied: thinking',
      'Extra inputs are not permitted',
      'thinking.type.enabled: Extended thinking is not supported for this model',
      'Adaptive thinking is not supported by this model',
      'This model does not support the reasoning_effort parameter'
    ]) {
      expect(unsupportedThinking(400, message, ['thinking']), message).toBe(true)
    }
  })

  it('matches a gateway that names only the member this desk sent', () => {
    expect(unsupportedThinking(400, 'bad request: output_config', ['thinking', 'output_config'])).toBe(
      true
    )
    // …and never a member this desk did not send.
    expect(unsupportedThinking(400, 'bad request: temperature', ['thinking'])).toBe(false)
  })

  it('does not read an endpoint’s prose about a document as a refusal of the tier', () => {
    // The list is closed for this: an open search for "thinking" in a body
    // would turn a real failure into a silent degrade.
    expect(
      unsupportedThinking(400, 'the pack is invalid: rule 3 names no outcome', ['reasoning_effort'])
    ).toBe(false)
  })
})

describe('the five states', () => {
  it('is the tier while nothing has happened', () => {
    expect(slot('off').state()).toBe('off')
    expect(slot('on').state()).toBe('on')
    expect(slot('ultra').state()).toBe('ultra')
  })

  it('reports "always" when reasoning arrives with the tier off, once', () => {
    const it0 = slot('off')
    expect(it0.members()).toBeNull()
    const first = it0.reasoned()
    expect(first!.type).toBe('thinking_unavailable')
    expect((first as { detail: string }).detail).toContain(THINKING_ALWAYS)
    expect(it0.state()).toBe('always')
    // Once. A second passage of reasoning is not a second line.
    expect(it0.reasoned()).toBeNull()
  })

  it('falls back once on Anthropic and degrades on the second refusal', () => {
    const it0 = slot('on', 'anthropic')
    expect(it0.members()).toEqual({ thinking: { type: 'adaptive' }, output_config: { effort: 'high' } })
    const retry = it0.refused(400, 'Adaptive thinking is not supported by this model')
    expect(retry.kind).toBe('retry')
    // Silent: the desk asked in the other spelling, and the session still thinks.
    expect(it0.state()).toBe('on')
    expect(it0.members()).toEqual({
      thinking: { type: 'enabled', budget_tokens: 8000 },
      max_tokens: 8000 + RESPONSE_TOKENS
    })
    const degraded = it0.refused(400, 'Extra inputs are not permitted')
    expect(degraded.kind).toBe('degrade')
    expect((degraded as { event: AssistantEvent }).event!.type).toBe('thinking_unavailable')
    expect(it0.state()).toBe('unavailable')
    expect(it0.members()).toBeNull()
  })

  it('degrades on the first refusal where the family has no second dialect', () => {
    const it0 = slot('on')
    const degraded = it0.refused(400, 'Unsupported parameter: reasoning_effort')
    expect(degraded.kind).toBe('degrade')
    expect(it0.state()).toBe('unavailable')
  })

  it('says the degrade once, however many times it is asked', () => {
    const it0 = slot('on')
    expect((it0.refused(400, 'Unsupported parameter: reasoning_effort') as { event: unknown }).event)
      .not.toBeNull()
    const again = it0.refused(400, 'Unsupported parameter: reasoning_effort')
    // The tier is gone, so the second refusal is not about the tier at all.
    expect(again.kind).toBe('other')
  })

  it('leaves a refusal that is not about the tier to the engine', () => {
    expect(slot('on').refused(400, 'messages: at least one message is required').kind).toBe('other')
    expect(slot('on').refused(429, 'rate limited').kind).toBe('other')
  })

  it('reports unavailable when the first turn carries no reasoning at all', () => {
    const it0 = slot('on')
    const said = it0.silent()
    expect((said as { detail: string }).detail).toContain(THINKING_UNAVAILABLE)
    expect(it0.state()).toBe('unavailable')
    // And only the **first** turn: a later quiet turn is not a degrade.
    expect(slot('on').reasoned()).toBeNull()
    const two = slot('on')
    two.reasoned()
    expect(two.silent()).toBeNull()
    expect(two.state()).toBe('on')
  })

  it('says nothing about a quiet first turn when the tier is off', () => {
    const it0 = slot('off')
    expect(it0.silent()).toBeNull()
    expect(it0.state()).toBe('off')
  })

  it('degrades once on a truncated signature', () => {
    const it0 = slot('on', 'anthropic')
    const said = it0.truncated('the signature came back as a prefix of what arrived')
    expect((said as { detail: string }).detail).toContain('prefix')
    expect(it0.state()).toBe('unavailable')
    expect(it0.truncated('again')).toBeNull()
  })
})

describe('the refutation pass’s gate, and the ruling on a degraded endpoint', () => {
  it('does not run at tier off', () => {
    expect(slot('off').runsRefutation()).toBe(false)
  })

  it('runs at on and ultra', () => {
    expect(slot('on').runsRefutation()).toBe(true)
    expect(slot('ultra').runsRefutation()).toBe(true)
  })

  it('still runs on a degraded endpoint, under the default this chunk carries', () => {
    // **The open ruling.** The value of the pass is the runtime's checks over
    // the proposed document, not the model's thinking. The alternative is this
    // constant set to false and nothing else.
    expect(REFUTE_ON_A_DEGRADED_ENDPOINT).toBe(true)
    const it0 = slot('on')
    it0.refused(400, 'Unsupported parameter: reasoning_effort')
    expect(it0.state()).toBe('unavailable')
    expect(it0.runsRefutation()).toBe(REFUTE_ON_A_DEGRADED_ENDPOINT)
  })
})

describe('a signature that came back short', () => {
  it('catches a strict prefix and a strict suffix', () => {
    expect(isTruncatedSignature(['abcdef'], 'abc')).toBe(true)
    expect(isTruncatedSignature(['abcdef'], 'def')).toBe(true)
  })

  it('passes a signature carried whole', () => {
    expect(isTruncatedSignature(['abcdef'], 'abcdef')).toBe(false)
  })

  it('says nothing about a signature this desk never received', () => {
    expect(isTruncatedSignature(['abcdef'], 'zzz')).toBe(false)
    expect(isTruncatedSignature([], 'abc')).toBe(false)
    expect(isTruncatedSignature(['abcdef'], '')).toBe(false)
  })
})

describe('the line the tab shows', () => {
  it('names the tier alone where the endpoint did what was asked', () => {
    expect(thinkingLine('off', 'off')).toBe('thinking off')
    expect(thinkingLine('ultra', 'ultra')).toBe('thinking ultra')
  })

  it('names the tier and the real state where they differ', () => {
    expect(thinkingLine('off', 'always')).toContain('always thinks')
    expect(thinkingLine('on', 'unavailable')).toContain('unavailable for this endpoint')
    expect(thinkingLine('on', 'unavailable')).toContain('thinking on')
  })

  it('is read off the run’s own events', () => {
    const notice = (detail: string): AssistantEvent => ({ type: 'thinking_unavailable', detail })
    expect(stateFromEvents('on', [])).toBe('on')
    expect(stateFromEvents('on', [notice(`${THINKING_UNAVAILABLE}: the endpoint answered 400`)])).toBe(
      'unavailable'
    )
    expect(stateFromEvents('off', [notice(`${THINKING_ALWAYS}.`)])).toBe('always')
  })
})
