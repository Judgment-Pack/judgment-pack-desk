import { describe, expect, it } from 'vitest'
import { buildEvaluateArguments } from './queries'

const SAVED = { source: 'pack_id', packId: 'intake' } as const
const DRAFT = { source: 'pack', pack: '{"specVersion":"0.2.0-draft"}' } as const

describe('buildEvaluateArguments', () => {
  it('declares a rehearsal exactly when the runtime advertises the argument', () => {
    const input = { ...SAVED, facts: '{}' }
    expect(buildEvaluateArguments(input, true)).toEqual({
      pack_id: 'intake',
      facts: '{}',
      rehearsal: true
    })
    // An older runtime never receives the member at all: an unknown key is a
    // refused call there, and a false would still be a spelled declaration.
    expect(buildEvaluateArguments(input, false)).toEqual({ pack_id: 'intake', facts: '{}' })
  })

  it('expresses evidence absence as the omitted key, never an empty value', () => {
    expect(buildEvaluateArguments({ ...SAVED, facts: '{}' }, true)).not.toHaveProperty('evidence')
    expect(
      buildEvaluateArguments({ ...SAVED, facts: '{}', evidence: '{"e":"present"}' }, true)
    ).toMatchObject({ evidence: '{"e":"present"}' })
  })

  /**
   * The tool's `required` list is `["facts"]` alone and the exactly-one-of rule
   * is enforced by the handler by hand: **both are refused, and neither is
   * refused**. So a call that sent the two, or sent neither, would be rejected
   * on an argument mistake rather than on anything about the pack — which
   * reads to an author mid-edit exactly like the runtime refusing their draft.
   */
  it('sends the draft as pack, and never a pack_id beside it', () => {
    const args = buildEvaluateArguments({ ...DRAFT, facts: '{}' }, true)
    expect(args.pack).toBe(DRAFT.pack)
    expect(args).not.toHaveProperty('pack_id')
  })

  it('sends the saved pack as pack_id, and never a pack beside it', () => {
    const args = buildEvaluateArguments({ ...SAVED, facts: '{}' }, true)
    expect(args.pack_id).toBe('intake')
    expect(args).not.toHaveProperty('pack')
  })

  it('always sends exactly one of the two', () => {
    for (const source of [SAVED, DRAFT]) {
      for (const advertised of [true, false]) {
        const args = buildEvaluateArguments({ ...source, facts: '{}' }, advertised)
        const named = ['pack', 'pack_id'].filter((key) => key in args)
        expect(named).toHaveLength(1)
      }
    }
  })

  it('declares the rehearsal on a draft run too, where it is advertised', () => {
    // The draft is where it matters most: `auditWriter` runs for every call
    // including a text pack, and only the declaration suppresses the record.
    expect(buildEvaluateArguments({ ...DRAFT, facts: '{}' }, true).rehearsal).toBe(true)
    expect(buildEvaluateArguments({ ...DRAFT, facts: '{}' }, false)).not.toHaveProperty('rehearsal')
  })
})
