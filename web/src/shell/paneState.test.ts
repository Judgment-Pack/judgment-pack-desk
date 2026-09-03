/**
 * The layout record: how it is keyed, what it survives, and what it refuses.
 *
 * The load-bearing case is the one about a **throwing** accessor. A private
 * window and a browser with site data blocked do not answer null — they raise
 * on the property access itself — and a shell that crashed there would be a
 * desk that will not open in a private window.
 */
import { afterEach, describe, expect, it, vi } from 'vitest'
import {
  BUILT_IN_SHELL_STATE,
  NOTHING_TOUCHED,
  identityIsResolved,
  initialShellState,
  projectKey,
  readShellState,
  resetShellState,
  shellStateKey,
  writeShellState
} from './paneState'
import { DESK_DEFAULTS } from '../config/deskConfig'

afterEach(() => {
  vi.unstubAllGlobals()
  window.localStorage.clear()
})

const OPEN_EVERYTHING = JSON.stringify({
  v: 1,
  left: { mode: 'icons' },
  inspector: { open: true },
  console: { open: true, tab: 'files' }
})

describe('projectKey', () => {
  it('is the literal default when the chassis has not reported a root', () => {
    expect(projectKey(undefined)).toBe('default')
    expect(projectKey('')).toBe('default')
    expect(projectKey('   ')).toBe('default')
  })

  it('keys two configless projects apart, which the config path could not', () => {
    // The defect this replaced: the key came from the runtime's `configPath`,
    // and a project with no `jpack.json` has none — so every configless
    // project on one origin mapped to the literal `default` and shared one
    // record. The chassis' root is there either way.
    expect(projectKey('/home/someone/one')).not.toBe(projectKey('/home/someone/two'))
    expect(projectKey('/home/someone/one')).not.toBe('default')
  })

  it('says outright whether the identity has been read yet', () => {
    expect(identityIsResolved(undefined)).toBe(false)
    expect(identityIsResolved('')).toBe(false)
    expect(identityIsResolved('   ')).toBe(false)
    expect(identityIsResolved('/home/someone/one')).toBe(true)
  })

  it('slugs the path and appends a hash of the whole of it', () => {
    const key = projectKey('/home/someone/Projects/Intake Triage/jpack.json')
    expect(key).toMatch(/^[a-z0-9._-]+-[0-9a-f]{8}$/)
  })

  it('does not collide for two paths that differ only past the 64th character', () => {
    // The hash is taken over the untruncated path and appended after the
    // truncation, so a shared prefix is not a shared record. Without that,
    // two long sibling directories would share one layout.
    const prefix = `/${'a'.repeat(70)}/`
    expect(projectKey(`${prefix}one/jpack.json`)).not.toBe(projectKey(`${prefix}two/jpack.json`))
  })

  it('is stable for one path', () => {
    expect(projectKey('/p/jpack.json')).toBe(projectKey('/p/jpack.json'))
  })
})

describe('readShellState', () => {
  it('round-trips a record this shell wrote', () => {
    const key = shellStateKey('p')
    window.localStorage.setItem(key, OPEN_EVERYTHING)
    expect(readShellState(key)).toEqual({
      left: { mode: 'icons' },
      inspector: { open: true },
      console: { open: true, tab: 'files' }
    })
  })

  it('keeps two projects apart', () => {
    window.localStorage.setItem(shellStateKey('one'), OPEN_EVERYTHING)
    expect(readShellState(shellStateKey('one'))?.left?.mode).toBe('icons')
    expect(readShellState(shellStateKey('two'))).toBeUndefined()
  })

  it('discards another version silently', () => {
    const key = shellStateKey('p')
    window.localStorage.setItem(key, JSON.stringify({ v: 2, left: { mode: 'icons' } }))
    expect(readShellState(key)).toBeUndefined()
  })

  it('discards a value that is not JSON, or not an object', () => {
    const key = shellStateKey('p')
    window.localStorage.setItem(key, 'not json')
    expect(readShellState(key)).toBeUndefined()
    window.localStorage.setItem(key, '[1,2,3]')
    expect(readShellState(key)).toBeUndefined()
  })

  it('survives a localStorage accessor that THROWS rather than answering', () => {
    vi.stubGlobal('localStorage', {
      getItem() {
        throw new DOMException('The operation is insecure.', 'SecurityError')
      },
      setItem() {
        throw new DOMException('The operation is insecure.', 'SecurityError')
      },
      removeItem() {
        throw new DOMException('The operation is insecure.', 'SecurityError')
      }
    })
    expect(readShellState(shellStateKey('p'))).toBeUndefined()
    // And the reset does not take the page down either.
    expect(() => resetShellState(shellStateKey('p'))).not.toThrow()
  })
})

