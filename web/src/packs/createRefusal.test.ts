/**
 * The code contract, held from both ends.
 *
 * A stable code is only worth having if the two sides agree on what the set
 * *is*. The Go constants are the authority; `CHASSIS_CODES` is a copy, and this
 * reads the Go source to prove the copy is complete — so a code added on one
 * side fails here the moment it is added, rather than the first time somebody
 * meets an unhandled refusal in a dialog.
 */
import { readFileSync } from 'node:fs'
import { join } from 'node:path'
import { describe, expect, it } from 'vitest'
import { FileRequestError, StaleWrite } from '../files/client'
import {
  CHASSIS_CODES,
  CONTROL_FLOW_CODES,
  CREATE_REFUSALS,
  OTHER_ENDPOINT_CODES,
  codeOf,
  refusalDetail,
  refusalLead
} from './createRefusal'

/** The `Code… = "…"` constants the chassis declares, read from its source. */
function goCodes(): string[] {
  const source = readFileSync(
    join(import.meta.dirname, '..', '..', '..', 'internal', 'desk', 'files.go'),
    'utf8'
  )
  return [...source.matchAll(/^\tCode[A-Za-z0-9]+ = "([a-z0-9-]+)"$/gm)].map((match) => match[1]!)
}

describe('the code set', () => {
  it('is the same set on both sides', () => {
    // Both directions. One side gaining a member is the interesting failure
    // and either side can be the one that gained it.
    expect([...CHASSIS_CODES].sort()).toEqual(goCodes().sort())
  })

  it('gives every code either a sentence or a documented reason to have none', () => {
    // Exhaustive by construction: a code is handled, or it is named as control
    // flow, or it is named as belonging to an endpoint no create calls. Three
    // categories rather than the two this started with, because the code set
    // is the *chassis'* and it grew members only the assistant probe answers.
    // What has not changed is that there is no fourth: "nobody thought about
    // it" is still what this exists to stop being one.
    const controlFlow = new Set<string>(CONTROL_FLOW_CODES)
    const elsewhere = new Set<string>(OTHER_ENDPOINT_CODES)
    for (const code of CHASSIS_CODES) {
      if (elsewhere.has(code)) {
        expect(
          CREATE_REFUSALS[code],
          `${code} belongs to another endpoint and has a create sentence`
        ).toBeUndefined()
        continue
      }
      if (controlFlow.has(code)) {
        expect(CREATE_REFUSALS[code], `${code} is control flow and has a sentence`).toBeUndefined()
        continue
      }
      expect(CREATE_REFUSALS[code], `${code} has no Create sentence`).toBeTypeOf('string')
      expect(CREATE_REFUSALS[code]!.length, `${code}'s sentence is empty`).toBeGreaterThan(0)
    }
  })

  it('names every listed exception as a code the chassis actually declares', () => {
    // A list of exceptions is only a documented gap if its members exist. A
    // stale entry here would silently exempt nothing while looking like it
    // exempted something.
    for (const code of [...CONTROL_FLOW_CODES, ...OTHER_ENDPOINT_CODES]) {
      expect(CHASSIS_CODES as readonly string[], `${code} is not a chassis code`).toContain(code)
    }
  })

  it('maps no sentence to a code the chassis does not declare', () => {
    for (const code of Object.keys(CREATE_REFUSALS)) {
      expect(CHASSIS_CODES as readonly string[], `${code} is not a chassis code`).toContain(code)
    }
  })

  it('says what happened, and never in the chassis’ words', () => {
    // Two codes tell the person what to change and are actionable on their
    // own; every other sentence has to say that nothing was created, because
    // a refusal that leaves the reader wondering whether a file appeared is
    // the thing these sentences exist to prevent.
    const actionable = new Set(['exists', 'staging-file'])
    for (const [code, sentence] of Object.entries(CREATE_REFUSALS)) {
      expect(sentence.endsWith('.'), `${code} is not a sentence`).toBe(true)
      if (!actionable.has(code)) {
        expect(sentence, `${code} does not say what happened`).toContain('Nothing was created.')
      } else {
        expect(sentence, `${code} does not say what to do`).toMatch(/try another/)
      }
      // The chassis' own words belong in the detail line, never here.
      expect(sentence, `${code} quotes an editor's advice`).not.toMatch(/override|reload it|baseSha/i)
      expect(sentence, `${code} names a tool call`).not.toMatch(/PUT |GET |api\//i)
    }
  })
})

describe('reading a refusal', () => {
  it('takes the code from either kind of failure', () => {
    expect(codeOf(new FileRequestError(404, 'gone', 'chassis', 'not-found'))).toBe('not-found')
    expect(
      codeOf(new StaleWrite({ error: 'conflict', path: 'x', exists: true, code: 'exists' }))
    ).toBe('exists')
    expect(codeOf(new TypeError('Failed to fetch'))).toBeUndefined()
    expect(codeOf('a string nobody typed')).toBeUndefined()
  })

  it('keeps the caller’s own lead for a code it has never seen', () => {
    // The fallback the contract above deliberately preserves: a newer chassis
    // can send a code this build does not know, and inventing a sentence for
    // it would be worse than the general one the caller already has.
    expect(refusalLead(new FileRequestError(500, 'x', 'chassis', 'quota-exceeded'))).toBeUndefined()
    expect(refusalLead(new TypeError('Failed to fetch'))).toBeUndefined()
  })

  it('carries the failure’s own words as the detail, whatever it is', () => {
    expect(refusalDetail(new FileRequestError(413, 'too big', 'chassis', 'too-large'))).toBe(
      'too big'
    )
    expect(refusalDetail('not an error at all')).toBe('not an error at all')
  })
})
