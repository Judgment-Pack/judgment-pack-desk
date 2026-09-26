import {describe, expect, it} from 'vitest'
import {overlayGeometry} from './overlayGeometry'
import {BUILT_IN_SHELL_STATE, initialShellState, readShellState, writeShellState, NOTHING_TOUCHED} from './paneState'

describe('expanded pane geometry', () => {
  it('uses three quarters of the available workspace while retaining pack context', () => {
    expect(overlayGeometry(1200)).toEqual({full: false, min: 640, max: 960, width: 900})
    expect(overlayGeometry(1200, 2000).width).toBe(960)
    expect(overlayGeometry(900).width).toBe(660)
    expect(overlayGeometry(880).width).toBe(640)
  })
  it('reserves a docked folder column in addition to the actual pack content', () => {
    expect(overlayGeometry(1280, 1200, 220)).toEqual({full: false, min: 640, max: 820, width: 820})
    expect(overlayGeometry(1000, 1200, 220).full).toBe(true)
  })
  it('takes the available width when the reading width and visible pack cannot both fit', () => {
    for (const width of [0, 320, 640, 879]) expect(overlayGeometry(width, 1500)).toEqual({full: true, min: width, max: width, width})
  })
  it('ignores invalid preferences and restores a wide preference after viewport clamping', () => {
    for (const width of [NaN, Infinity, -20, 400, 640.5, 99999]) expect(overlayGeometry(1200, width).width).toBe(900)
    expect(overlayGeometry(1200, 1200).width).toBe(960)
    expect(overlayGeometry(1800, 1200).width).toBe(1200)
  })
})

it('stores overlay width separately and preserves it through unrelated pane updates and reload', () => {
  const key = 'overlay-width-test'
  try {
    writeShellState(key, {...BUILT_IN_SHELL_STATE, inspectorWidth: 400, overlayWidth: 1100}, {...NOTHING_TOUCHED, inspectorWidth: true, overlayWidth: true})
    writeShellState(key, {...BUILT_IN_SHELL_STATE, inspectorWidth: 480}, {...NOTHING_TOUCHED, inspectorWidth: true})
    expect(readShellState(key)).toEqual({inspectorWidth: 480, overlayWidth: 1100})
    const state = initialShellState(readShellState(key), undefined, {railIsDrawer: false, inspectorIsDrawer: true})
    expect(state.overlayWidth).toBe(1100)
    writeShellState(key, {...state, overlayWidth: undefined}, {...NOTHING_TOUCHED, overlayWidth: true})
    expect(readShellState(key)).toEqual({inspectorWidth: 480})
    localStorage.setItem(key, JSON.stringify({v: 2, overlayWidth: '1100', inspectorWidth: 400}))
    expect(readShellState(key)).toEqual({inspectorWidth: 400})
  } finally { localStorage.removeItem(key) }
})
