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
import { DESK_DEFAULTS, decodeDeskConfig } from './deskConfig'

const FIXTURES = join(import.meta.dirname, 'fixtures', 'desk-config')

interface Verdict {
  accepted: boolean
  keys: string[]
  /**
   * The decoded values an accepted file yields, the defaults included.
   *
   * **Present on every accepted verdict**, so that this corpus proves decoding
   * parity and not only acceptance parity: the two decoders could otherwise
   * agree that `{"engine":"builtin"}` is legal and disagree about what it
   * decoded to, and nothing here would say so.
   */
  engine?: string
  thinking?: string
  /**
   * `project.file` as the file decodes to, absent where it names none. On the
   * same terms as `engine` and `thinking`: the corpus proves what a file
   * *means* on both sides and not only whether it is legal.
   */
  projectFile?: string
  /**
   * What the decoder **did** with a member it accepted, where it did anything.
   *
   * Compared-if-present rather than required, unlike `engine` and `thinking`,
   * and the difference is that an omission here still checks: the absent value
   * is the empty list, so a fixture that starts producing a notice and does not
   * declare one fails. A required field would only add `"notices": []` to
   * twenty-odd verdicts that say nothing.
   */
  notices?: { key: string; says: string }[]
  /**
   * The endpoint's enabled set and its default, as the file decodes to them.
   *
   * Compared-if-present on the same terms as `notices`, and the omission still
   * checks: the absent values are the empty set and no default, so a fixture
   * that starts decoding to a set and does not declare one fails. This is where
   * the migration is stated — a file naming a model and no set decodes to the
   * set of that one id, on both sides — and where a default is proved to be
   * held to the set beside it.
   */
  models?: string[]
  model?: string
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
        // The values, not only the verdict. Required rather than
        // compared-if-present: an omitted pair would exempt a fixture from the
        // parity this exists to hold.
        expect(verdict.engine, `${name} has no expected engine`).toBeTypeOf('string')
        expect(verdict.thinking, `${name} has no expected thinking`).toBeTypeOf('string')
        const assistant = { ...DESK_DEFAULTS.assistant, ...(decoded.values?.assistant ?? {}) }
        expect(assistant.engine, `${name}: engine`).toBe(verdict.engine)
        expect(assistant.thinking, `${name}: thinking`).toBe(verdict.thinking)
        const project = { ...DESK_DEFAULTS.project, ...(decoded.values?.project ?? {}) }
        expect(project.file ?? '', `${name}: project.file`).toBe(verdict.projectFile ?? '')
        // The migrations, in the decoder's own words. A sentence changed on
        // one side of the shared decoder and not the other fails on both.
        expect(decoded.notices, `${name}: notices`).toEqual(verdict.notices ?? [])
        // The set and its default, which is where the migration is stated.
        const endpoint = (decoded.values?.assistant ?? DESK_DEFAULTS.assistant).endpoint
        expect(endpoint?.models ?? [], `${name}: models`).toEqual(verdict.models ?? [])
        expect(endpoint?.model ?? '', `${name}: model`).toBe(verdict.model ?? '')
        return
      }
      expect(decoded.values, `${name} was accepted`).toBeUndefined()
      // A refused file shows nothing: it decoded to nothing, so it did nothing.
      expect(decoded.notices, `${name}: a refused file carries a notice`).toEqual([])
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
