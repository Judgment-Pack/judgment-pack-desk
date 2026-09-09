/**
 * The appearance record, and the ladder above it.
 *
 * Four properties, and each is one this store gets wrong if it is written the
 * obvious way: the preference wins where it is set and only there, a value
 * outside the decoder's unions is absent rather than applied, every access
 * survives a storage that throws on the accessor itself, and the reset takes
 * exactly one key and only a record this desk wrote.
 */
import { afterEach, describe, expect, it, vi } from 'vitest'
import { DESK_DEFAULTS } from '../config/deskConfig'
import {
  appearanceKey,
  effectiveAppearance,
  readAppearance,
  resetAppearance,
  writeAppearance
} from './appearanceState'
import { projectKey } from './paneState'

afterEach(() => {
  window.localStorage.clear()
  vi.unstubAllGlobals()
})

const ROOT = '/home/someone/a-project'
const KEY = appearanceKey(projectKey(ROOT))
const FILE = { theme: 'light', density: 'compact' } as const

describe('the appearance ladder', () => {
  it('prefers what this viewer chose over what the project file says', () => {
    expect(effectiveAppearance({ theme: 'dark' }, FILE)).toEqual({
      theme: 'dark',
      // Unset here, so the file still answers for it — a preference is per
      // member and not a wholesale replacement of the project's appearance.
      density: 'compact'
    })
  })

  it('falls back to the project file where this viewer has chosen nothing', () => {
    expect(effectiveAppearance(undefined, FILE)).toEqual(FILE)
    expect(effectiveAppearance({}, FILE)).toEqual(FILE)
  })

  it('falls back to the schema’s own default where the file says nothing either', () => {
    // The decoder has already filled an absent member in with `DESK_DEFAULTS`,
    // which is why the third rung is one value by the time this is reached.
    expect(effectiveAppearance({}, DESK_DEFAULTS.appearance)).toEqual({
      theme: 'system',
      density: 'comfortable'
    })
  })
})

describe('the appearance record', () => {
  it('is keyed on the whole project root, under its own prefix', () => {
    // The pane record's grammar: percent-encoding is injective, so two roots
    // are two keys by construction rather than with probability. A different
    // prefix, because these are two records and a reset of one is not a reset
    // of the other.
    expect(KEY).toBe('jpack-desk:appearance:v1:%2Fhome%2Fsomeone%2Fa-project')
    expect(KEY.startsWith('jpack-desk:shell:')).toBe(false)
  })

  it('writes only the members the viewer chose, over the ones they chose before', () => {
    writeAppearance(KEY, { theme: 'dark' })
    expect(JSON.parse(window.localStorage.getItem(KEY)!)).toEqual({ v: 1, theme: 'dark' })
    // A density chosen later must not take the theme with it, and must not
    // drop it either.
    writeAppearance(KEY, { density: 'compact' })
    expect(JSON.parse(window.localStorage.getItem(KEY)!)).toEqual({
      v: 1,
      theme: 'dark',
      density: 'compact'
    })
  })

  it('reads back what it wrote', () => {
    writeAppearance(KEY, { theme: 'light', density: 'compact' })
    expect(readAppearance(KEY)).toEqual({ theme: 'light', density: 'compact' })
  })

  it('treats a value outside the decoder’s unions as bytes it did not write', () => {
    // Strictness about a *file* belongs to the decoder, which refuses a typo'd
    // theme by name and says which key. Here the question is narrower and
    // harder: could this writer have produced these bytes? It could not have
    // written `"midnight"`, so the record is not this desk's — the project's
    // default applies, and the bytes are left alone.
    window.localStorage.setItem(KEY, JSON.stringify({ v: 1, theme: 'midnight', density: 'roomy' }))
    expect(readAppearance(KEY)).toBeUndefined()
    expect(effectiveAppearance(readAppearance(KEY), FILE)).toEqual(FILE)
  })

  it('disowns the whole record when one member is outside the union', () => {
    // Not "keep the half I can read". A record is a set of bytes with one
    // writer, and a member this writer could not have written says the writer
    // was not this one — whatever the member beside it happens to say.
    window.localStorage.setItem(KEY, JSON.stringify({ v: 1, theme: 'dark', density: 'roomy' }))
    expect(readAppearance(KEY)).toBeUndefined()
  })

  it('discards bytes that are not a record of this version at all', () => {
    for (const raw of ['not json at all', '[]', 'null', '"dark"', '{"v":2,"theme":"dark"}']) {
      window.localStorage.setItem(KEY, raw)
      expect(readAppearance(KEY), raw).toBeUndefined()
    }
  })

  /**
   * **Ownership is the whole member set, not the version number.**
   *
   * `localStorage` is one namespace shared with everything this origin has ever
   * served, and the key is derived from a path the viewer never chose. A record
   * carrying a member this writer never writes is somebody else's value under a
   * name this desk merely computed — and reading `v === 1` alone applied it to
   * the page and let "Use the project's default" delete it.
   *
   * A bare `{"v":1}` is the same argument from the other side: this writer
   * produces a record because somebody chose something, so a record with
   * nothing chosen in it is not one of ours either.
   */
  it.each([
    ['a member this writer never writes', { v: 1, writer: 'another-app', theme: 'dark' }],
    ['an unknown member beside nothing else', { v: 1, mode: 'compact' }],
    ['no chosen member at all', { v: 1 }]
  ])('discards a v1 record carrying %s', (_what, record) => {
    window.localStorage.setItem(KEY, JSON.stringify(record))
    expect(readAppearance(KEY)).toBeUndefined()
    // Not applied — and not deleted either: it is left exactly where it is,
    // named as somebody else's.
    expect(resetAppearance(KEY)).toBe('foreign')
    expect(JSON.parse(window.localStorage.getItem(KEY)!)).toEqual(record)
  })

  /**
   * **A right-named member with an impossible value is not this writer's.**
   *
   * The round after the one that narrowed ownership to the member *names*
   * found what that still admitted: `{"v":1,"theme":17}` was owned, read as a
   * record with nothing usable in it, and deleted by "Use the project's
   * default" — a control deleting somebody else's bytes under a key this desk
   * merely computed, which is the exact thing the foreign rule exists to stop.
   */
  it.each([
    ['a theme that is a number', { v: 1, theme: 17 }],
    ['a theme that is an object', { v: 1, theme: { name: 'dark' } }],
    ['a theme that is null', { v: 1, theme: null }],
    ['a theme outside its union', { v: 1, theme: 'midnight' }],
    ['a density that is a number', { v: 1, density: 0 }],
    ['a density that is a boolean', { v: 1, density: true }],
    ['a density outside its union', { v: 1, density: 'roomy' }],
    ['one member good and one impossible', { v: 1, theme: 'dark', density: 42 }]
  ])('disowns %s, and leaves the bytes exactly as they are', (_what, record) => {
    const bytes = JSON.stringify(record)
    window.localStorage.setItem(KEY, bytes)
    expect(readAppearance(KEY)).toBeUndefined()
    expect(resetAppearance(KEY)).toBe('foreign')
    expect(window.localStorage.getItem(KEY)).toBe(bytes)
  })

  it('never writes a record its own reader would disown', () => {
    // A writer able to emit bytes its reader calls foreign is the shape of the
    // defect above. Nothing reaches this with both members empty — a chosen
    // member always carries a value — so the absence of a preference is the
    // absence of a record.
    writeAppearance(KEY, {})
    expect(window.localStorage.getItem(KEY)).toBeNull()
  })

  it('survives a storage that throws on the accessor itself', () => {
    // A private window and a browser set to block site data throw rather than
    // answering null, and a thrown accessor must still leave a working desk.
    vi.stubGlobal('localStorage', {
      getItem: () => {
        throw new Error('site data is blocked')
      },
      setItem: () => {
        throw new Error('site data is blocked')
      },
      removeItem: () => {
        throw new Error('site data is blocked')
      },
      clear: () => {}
    })
    expect(readAppearance(KEY)).toBeUndefined()
    expect(() => writeAppearance(KEY, { theme: 'dark' })).not.toThrow()
    expect(resetAppearance(KEY)).toBe('refused')
  })
})

