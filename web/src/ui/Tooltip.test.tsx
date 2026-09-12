import { act, cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react'
import { afterEach, describe, expect, it, vi } from 'vitest'
import { Link, MemoryRouter, Route, Routes } from 'react-router-dom'
import { Tooltip, TooltipProvider, OverflowTooltip } from './Tooltip'
import { Digest } from './Digest'

afterEach(() => { cleanup(); vi.restoreAllMocks(); vi.unstubAllGlobals() })
const focus = (element: HTMLElement) => act(() => element.focus())
function dimensions(width: number, scroll: number) {
  vi.spyOn(HTMLElement.prototype, 'clientWidth', 'get').mockReturnValue(width)
  vi.spyOn(HTMLElement.prototype, 'clientHeight', 'get').mockReturnValue(20)
  vi.spyOn(HTMLElement.prototype, 'scrollWidth', 'get').mockReturnValue(scroll)
}

describe('shared tooltip behavior', () => {
  it('preserves the control, its helper, activation and focus after Escape', async () => {
    const click = vi.fn()
    render(<><p id="help">Existing help</p><Tooltip content="Additional hint"><button aria-describedby="help" onClick={click}>Action</button></Tooltip></>)
    const button = screen.getByRole('button', { name: 'Action' })
    focus(button)
    const hint = await screen.findByRole('tooltip')
    const ids = button.getAttribute('aria-describedby')!.split(' ')
    expect(ids).toContain('help')
    expect(ids).toContain(hint.id)
    fireEvent.keyDown(button, { key: 'Escape' })
    await waitFor(() => expect(screen.queryByRole('tooltip')).toBeNull())
    expect(document.activeElement).toBe(button)
    expect(button.getAttribute('aria-describedby')).toBe('help')
    fireEvent.click(button)
    expect(click).toHaveBeenCalledOnce()
    expect(button.hasAttribute('title')).toBe(false)
  })

  it('does not show a tooltip for text that fits or hidden text', () => {
    dimensions(100, 80)
    const { rerender } = render(<OverflowTooltip><button>Fits</button></OverflowTooltip>)
    focus(screen.getByRole('button'))
    expect(screen.queryByRole('tooltip')).toBeNull()
    dimensions(0, 900)
    rerender(<OverflowTooltip><button>Hidden description</button></OverflowTooltip>)
    fireEvent(window, new Event('resize'))
    expect(screen.queryByRole('tooltip')).toBeNull()
  })

  it('reveals clipped descendants and dismisses when resizing makes them fit', async () => {
    dimensions(100, 200)
    render(<OverflowTooltip selector="span"><button><span>A clipped question</span></button></OverflowTooltip>)
    focus(screen.getByRole('button'))
    expect((await screen.findByRole('tooltip')).textContent).toBe('A clipped question')
    dimensions(300, 200)
    fireEvent(window, new Event('resize'))
    await waitFor(() => expect(screen.queryByRole('tooltip')).toBeNull())
  })

  it('dismisses stale content when a virtualized row changes, and measures its new text', async () => {
    dimensions(100, 200)
    const { rerender } = render(<OverflowTooltip><button>First question</button></OverflowTooltip>)
    focus(screen.getByRole('button'))
    await screen.findByRole('tooltip')
    rerender(<OverflowTooltip><button>Replacement question</button></OverflowTooltip>)
    await waitFor(() => expect(screen.queryByRole('tooltip')).toBeNull())
    act(() => screen.getByRole('button').blur())
    focus(screen.getByRole('button'))
    expect((await screen.findByRole('tooltip')).textContent).toBe('Replacement question')
  })

  it('keeps very long text in the destination, with a short hint', async () => {
    dimensions(100, 200)
    render(<OverflowTooltip fallback="Open Preview to read the full pack details."><button>{'Long question '.repeat(80)}</button></OverflowTooltip>)
    focus(screen.getByRole('button'))
    expect((await screen.findByRole('tooltip')).textContent).toBe('Open Preview to read the full pack details.')
    expect(screen.getByRole('button').textContent).toBe('Long question '.repeat(80))
  })

  it('dismisses on navigation even when the trigger remains mounted', async () => {
    render(<MemoryRouter><TooltipProvider><Tooltip content="Persistent header"><button>Header</button></Tooltip><Link to="/next">Next</Link><Routes><Route path="*" element={<p>Page</p>} /></Routes></TooltipProvider></MemoryRouter>)
    focus(screen.getByRole('button'))
    await screen.findByRole('tooltip')
    fireEvent.click(screen.getByRole('link'))
    await waitFor(() => expect(screen.queryByRole('tooltip')).toBeNull())
  })

  it('does not open on automatic dialog focus when suppressed', () => {
    render(<Tooltip content="Close Inspector" openOnFocus={false}><button>Close</button></Tooltip>)
    focus(screen.getByRole('button'))
    expect(screen.queryByRole('tooltip')).toBeNull()
  })

  it('closes an open hint when disabled during a gesture', async () => {
    const { rerender } = render(<Tooltip content="Resize"><button>Divider</button></Tooltip>)
    focus(screen.getByRole('button'))
    await screen.findByRole('tooltip')
    rerender(<Tooltip content="Resize" disabled><button>Divider</button></Tooltip>)
    await waitFor(() => expect(screen.queryByRole('tooltip')).toBeNull())
  })
})

describe('full digest disclosure', () => {
  it('copies the complete value and retains selectable text if clipboard access fails', async () => {
    const value = '0123456789abcdef'.repeat(4)
    const writeText = vi.fn().mockResolvedValue(undefined)
    vi.stubGlobal('navigator', new Proxy(navigator, { get: (target, key) => key === 'clipboard' ? { writeText } : Reflect.get(target, key) }))
    render(<Digest value={value} />)
    fireEvent.click(screen.getByRole('button', { name: `Show full digest ${value}` }))
    const popover = await screen.findByRole('dialog', { name: 'Full digest' })
    expect(popover.querySelector('code')?.textContent).toBe(value)
    fireEvent.click(screen.getByRole('button', { name: 'Copy digest' }))
    await waitFor(() => expect(screen.getByRole('status').textContent).toBe('Copied'))
    expect(writeText).toHaveBeenCalledWith(value)
    writeText.mockRejectedValueOnce(new Error('Clipboard denied'))
    fireEvent.click(screen.getByRole('button', { name: 'Copy digest' }))
    await waitFor(() => expect(screen.getByRole('status').textContent).toContain('Select the full value'))
    expect(popover.querySelector('code')?.textContent).toBe(value)
  })

  it('represents an absent file without offering an empty digest', () => {
    render(<Digest value="" />)
    expect(screen.getByText('sha256 (no file)')).toBeTruthy()
    expect(screen.queryByRole('button')).toBeNull()
  })
})
