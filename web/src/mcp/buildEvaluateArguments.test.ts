import { describe, expect, it } from 'vitest'
import { buildEvaluateArguments } from './queries'

describe('buildEvaluateArguments', () => {
  it('declares a rehearsal exactly when the runtime advertises the argument', () => {
    const input = { packId: 'intake', facts: '{}' }
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
    expect(buildEvaluateArguments({ packId: 'p', facts: '{}' }, true)).not.toHaveProperty('evidence')
    expect(
      buildEvaluateArguments({ packId: 'p', facts: '{}', evidence: '{"e":"present"}' }, true)
    ).toMatchObject({ evidence: '{"e":"present"}' })
  })
})
