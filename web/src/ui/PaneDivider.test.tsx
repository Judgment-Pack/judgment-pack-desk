import { act, cleanup, fireEvent, render, screen } from '@testing-library/react'
import { useState } from 'react'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
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

describe('divider for a pane on the left',()=>{
 it('uses the same keyboard controls with movement following the divider',()=>{
  const collapse=vi.fn()
  function Example(){const [width,setWidth]=useState(220);return <PaneDivider paneSide="start" label="Folders" controls="folders" value={width} min={180} max={320} onChange={setWidth} onReset={()=>setWidth(220)} onCollapse={collapse}/>}
  render(<Example/>);const divider=screen.getByRole('separator',{name:'Folders'})
  expect(divider.getAttribute('data-pane-side')).toBe('start')
  fireEvent.keyDown(divider,{key:'ArrowRight'});expect(divider.getAttribute('aria-valuenow')).toBe('228')
  fireEvent.keyDown(divider,{key:'ArrowLeft',shiftKey:true});expect(divider.getAttribute('aria-valuenow')).toBe('196')
  fireEvent.keyDown(divider,{key:'Home'});fireEvent.keyDown(divider,{key:'ArrowLeft'});expect(divider.getAttribute('aria-valuenow')).toBe('180')
  fireEvent.keyDown(divider,{key:'End'});fireEvent.keyDown(divider,{key:'ArrowRight'});expect(divider.getAttribute('aria-valuenow')).toBe('320')
  fireEvent.doubleClick(divider);expect(divider.getAttribute('aria-valuenow')).toBe('220')
  fireEvent.keyDown(divider,{key:'Enter'});expect(collapse).toHaveBeenCalledOnce()
 })
})


describe('pointer resizing', () => {
  let frames: Map<number, FrameRequestCallback>, frameId: number, capture: Set<number>
  beforeEach(() => {
    frames = new Map(); frameId = 0; capture = new Set()
    vi.stubGlobal('requestAnimationFrame', (callback: FrameRequestCallback) => { frames.set(++frameId, callback); return frameId })
    vi.stubGlobal('cancelAnimationFrame', (id: number) => frames.delete(id))
    vi.stubGlobal('PointerEvent', class extends MouseEvent {
      pointerId: number; isPrimary: boolean
      constructor(type: string, options: PointerEventInit) { super(type, options); this.pointerId = options.pointerId ?? 1; this.isPrimary = options.isPrimary ?? true }
    })
    Object.defineProperties(HTMLElement.prototype, {
      setPointerCapture: { configurable: true, value: (id: number) => capture.add(id) },
      hasPointerCapture: { configurable: true, value: (id: number) => capture.has(id) },
      releasePointerCapture: { configurable: true, value: (id: number) => capture.delete(id) }
    })
  })
  afterEach(() => {
    cleanup(); vi.unstubAllGlobals()
    for (const method of ['setPointerCapture', 'hasPointerCapture', 'releasePointerCapture']) Reflect.deleteProperty(HTMLElement.prototype, method)
    document.body.removeAttribute('style')
  })
  const tick = () => act(() => { const queued = [...frames.values()]; frames.clear(); queued.forEach(fn => fn(16)) })
  function setup(orientation: 'vertical' | 'horizontal' = 'vertical', paneSide: 'start' | 'end' = 'end') {
    const preview = document.createElement('section'), commit = vi.fn()
    document.body.append(preview)
    let renders = 0
    function Example({ max = 540 }: { max?: number }) {
      renders++
      const [width, setWidth] = useState(360)
      return <PaneDivider label="Pane" controls="pane" value={width} min={320} max={max} orientation={orientation} paneSide={paneSide}
        preview={{ element: preview, property: '--size' }} onChange={next => { commit(next); setWidth(next) }} onReset={() => setWidth(360)} onCollapse={() => {}} />
    }
    const view = render(<Example />), divider = screen.getByRole('separator', { name: 'Pane' })
    const pointer = (type: string, x = 500, y = 500, pointerId = 1) => fireEvent(divider, new PointerEvent(type, { bubbles: true, pointerId, isPrimary: true, button: 0, clientX: x, clientY: y }))
    return { preview, commit, renders: () => renders, pointer, divider, ...view, changeBounds: () => view.rerender(<Example max={400} />) }
  }
  it.each([['vertical', 'end'], ['vertical', 'start'], ['horizontal', 'end'], ['horizontal', 'start']] as const)('previews %s/%s once per frame without rendering or saving the owner', (orientation, side) => {
    const f = setup(orientation, side), initialRenders = f.renders()
    f.pointer('pointerdown')
    const delta = side === 'end' ? -1 : 1
    for (let i = 1; i <= 20; i++) f.pointer('pointermove', 500 + (orientation === 'vertical' ? delta * i : 0), 500 + (orientation === 'horizontal' ? delta * i : 0))
    expect(f.preview.style.getPropertyValue('--size')).toBe('')
    tick()
    expect(f.preview.style.getPropertyValue('--size')).toBe('380px')
    expect(f.divider.getAttribute('aria-valuenow')).toBe('380')
    expect(f.commit).not.toHaveBeenCalled()
    expect(f.renders()).toBe(initialRenders)
    // Release before the next frame: keep the latest pointer coordinate, not the last painted width.
    f.pointer('pointerup', 500 + (orientation === 'vertical' ? delta * 55 : 0), 500 + (orientation === 'horizontal' ? delta * 55 : 0))
    expect(f.commit).toHaveBeenCalledExactlyOnceWith(415)
    expect(f.divider.getAttribute('aria-valuenow')).toBe('415')
    expect(f.preview.style.getPropertyValue('--size')).toBe('')
    tick(); expect(f.commit).toHaveBeenCalledTimes(1)
    f.preview.remove()
  })
  it.each(['Escape', 'pointercancel', 'lostpointercapture', 'unmount', 'bounds'])('cancels on %s without saving and cleans up pending work', reason => {
    const f = setup()
    f.preview.style.setProperty('--size', '360px', 'important')
    document.body.style.cursor = 'crosshair'; document.body.style.userSelect = 'text'
    f.pointer('pointerdown'); f.pointer('pointermove', 460); tick()
    f.pointer('pointermove', 440)
    if (reason === 'Escape') fireEvent.keyDown(f.divider, { key: 'Escape' })
    else if (reason === 'unmount') f.unmount()
    else if (reason === 'bounds') f.changeBounds()
    else f.pointer(reason)
    tick()
    expect(f.commit).not.toHaveBeenCalled()
    expect(f.preview.style.getPropertyValue('--size')).toBe('360px')
    expect(f.preview.style.getPropertyPriority('--size')).toBe('important')
    expect(document.body.style.cursor).toBe('crosshair')
    expect(document.body.style.userSelect).toBe('text')
    expect(capture.size).toBe(0)
    f.preview.remove()
  })
  it('clamps drag bounds, ignores other pointers, and does not save a click without movement', () => {
    const f = setup()
    f.pointer('pointerdown'); f.pointer('pointerup'); expect(f.commit).not.toHaveBeenCalled()
    f.pointer('pointerdown'); f.pointer('pointermove', 0, 500, 2); tick()
    expect(f.preview.style.getPropertyValue('--size')).toBe('')
    f.pointer('pointermove', 0); tick(); expect(f.divider.getAttribute('aria-valuenow')).toBe('540')
    f.pointer('pointermove', 1000); tick(); expect(f.divider.getAttribute('aria-valuenow')).toBe('320')
    f.pointer('pointerup', 1000); expect(f.commit).toHaveBeenCalledExactlyOnceWith(320)
    f.preview.remove()
  })
})
