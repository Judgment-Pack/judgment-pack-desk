/**
 * What one card's Save composes, and what it refuses to compose.
 *
 * Four claims, and each of them is a way the obvious implementation is wrong:
 * one member's bytes move and no others do; a field nobody edited declares
 * nothing; a value this desk would refuse to read is never composed into a
 * request; and a file two readers would disagree about is not spliced at all.
 *
 * The fixture is deliberately not what a formatter would emit — aligned
 * colons, a member on one line, four-space indentation — because a claim about
 * bytes tested against bytes a serialiser happens to produce is a claim about
 * nothing.
 */
import { readFileSync } from 'node:fs'
import { join } from 'node:path'
import { describe, expect, it } from 'vitest'
import { KEYS_ARE_NEVER_IN_CONFIGURATION } from '../config/deskConfig'
import { buffered, bytesAt } from '../packs/edit/writes'
import { CARD_POINTERS, composeProjectFile } from './useProjectFileSave'

const FILE = `{
    "deskConfigVersion": 1,
    "organization": { "name": "Unveil", "mark": null },
    "user": { "displayName": "local user" },
    "appearance": { "theme": "system", "density": "comfortable" },
    "panes": {
      "left":      { "mode": "expanded", "width": 2.48e2 }
    },
    "storage": { "packs": { "dir": "packs", "idBase": "https://acme.example/d/" } }
}
`

/** Every top-level member's own bytes, by pointer. */
function members(text: string): Record<string, string | undefined> {
  const current = buffered(text)
  const seen: Record<string, string | undefined> = {}
  for (const name of Object.keys(JSON.parse(text) as Record<string, unknown>)) {
    seen[`/${name}`] = bytesAt(current, `/${name}`)
  }
  return seen
}

