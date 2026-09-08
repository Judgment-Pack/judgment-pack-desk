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
  writeAppearance,
  type ChosenAppearance
} from './appearanceState'
import { projectKey } from './paneState'

afterEach(() => {
  window.localStorage.clear()
  vi.unstubAllGlobals()
})

const ROOT = '/home/someone/a-project'
const KEY = appearanceKey(projectKey(ROOT))
const BOTH: ChosenAppearance = { theme: true, density: true }
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
    writeAppearance(KEY, { theme: 'dark' }, { theme: true, density: false })
    expect(JSON.parse(window.localStorage.getItem(KEY)!)).toEqual({ v: 1, theme: 'dark' })
    // A density chosen later must not take the theme with it, and must not
    // drop it either.
    writeAppearance(KEY, { density: 'compact' }, { theme: false, density: true })
    expect(JSON.parse(window.localStorage.getItem(KEY)!)).toEqual({
      v: 1,
      theme: 'dark',
      density: 'compact'
    })
  })

  it('reads back what it wrote', () => {
    writeAppearance(KEY, { theme: 'light', density: 'compact' }, BOTH)
    expect(readAppearance(KEY)).toEqual({ theme: 'light', density: 'compact' })
  })

  it('treats a value outside the decoder’s unions as no preference at all', () => {
    // Strictness about a *file* belongs to the decoder, which refuses a typo'd
    // theme by name. A value somebody put in this browser's storage by hand is
    // not a file: it is discarded, and the project's default applies.
    window.localStorage.setItem(KEY, JSON.stringify({ v: 1, theme: 'midnight', density: 'roomy' }))
    expect(readAppearance(KEY)).toEqual({})
    expect(effectiveAppearance(readAppearance(KEY), FILE)).toEqual(FILE)
  })

  it('keeps the member it can read when the other is outside the union', () => {
    window.localStorage.setItem(KEY, JSON.stringify({ v: 1, theme: 'dark', density: 'roomy' }))
    expect(readAppearance(KEY)).toEqual({ theme: 'dark' })
  })

  it('discards a record that is not this desk’s', () => {
    for (const raw of ['not json at all', '[]', 'null', '"dark"', '{"v":2,"theme":"dark"}']) {
      window.localStorage.setItem(KEY, raw)
      expect(readAppearance(KEY), raw).toBeUndefined()
    }
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
    expect(() => writeAppearance(KEY, { theme: 'dark' }, BOTH)).not.toThrow()
    expect(resetAppearance(KEY)).toBe('refused')
  })
})

describe('forgetting this browser’s appearance', () => {
  it('removes exactly one key', () => {
    // `localStorage.clear()` would take the session token and every other
    // project's record with it.
    writeAppearance(KEY, { theme: 'dark' }, BOTH)
    window.localStorage.setItem('jpack-desk:appearance:v1:another', '{"v":1}')
    window.localStorage.setItem('jpack-desk-token', 'a token')
    expect(resetAppearance(KEY)).toBe('cleared')
    expect(window.localStorage.getItem(KEY)).toBeNull()
    expect(window.localStorage.getItem('jpack-desk:appearance:v1:another')).toBe('{"v":1}')
    expect(window.localStorage.getItem('jpack-desk-token')).toBe('a token')
  })

  it('leaves the pane record where it is', () => {
    // Two records, two keys, two controls. A reset of one that took the other
    // would be a control lying about its scope.
    writeAppearance(KEY, { theme: 'dark' }, BOTH)
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
