import { act, cleanup, fireEvent, render, screen } from '@testing-library/react'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { DESK_DEFAULTS } from '../config/deskConfig'
import { ShellStateProvider, projectKey, shellStateKey, useShellState } from './paneState'

const key = (project: string) => shellStateKey(projectKey(project))
const viewport = { railIsDrawer: false, inspectorIsDrawer: false }
function Controls() {
  const shell = useShellState()
  return <>
    <output>{shell.inspectorWidth ?? 'configured'}</output>
    <button onClick={() => shell.resizeInspector(520)}>Resize</button>
    <button onClick={shell.toggleConsole}>Console</button>
    <button onClick={shell.resetInspectorWidth}>Default width</button>
    <button onClick={shell.resetPanes}>Reset panes</button>
  </>
}
const app = (project = '/one') => <ShellStateProvider projectIdentity={project} panes={DESK_DEFAULTS.panes} viewport={viewport}><Controls /></ShellStateProvider>
const flush = () => act(() => vi.advanceTimersByTime(300))
beforeEach(() => vi.useFakeTimers())
afterEach(() => { cleanup(); vi.useRealTimers(); localStorage.clear() })

describe('per-project inspector size preference', () => {
  it('migrates old collapse choices only when a width is explicitly chosen', () => {
    localStorage.setItem(key('/one'), JSON.stringify({ v: 1, left: { mode: 'icons' } }))
    render(app()); flush()
    expect(JSON.parse(localStorage.getItem(key('/one'))!).v).toBe(1)
    fireEvent.click(screen.getByText('Resize')); flush()
    expect(JSON.parse(localStorage.getItem(key('/one'))!)).toEqual({ v: 2, left: { mode: 'icons' }, inspectorWidth: 520 })
    fireEvent.click(screen.getByText('Console')); flush()
    expect(JSON.parse(localStorage.getItem(key('/one'))!).inspectorWidth).toBe(520)
  })
  it('restores widths without writing configured defaults or viewport clamps', () => {
    localStorage.setItem(key('/one'), JSON.stringify({ v: 2, inspectorWidth: 640 }))
    const { rerender } = render(app()); flush()
    expect(screen.getByRole('status').textContent).toBe('640')
    rerender(<ShellStateProvider projectIdentity="/one" panes={DESK_DEFAULTS.panes} viewport={{ ...viewport, inspectorIsDrawer: true }}><Controls /></ShellStateProvider>); flush()
    expect(JSON.parse(localStorage.getItem(key('/one'))!)).toEqual({ v: 2, inspectorWidth: 640 })
  })
  it('does not carry a resized width across project identities', () => {
    localStorage.setItem(key('/two'), JSON.stringify({ v: 2, inspectorWidth: 400 }))
    const { rerender } = render(app())
    fireEvent.click(screen.getByText('Resize')); flush()
    rerender(app('/two')); flush()
    expect(screen.getByRole('status').textContent).toBe('400')
    expect(JSON.parse(localStorage.getItem(key('/two'))!).inspectorWidth).toBe(400)
    expect(JSON.parse(localStorage.getItem(key('/one'))!).inspectorWidth).toBe(520)
  })
  it('resets the width alone, and cancels pending width writes on full reset', () => {
    render(app())
    fireEvent.click(screen.getByText('Console')); fireEvent.click(screen.getByText('Resize')); flush()
    fireEvent.click(screen.getByText('Default width')); flush()
    expect(JSON.parse(localStorage.getItem(key('/one'))!)).toEqual({ v: 2, console: { open: true, tab: 'connection' } })
    fireEvent.click(screen.getByText('Resize'))
    fireEvent.click(screen.getByText('Reset panes')); flush()
    expect(localStorage.getItem(key('/one'))).toBeNull()
    expect(screen.getByRole('status').textContent).toBe('configured')
  })
})
