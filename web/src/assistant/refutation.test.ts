/**
 * The three rules ADR-0001 holds the refutation pass to, each measured here.
 *
 * The **primitive** is each engine's and its own suite measures it; what is
 * here is the part that must be the same on every engine — which answers count
 * as checks, what the verdict is computed from, and what a person is shown when
 * the critic reached nothing at all.
 */
import { describe, expect, it } from 'vitest'
import {
  CRITIC_SENTENCE,
  CRITIC_SYSTEM,
  MAX_CRITIC_TURNS,
  NO_CHECKS,
  REFUTATION_MARKER,
  SETTLED_STATUS,
  criticMessage,
  critiqueEvent,
  critiqueOnProposal,
  openCritique,
  quoteOf,
  statusOf,
  verdictOf
} from './refutation'

const valid = JSON.stringify({ status: 'valid', diagnostics: [] })
const invalid = JSON.stringify({
  status: 'invalid',
  diagnostics: [{ code: 'JPS-SEMANTIC-UNRESOLVED-OUTCOME' }]
})
const evaluated = JSON.stringify({ status: 'evaluated', rehearsal: true })

describe('what counts as a check', () => {
  it('is the two tools that say something about the document', () => {
    // A question *about the runtime* — the schema, the examples — is not
    // evidence about a document, so a critic that only asked those has run no
    // check at all.
    expect(Object.keys(SETTLED_STATUS).sort()).toEqual(['experimental_evaluate', 'validate'])
  })

  it('names the runtime’s own word for each, and they are not the same word', () => {
    // **The reason this is a table and not the constant `'valid'`.** A rehearsal
    // evaluation answers `"status": "evaluated"`, so one word over both tools
    // would report every session ever run as refuted.
    expect(SETTLED_STATUS.validate).toBe('valid')
    expect(SETTLED_STATUS.experimental_evaluate).toBe('evaluated')
  })

  it('reads the runtime’s status and diagnostic count, or nothing', () => {
    expect(statusOf(invalid)).toEqual({ status: 'invalid', diagnostics: 1 })
    expect(statusOf(valid)).toEqual({ status: 'valid', diagnostics: 0 })
    expect(statusOf('not json')).toBeNull()
    expect(statusOf('{"diagnostics":[]}')).toBeNull()
  })

  it('collects only the answers that are about the document', () => {
    const recorder = openCritique()
    recorder.saw('get_schema', JSON.stringify({ status: 'ok' }), false)
    recorder.saw('list_examples', JSON.stringify({ status: 'ok' }), false)
    recorder.saw('validate', valid, false)
    expect(recorder.critique('').checks).toEqual([
      { tool: 'validate', status: 'valid', diagnostics: 0 }
    ])
  })

  it('counts a refused check as a check, and as a refutation', () => {
    // A runtime that could not read the document has not approved it.
    const recorder = openCritique()
    recorder.saw('validate', 'refused: the gate did not let that through', true)
    const critique = recorder.critique('')
    expect(critique.checks).toEqual([{ tool: 'validate', status: 'refused', diagnostics: 0 }])
    expect(critique.refuted).toBe(true)
  })
})

describe('the verdict, and where it does not come from', () => {
  it('is false where every check came back as the runtime’s own settled word', () => {
    expect(
      verdictOf([
        { tool: 'validate', status: 'valid', diagnostics: 0 },
        { tool: 'experimental_evaluate', status: 'evaluated', diagnostics: 0 }
      ])
    ).toBe(false)
  })

  it('is true where any check did not', () => {
    expect(
      verdictOf([
        { tool: 'validate', status: 'invalid', diagnostics: 1 },
        { tool: 'experimental_evaluate', status: 'evaluated', diagnostics: 0 }
      ])
    ).toBe(true)
  })

  it('is false where the critic reached no check at all', () => {
    // ADR-0001's first rule, at the verdict: with nothing checked there is
    // nothing refuted **and** nothing to say "not refuted" about.
    expect(verdictOf([])).toBe(false)
  })

  it('ignores the critic’s prose in both directions', () => {
    // **The rule this is here for.** The prose is the model's and the verdict
    // is the runtime's, and neither is allowed to be the other.
    const said = openCritique()
    said.saw('validate', valid, false)
    expect(said.critique('REFUTATION: this pack is broken and must not be used.').refuted).toBe(
      false
    )
    const other = openCritique()
    other.saw('validate', invalid, false)
    expect(other.critique('REFUTATION: none found. Everything checks out.').refuted).toBe(true)
  })

  it('keeps the critic’s words beside the verdict rather than in it', () => {
    const recorder = openCritique()
    recorder.saw('validate', valid, false)
    const critique = recorder.critique('I looked hard and found nothing.')
    expect(critique.modelText).toBe('I looked hard and found nothing.')
    expect(critique.text).not.toContain('I looked hard')
    expect(critique.text).toContain('"status": "valid"')
  })
})

