/**
 * The shared desk-configuration fixtures, read by this decoder.
 *
 * **The same directory is read by `internal/desk/deskfile_test.go`.** There
 * are two implementations of one contract — the browser's, which decides what
 * Admin shows, and the chassis', which decides whether a credential leaves
 * this machine — and two implementations of one rule drift. They drifted
 * once, and the way they drifted is the reason this directory exists: the
 * chassis read only `assistant.endpoint`, so a file the browser refused whole
 * (a stray `apiKey`, a missing `tools`, a whitespace model) still authorised
 * an outbound request carrying the stored key.
 *
 * So the fixtures are the contract. Each file is accepted or refused, and each
 * refusal names its keys; a rule changed on one side and not the other fails
 * on both. `expected.json` is the only place the verdicts are written down.
 */
import { readFileSync, readdirSync } from 'node:fs'
import { join } from 'node:path'
import { describe, expect, it } from 'vitest'
import { decodeDeskConfig } from './deskConfig'

const FIXTURES = join(import.meta.dirname, 'fixtures', 'desk-config')

interface Verdict {
  accepted: boolean
  keys: string[]
}

const expected = JSON.parse(
  readFileSync(join(FIXTURES, 'expected.json'), 'utf8')
) as Record<string, Verdict>

function fixtureNames(): string[] {
  return readdirSync(FIXTURES)
    .filter((name) => name.endsWith('.json') && name !== 'expected.json')
    .map((name) => name.replace(/\.json$/, ''))
    .sort()
}

describe('the shared desk-configuration fixtures', () => {
  it('has a verdict for every fixture, and a fixture for every verdict', () => {
    // A verdict file that has drifted from the directory is a suite that
    // silently stops checking a case. Both directions, so neither an
    // unjudged fixture nor a verdict about a file nobody wrote survives.
    expect(fixtureNames()).toEqual(Object.keys(expected).sort())
  })

  for (const name of fixtureNames()) {
    it(`decodes ${name} as the shared verdict says`, () => {
      const verdict = expected[name]!
      const decoded = decodeDeskConfig(
        readFileSync(join(FIXTURES, `${name}.json`), 'utf8'),
        'desk'
      )
      if (verdict.accepted) {
        expect(decoded.problems, `${name} was refused`).toEqual([])
        expect(decoded.values, `${name} produced no values`).toBeDefined()
        return
      }
      expect(decoded.values, `${name} was accepted`).toBeUndefined()
      // The keys are asserted as a set rather than in order: the two decoders
      // walk the document differently, and requiring one order would be a
      // contract about traversal that neither side promises.
      expect([...new Set(decoded.problems.map((problem) => problem.key))].sort()).toEqual(
        [...verdict.keys].sort()
      )
    })
  }

  it('names a key wherever it is written, with the sentence about keys', () => {
    // The four fixtures that exist because the rule used to stop at the
    // schema's own objects: a key inside an object this schema has never
    // heard of, inside an array, and four levels down.
    for (const name of [
      'refused-top-level-key',
      'refused-key-in-endpoint',
      'refused-key-in-unknown-object',
      'refused-key-in-array',
      'refused-key-deeply-nested'
    ]) {
      const decoded = decodeDeskConfig(
        readFileSync(join(FIXTURES, `${name}.json`), 'utf8'),
        'desk'
      )
      const said = decoded.problems.filter((problem) =>
        problem.reason.includes('never stored in configuration')
      )
      expect(said.length, `${name} never says a key is what is wrong`).toBeGreaterThan(0)
    }
  })
})
