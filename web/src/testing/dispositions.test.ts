import { describe, expect, it } from 'vitest'
import { DISPOSITIONS, TARGETS, canonicalise } from './dispositions'

/**
 * Every disposition and target rendering these fixtures use, held to the form
 * the runtime actually emits.
 *
 * A matrix row is decided on canonical bytes. A fixture whose disposition is
 * not canonical — members out of order, `handoff` missing, a kind the evaluator
 * has no name for — is a fixture of a payload no runtime produces, and a view
 * tested against one is tested against nothing. This is what stops the next one
 * drifting.
 */
describe('the shared disposition fixtures', () => {
  const dispositions = Object.entries(DISPOSITIONS)
  const targets = Object.entries(TARGETS)

  for (const [name, text] of [...dispositions, ...targets]) {
    it(`${name} is RFC 8785 canonical`, () => {
      expect(text).toBe(canonicalise(text))
    })
  }

  it('names only kinds the evaluator has, and never omits the handoff', () => {
    // §8.3 requires `handoff` on every disposition, and the kind vocabulary is
    // closed: outcome, unresolved, not-applicable. There is no `unknown` kind,
    // which is exactly the invention this catches.
    const kinds = new Set(['outcome', 'unresolved', 'not-applicable'])
    for (const [name, text] of dispositions) {
      const parsed = JSON.parse(text) as { kind?: string; handoff?: { state?: string } }
      expect(kinds, `${name} names a kind the evaluator does not have`).toContain(parsed.kind)
      expect(parsed.handoff?.state, `${name} omits its handoff state`).toBeTruthy()
    }
  })

  it('gives every unresolved disposition the handoff it always requests', () => {
    for (const [name, text] of dispositions) {
      const parsed = JSON.parse(text) as { kind?: string; handoff?: { state?: string } }
      if (parsed.kind !== 'unresolved') continue
      expect(parsed.handoff?.state, `${name} is unresolved without a handoff`).toBe('requested')
    }
  })

  it('names what triggered every requested handoff, and nothing else', () => {
    for (const [name, text] of dispositions) {
      const parsed = JSON.parse(text) as {
        handoff?: { state?: string; triggeredBy?: string[] }
      }
      const requested = parsed.handoff?.state === 'requested'
      expect(
        (parsed.handoff?.triggeredBy?.length ?? 0) > 0,
        `${name} pairs its handoff state with the wrong triggeredBy`
      ).toBe(requested)
    }
  })

  it('catches a fixture whose members drifted out of order', () => {
    expect(canonicalise('{"kind":"outcome","handoff":{"state":"none"},"reasons":[]}')).toBe(
      '{"handoff":{"state":"none"},"kind":"outcome","reasons":[]}'
    )
  })

  it('leaves array order alone, so a wrongly ordered set is caught not hidden', () => {
    // The runtime sorts the reason and trigger sets at the source rather than
    // at serialization. Re-sorting them here would make a fixture that listed
    // them wrongly pass.
    expect(canonicalise('{"reasons":["b","a"]}')).toBe('{"reasons":["b","a"]}')
  })
})