describe('writeShellState', () => {
  const STATE = {
    left: { mode: 'icons' as const },
    inspector: { open: true },
    console: { open: true, tab: 'files' as const }
  }

  it('serializes only the panes the viewer actually moved', () => {
    // A record is preferred over the configuration on the next read, so a
    // section written on the strength of a sibling's toggle is a built-in
    // default silently outranking `panes` in `jpack-desk.json` for ever.
    const key = shellStateKey('p')
    writeShellState(key, STATE, { ...NOTHING_TOUCHED, console: true })
    expect(JSON.parse(window.localStorage.getItem(key)!)).toEqual({
      v: 1,
      console: { open: true, tab: 'files' }
    })
  })

  it('writes all three where all three were chosen', () => {
    const key = shellStateKey('p')
    writeShellState(key, STATE, { left: true, inspector: true, console: true })
    expect(JSON.parse(window.localStorage.getItem(key)!)).toEqual({
      v: 1,
      left: { mode: 'icons' },
      inspector: { open: true },
      console: { open: true, tab: 'files' }
    })
  })

  it('round-trips through the reader as a partial record', () => {
    const key = shellStateKey('p')
    writeShellState(key, STATE, { ...NOTHING_TOUCHED, left: true })
    expect(readShellState(key)).toEqual({ left: { mode: 'icons' } })
  })

  it('keeps the sections an earlier visit chose, and overrides only this visit’s', () => {
    // Starting the outgoing record at `{v}` erased them: with a stored rail
    // and Inspector on disk, toggling only the Console rewrote the key as
    // `{"v":1,"console":…}` and threw both away — so the next narrow→wide
    // cycle restored defaults rather than the layout the viewer had chosen.
    const key = shellStateKey('p')
    window.localStorage.setItem(
      key,
      JSON.stringify({ v: 1, left: { mode: 'icons' }, inspector: { open: true } })
    )
    writeShellState(
      key,
      {
        left: { mode: 'expanded' },
        inspector: { open: false },
        console: { open: true, tab: 'files' }
      },
      { ...NOTHING_TOUCHED, console: true }
    )
    expect(JSON.parse(window.localStorage.getItem(key)!)).toEqual({
      v: 1,
      left: { mode: 'icons' },
      inspector: { open: true },
      console: { open: true, tab: 'files' }
    })
  })

  it('carries forward nothing from a record it cannot read', () => {
    // The base is taken through the same validator, so a corrupt or
    // wrong-version record is discarded rather than merged into the new one.
    const key = shellStateKey('p')
    window.localStorage.setItem(key, JSON.stringify({ v: 99, left: { mode: 'icons' } }))
    writeShellState(key, STATE, { ...NOTHING_TOUCHED, console: true })
    expect(JSON.parse(window.localStorage.getItem(key)!)).toEqual({
      v: 1,
      console: { open: true, tab: 'files' }
    })
  })

  it('overrides a stored section the viewer has moved again', () => {
    const key = shellStateKey('p')
    window.localStorage.setItem(key, JSON.stringify({ v: 1, left: { mode: 'expanded' } }))
    writeShellState(key, STATE, { ...NOTHING_TOUCHED, left: true })
    expect(JSON.parse(window.localStorage.getItem(key)!)).toEqual({
      v: 1,
      left: { mode: 'icons' }
    })
  })
})

describe('resetShellState', () => {
  it('clears exactly one key and nothing beside it, and says it did', () => {
    window.localStorage.setItem(shellStateKey('one'), OPEN_EVERYTHING)
    window.localStorage.setItem(shellStateKey('two'), OPEN_EVERYTHING)
    window.localStorage.setItem('something-else', 'kept')
    expect(resetShellState(shellStateKey('one'))).toBe(true)
    expect(window.localStorage.getItem(shellStateKey('one'))).toBeNull()
    expect(window.localStorage.getItem(shellStateKey('two'))).toBe(OPEN_EVERYTHING)
    expect(window.localStorage.getItem('something-else')).toBe('kept')
  })

  it('reports a deletion that did not take, rather than assuming it did', () => {
    // `removeItem` resolves on a storage that keeps the value. The page's
    // "Cleared." is a claim about the record, so the record is read back.
    vi.stubGlobal('localStorage', {
      getItem: () => OPEN_EVERYTHING,
      setItem: () => {},
      removeItem: () => {}
    })
    expect(resetShellState(shellStateKey('one'))).toBe(false)
  })

  it('reports a storage that throws on the accessor as a failure, not a success', () => {
    vi.stubGlobal('localStorage', {
      getItem() {
        throw new DOMException('The operation is insecure.', 'SecurityError')
      },
      setItem() {
        throw new DOMException('The operation is insecure.', 'SecurityError')
      },
      removeItem() {
        throw new DOMException('The operation is insecure.', 'SecurityError')
      }
    })
    expect(resetShellState(shellStateKey('one'))).toBe(false)
  })
})

const WIDE = { railIsDrawer: false, inspectorIsDrawer: false }

describe('initialShellState', () => {
  it('is the built-in state with no record and no configuration', () => {
    expect(initialShellState(undefined, undefined, WIDE)).toEqual(BUILT_IN_SHELL_STATE)
  })

  it('prefers the stored record over the configured defaults', () => {
    const stored = { left: { mode: 'icons' as const } }
    const state = initialShellState(stored, DESK_DEFAULTS.panes, WIDE)
    expect(state.left.mode).toBe('icons')
  })

  it('falls back to the configured defaults where the record says nothing', () => {
    const state = initialShellState(undefined, {
      ...DESK_DEFAULTS.panes,
      console: { open: true, height: 240 }
    }, WIDE)
    expect(state.console.open).toBe(true)
  })

  it('clamps a wide-monitor layout to the viewport that is actually there', () => {
    const stored = {
      left: { mode: 'expanded' as const },
      inspector: { open: true },
      console: { open: true, tab: 'connection' as const }
    }
    const narrow = initialShellState(stored, undefined, {
      railIsDrawer: true,
      inspectorIsDrawer: true
    })
    expect(narrow.left.mode).toBe('icons')
    expect(narrow.inspector.open).toBe(false)
    // The console is not a column, so nothing about the width clamps it.
    expect(narrow.console.open).toBe(true)
  })
})
