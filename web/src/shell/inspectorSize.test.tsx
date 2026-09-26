import { act, cleanup, render, screen } from '@testing-library/react'
import { afterEach, expect, it, vi } from 'vitest'
import { InspectorSizeProvider, InspectorSlotContext, useInspectorControls, useInspectorSlot } from './InspectorSlot'

afterEach(() => { cleanup(); vi.unstubAllGlobals() })

it('updates measured-size readers without re-rendering portal/action consumers during resize', () => {
  const pane = document.createElement('aside')
  let width = 400, controlsRenders = 0, read: () => void = () => {}
  pane.getBoundingClientRect = () => ({ width, height: 800 } as DOMRect)
  vi.stubGlobal('ResizeObserver', class { constructor(callback: () => void) { read = callback } observe() {} disconnect() {} })
  const reveal = vi.fn(), slot = { open: true, size: 0, tab: null, setTab: () => {}, target: pane, claim: () => () => {}, reveal }
  function Controls() { controlsRenders++; const controls = useInspectorControls(); return <button onClick={controls.reveal}>{controls.open ? 'Open' : 'Closed'}</button> }
  function Size() { const slot = useInspectorSlot(); return <output aria-label="Pane width">{slot.size}</output> }
  render(<InspectorSlotContext.Provider value={slot}><InspectorSizeProvider pane={pane} open><Controls /><Size /></InspectorSizeProvider></InspectorSlotContext.Provider>)
  expect(screen.getByLabelText('Pane width').textContent).toBe('400')
  const before = controlsRenders
  for (width = 401; width <= 460; width++) act(() => read())
  expect(screen.getByLabelText('Pane width').textContent).toBe('460')
  expect(controlsRenders).toBe(before)
  screen.getByRole('button', { name: 'Open' }).click()
  expect(reveal).toHaveBeenCalledOnce()
})
