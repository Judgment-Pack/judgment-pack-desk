import { cleanup, fireEvent, render, screen } from '@testing-library/react'
import { useState } from 'react'
import { afterEach, describe, expect, it, vi } from 'vitest'
import { PaneDivider } from './PaneDivider'

afterEach(cleanup)
describe('keyboard-resizable inspector divider', () => {
  it('announces the controlled pane and bounds, with directional keyboard movement', () => {
    const collapse = vi.fn()
    function Example() {
      const [width, setWidth] = useState(360)
      return <PaneDivider label="Inspector" controls="inspector" value={width} min={320} max={540}
        onChange={setWidth} onReset={() => setWidth(360)} onCollapse={collapse} />
    }
    render(<Example />)
    const divider = screen.getByRole('separator', { name: 'Inspector' })
    expect(divider.getAttribute('aria-controls')).toBe('inspector')
    expect(divider.getAttribute('aria-orientation')).toBe('vertical')
    fireEvent.keyDown(divider, { key: 'ArrowLeft' })
    expect(divider.getAttribute('aria-valuenow')).toBe('368')
    fireEvent.keyDown(divider, { key: 'ArrowRight', shiftKey: true })
    expect(divider.getAttribute('aria-valuenow')).toBe('336')
    fireEvent.keyDown(divider, { key: 'Home' })
    fireEvent.keyDown(divider, { key: 'ArrowRight' })
    expect(divider.getAttribute('aria-valuenow')).toBe('320')
    fireEvent.keyDown(divider, { key: 'End' })
    fireEvent.keyDown(divider, { key: 'ArrowLeft' })
    expect(divider.getAttribute('aria-valuenow')).toBe('540')
    fireEvent.doubleClick(divider)
    expect(divider.getAttribute('aria-valuetext')).toBe('360 pixels wide')
    fireEvent.keyDown(divider, { key: 'Enter' })
    expect(collapse).toHaveBeenCalledOnce()
  })
})

describe('resizable details panel', () => {
  it('uses up/down keys, clamps height, resets, and leaves left/right to other controls', () => {
    const collapse = vi.fn()
    function Example() {
      const [height,setHeight] = useState(240)
      return <PaneDivider orientation="horizontal" label="Details and activity" controls="details" value={height} min={120} max={480} onChange={setHeight} onReset={() => setHeight(240)} onCollapse={collapse} />
    }
    render(<Example />)
    const divider = screen.getByRole('separator',{ name: 'Details and activity' })
    expect(divider.getAttribute('aria-orientation')).toBe('horizontal')
    fireEvent.keyDown(divider,{ key: 'ArrowUp', shiftKey: true })
    expect(divider.getAttribute('aria-valuenow')).toBe('272')
    fireEvent.keyDown(divider,{ key: 'ArrowLeft' })
    expect(divider.getAttribute('aria-valuenow')).toBe('272')
    fireEvent.keyDown(divider,{ key: 'Home' }); fireEvent.keyDown(divider,{ key: 'ArrowDown' })
    expect(divider.getAttribute('aria-valuenow')).toBe('120')
    fireEvent.keyDown(divider,{ key: 'End' }); fireEvent.keyDown(divider,{ key: 'ArrowUp' })
    expect(divider.getAttribute('aria-valuenow')).toBe('480')
    fireEvent.doubleClick(divider)
    expect(divider.getAttribute('aria-valuetext')).toBe('240 pixels high')
    fireEvent.keyDown(divider,{ key: 'Enter' }); expect(collapse).toHaveBeenCalledOnce()
  })
})