describe('composing one member of the project file', () => {
  it('leaves every other member’s bytes exactly as they were', () => {
    const before = members(FILE)
    const composed = composeProjectFile(FILE, '/organization', [
      { path: ['name'], value: 'Renamed' }
    ])
    expect(composed.problems).toEqual([])
    const after = members(composed.text!)
    // Every member but the one written, byte for byte — including the aligned
    // colons inside `panes` and the `2.48e2` a re-serialisation would turn
    // into `248`.
    for (const pointer of Object.keys(before)) {
      if (pointer === '/organization') continue
      expect(after[pointer], pointer).toBe(before[pointer])
    }
    expect(after['/organization']).not.toBe(before['/organization'])
    expect(composed.text).toContain('"width": 2.48e2')
    // And the file outside the member is untouched down to the newline it ends
    // with: a rewrite would take that too.
    expect(composed.text!.endsWith('}\n')).toBe(true)
  })

  it('writes a one-line member back on one line', () => {
    // The file writes `appearance` on one line, so a one-word change is one
    // line of diff. Expanding it would turn every edit into four.
    const composed = composeProjectFile(FILE, '/appearance', [
      { path: ['theme'], value: 'dark' }
    ])
    expect(composed.text).toContain(
      '    "appearance": {"theme":"dark","density":"comfortable"},'
    )
  })

  it('writes a member the file lays out over several lines the same way', () => {
    // And at the indentation the file gives it — four spaces here, not two.
    const composed = composeProjectFile(FILE, '/panes', [
      { path: ['left', 'width'], value: 300 }
    ])
    expect(composed.text).toContain(
      '    "panes": {\n      "left": {\n        "mode": "expanded",'
    )
    expect(composed.text).toContain('\n        "width": 300\n      }\n    }')
  })

  it('adds a member the file omits rather than dropping the edit', () => {
    const bare = '{\n  "deskConfigVersion": 1\n}\n'
    const composed = composeProjectFile(bare, '/organization', [
      { path: ['name'], value: 'Added' }
    ])
    expect(composed.problems).toEqual([])
    expect(JSON.parse(composed.text!)).toEqual({
      deskConfigVersion: 1,
      organization: { name: 'Added' }
    })
    // The version member is still its own bytes, in its own place.
    expect(composed.text!.startsWith('{\n  "deskConfigVersion": 1,')).toBe(true)
  })

  it('declares nothing the reader did not edit', () => {
    // The file states the rail's width and nothing else about the panes, which
    // is what `DeclaredPanes` reads: the Inspector's drawer has its own
    // baseline while its width is undeclared. Composing the whole member from
    // the effective configuration would declare two dimensions nobody wrote.
    const composed = composeProjectFile(FILE, '/panes', [
      { path: ['left', 'width'], value: 300 }
    ])
    expect(composed.problems).toEqual([])
    expect((JSON.parse(composed.text!) as { panes: unknown }).panes).toEqual({
      left: { mode: 'expanded', width: 300 }
    })
  })

  it('carries no member the edits did not name', () => {
    // The value is composed by laying named paths over the file's own member,
    // so a member nobody asked for cannot reach the request — which is what
    // makes the decoder's credential rule the second line and not the first.
    const composed = composeProjectFile(FILE, '/storage', [
      { path: ['packs', 'idBase'], value: 'https://acme.example/other' }
    ])
    expect((JSON.parse(composed.text!) as { storage: unknown }).storage).toEqual({
      packs: { dir: 'packs', idBase: 'https://acme.example/other' }
    })
  })

  it('refuses a credential-shaped member by name, and composes nothing', () => {
    const composed = composeProjectFile(FILE, '/storage', [
      { path: ['packs', 'apiKey'], value: 'sk-live-secret' }
    ])
    expect(composed.text).toBeUndefined()
    expect(composed.problems).toContainEqual({
      key: 'storage.packs.apiKey',
      reason: KEYS_ARE_NEVER_IN_CONFIGURATION
    })
  })

  it('refuses a value the decoder refuses, in the decoder’s own words', () => {
    const composed = composeProjectFile(FILE, '/panes', [
      { path: ['left', 'width'], value: 20000 }
    ])
    expect(composed.text).toBeUndefined()
    expect(composed.problems).toEqual([
      {
        key: 'panes.left.width',
        reason: 'must be between 160 and 640 pixels inclusive; found 20000'
      }
    ])
  })

  it('refuses a file whose member is written twice, rather than splicing one of them', () => {
    // `JSON.parse` keeps the last and this desk's scanner keeps the first, so a
    // save would decode one value and overwrite another.
    const twice = '{\n  "deskConfigVersion": 1,\n  "organization": {},\n  "organization": {}\n}\n'
    const composed = composeProjectFile(twice, '/organization', [
      { path: ['name'], value: 'Renamed' }
    ])
    expect(composed.text).toBeUndefined()
    expect(composed.problems).toEqual([
      { key: 'organization', reason: 'the member "organization" appears more than once' }
    ])
  })
})

/**
 * The README's count of the members a card may write, read out of the source.
 *
 * **A prose number is a claim, and a claim with no holder goes stale.** The
 * README said three long after the list held three: `panes` left with the Panes
 * card and `appearance` left with the Appearance card, and the sentence
 * describing the closed list went on naming a length the list no longer had.
 * The round-1 review found it by reading, which is the expensive way.
 *
 * The count is spelled in the README as a numeral so this can find it without a
 * word-to-number table nobody wants to maintain, and the assertion is against
 * `CARD_POINTERS.length` rather than against a literal here — a test that
 * compared the README with its own copy of the number would be a comparison
 * against itself.
 */
describe('the README’s account of the closed write list', () => {
  const README = readFileSync(join(import.meta.dirname, '..', '..', '..', 'README.md'), 'utf8')

  it('states the number of members a card may write, and states this one', () => {
    const stated = README.match(/`CARD_POINTERS` names the \*\*(\d+)\*\* members a card may write/)
    expect(stated, 'the README no longer states a CARD_POINTERS count in the shape this reads').not
      .toBeNull()
    expect(Number(stated![1])).toBe(CARD_POINTERS.length)
  })

  it('names each of them, so the count is not the only thing that has to be right', () => {
    for (const pointer of CARD_POINTERS) {
      expect(README, pointer).toContain(`\`${pointer}\``)
    }
    // And names no member a card cannot write, in that same sentence.
    const sentence = README.slice(README.indexOf('`CARD_POINTERS` names'))
    expect(sentence.slice(0, 160)).not.toContain('/appearance')
    expect(sentence.slice(0, 160)).not.toContain('/panes')
  })
})