describe('what a person is shown', () => {
  it('quotes the runtime, tool by tool', () => {
    expect(
      quoteOf([
        { tool: 'validate', status: 'invalid', diagnostics: 2 },
        { tool: 'experimental_evaluate', status: 'evaluated', diagnostics: 0 }
      ])
    ).toBe(
      'quoted from the runtime: validate → "status": "invalid", 2 diagnostic(s); ' +
        'experimental_evaluate → "status": "evaluated", 0 diagnostic(s)'
    )
  })

  it('says the critic ran no check rather than "not refuted"', () => {
    expect(quoteOf([])).toBe(NO_CHECKS)
    expect(NO_CHECKS).toContain('no runtime check')
    expect(NO_CHECKS).not.toContain('not refuted')
  })

  it('puts no refutation line on a proposal the critic did not check', () => {
    // ADR-0001's first rule, at the other end: the proposal is still shown —
    // the critic reaching nothing is not a reason to withhold a document — and
    // it is shown **without** a claim nobody measured.
    const nothing = openCritique().critique('I had a look.')
    expect(critiqueOnProposal(nothing)).toEqual({})
    expect(critiqueOnProposal(null)).toEqual({})
    const checked = openCritique()
    checked.saw('validate', invalid, false)
    expect(critiqueOnProposal(checked.critique(''))).toEqual({ critique: { refuted: true } })
  })

  it('carries the verdict, the checks and the runtime’s words on the event', () => {
    const recorder = openCritique()
    recorder.saw('validate', invalid, false)
    recorder.saw('experimental_evaluate', evaluated, false)
    expect(critiqueEvent(recorder.critique('my prose'))).toEqual({
      type: 'critique',
      refuted: true,
      checks: [
        { tool: 'validate', status: 'invalid' },
        { tool: 'experimental_evaluate', status: 'evaluated' }
      ],
      text: expect.stringContaining('quoted from the runtime')
    })
  })
})

describe('what the critic is told', () => {
  it('carries the marker in both the system text and the first message', () => {
    expect(CRITIC_SYSTEM).toContain(REFUTATION_MARKER)
    expect(CRITIC_SENTENCE).toContain(REFUTATION_MARKER)
  })

  it('is the runtime’s guidance, the desk’s one sentence, and the document fenced', () => {
    const message = criticMessage('THE RUNTIME’S GUIDANCE', { id: 'a-pack' })
    expect(message.indexOf('THE RUNTIME’S GUIDANCE')).toBe(0)
    expect(message).toContain(CRITIC_SENTENCE)
    expect(message).toContain('```json')
    expect(message).toContain('"id": "a-pack"')
    // The document comes after the instructions, fenced, which is the shape the
    // runtime's own prompts use for caller-supplied material.
    expect(message.indexOf('```json')).toBeGreaterThan(message.indexOf(CRITIC_SENTENCE))
  })

  it('works from the desk’s one sentence alone where the runtime advertises none', () => {
    const message = criticMessage('', { id: 'a-pack' })
    expect(message.startsWith(CRITIC_SENTENCE)).toBe(true)
  })

  it('is bounded: a pass is a read over a document, not a second session', () => {
    expect(MAX_CRITIC_TURNS).toBe(4)
  })
})