describe('forgetting this browser’s appearance', () => {
  it('removes exactly one key', () => {
    // `localStorage.clear()` would take the session token and every other
    // project's record with it.
    writeAppearance(KEY, { theme: 'dark' })
    window.localStorage.setItem('jpack-desk:appearance:v1:another', '{"v":1}')
    window.localStorage.setItem('jpack-desk-unrelated', 'a value')
    expect(resetAppearance(KEY)).toBe('cleared')
    expect(window.localStorage.getItem(KEY)).toBeNull()
    expect(window.localStorage.getItem('jpack-desk:appearance:v1:another')).toBe('{"v":1}')
    expect(window.localStorage.getItem('jpack-desk-unrelated')).toBe('a value')
  })

  it('leaves the pane record where it is', () => {
    // Two records, two keys, two controls. A reset of one that took the other
    // would be a control lying about its scope.
    writeAppearance(KEY, { theme: 'dark' })
    window.localStorage.setItem('jpack-desk:shell:v1:%2Fhome%2Fsomeone%2Fa-project', '{"v":1}')
    expect(resetAppearance(KEY)).toBe('cleared')
    expect(
      window.localStorage.getItem('jpack-desk:shell:v1:%2Fhome%2Fsomeone%2Fa-project')
    ).toBe('{"v":1}')
  })

  it('leaves a value this desk did not write alone, and says so', () => {
    window.localStorage.setItem(KEY, 'something else entirely')
    expect(resetAppearance(KEY)).toBe('foreign')
    expect(window.localStorage.getItem(KEY)).toBe('something else entirely')
  })

  it('reports a removal this browser refused, rather than one it made', () => {
    const backing = new Map<string, string>([[KEY, '{"v":1,"theme":"dark"}']])
    vi.stubGlobal('localStorage', {
      getItem: (key: string) => backing.get(key) ?? null,
      setItem: (key: string, value: string) => void backing.set(key, value),
      removeItem: () => {},
      clear: () => {}
    })
    expect(resetAppearance(KEY)).toBe('refused')
  })

  it('is content to clear a key with nothing under it', () => {
    // An absent record is the state this asks for; removing it again is
    // harmless, and calling it foreign would refuse to answer a question
    // nobody asked.
    expect(resetAppearance(KEY)).toBe('cleared')
  })
})
