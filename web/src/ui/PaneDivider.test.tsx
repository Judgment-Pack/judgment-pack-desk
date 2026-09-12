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
