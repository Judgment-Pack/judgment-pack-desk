import { afterEach, describe, expect, it } from 'vitest'
import {
  divergentPairIdentity,
  forgetDivergentPairs,
  recordDivergentPair
} from './refetchLedger'

afterEach(forgetDivergentPairs)

const A = 'a'.repeat(64)
const B = 'b'.repeat(64)
const C = 'c'.repeat(64)

const id = (run: string | undefined, served: string | undefined, graphId = 'onboarding') =>
  divergentPairIdentity({ graphId, run, served })

describe('divergentPairIdentity', () => {
  it('reads one pair spelled two ways as one identity', () => {
    // The comparison folds case and whitespace, so an identity that did not
    // would file two spellings of one disagreement separately and ask about it
    // twice — the alternating bug, reached between two spellings.
    expect(id(` ${B.toUpperCase()} `, A)).toBe(id(B, A))
  })

  it('keeps two different pairs apart', () => {
    expect(id(B, A)).not.toBe(id(C, A))
    expect(id(B, A)).not.toBe(id(A, B))
  })

  it('keeps two graphs apart', () => {
    // Two configured graphs may disagree independently, and one settling must
    // not silence the other.
    expect(id(B, A, 'onboarding')).not.toBe(id(B, A, 'renewal'))
  })

  it('does not let a graph id run into a digest', () => {
    // A configured id is arbitrary text. Joining on a separator would let an id
    // ending in one collide with a different id and digest.
    expect(id(B, A, 'x')).not.toBe(id(B, A, 'x","' + B))
  })

  it('reads an absent digest as absent rather than as the empty digest', () => {
    expect(id(undefined, A)).toBe(id('', A))
    expect(id(undefined, A)).not.toBe(id(B, A))
  })
})

describe('recordDivergentPair', () => {
  it('records one pair exactly once', () => {
    expect(recordDivergentPair(1, id(B, A))).toBe(true)
    expect(recordDivergentPair(1, id(B, A))).toBe(false)
  })

  it('records a second, different pair', () => {
    expect(recordDivergentPair(1, id(B, A))).toBe(true)
    expect(recordDivergentPair(1, id(C, A))).toBe(true)
  })

  it('remembers a pair two other pairs ago', () => {
    // The failure this exists for: a file edited back and forth lands B/A, then
    // C/A, then B/A again. A memory holding only the pair asked about last
    // reads the third as new and asks forever.
    expect(recordDivergentPair(1, id(B, A))).toBe(true)
    expect(recordDivergentPair(1, id(C, A))).toBe(true)
    expect(recordDivergentPair(1, id(B, A))).toBe(false)
    expect(recordDivergentPair(1, id(C, A))).toBe(false)
    expect(recordDivergentPair(1, id(B, A))).toBe(false)
  })

  it('asks again after a reconnect, and forgets the connection before it', () => {
    // A new epoch gives every document query a new cache identity, so the
    // answers after one are new answers and deserve one cycle of their own.
    expect(recordDivergentPair(1, id(B, A))).toBe(true)
    expect(recordDivergentPair(2, id(B, A))).toBe(true)
    expect(recordDivergentPair(2, id(B, A))).toBe(false)
  })

  it('keeps no memory of a retired epoch', () => {
    // Pruning is what stops the map growing across a session of reconnects: an
    // epoch that has been left behind can never answer again, so its pairs are
    // dropped rather than kept. Going back to epoch 1 therefore records afresh.
    expect(recordDivergentPair(1, id(B, A))).toBe(true)
    expect(recordDivergentPair(2, id(C, A))).toBe(true)
    expect(recordDivergentPair(1, id(B, A))).toBe(true)
  })
})
