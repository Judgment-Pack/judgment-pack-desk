/**
 * The provider, at the level Admin's reset actually runs.
 *
 * `AdminView.test.tsx` presses the button and reads the sentence; what it
 * cannot see is the write already on its way when the button is pressed. The
 * record is written on a 250ms debounce, so a reset that only removed the key
 * was overwritten a moment later by the very state it had just cleared — and
 * the page had already said "Cleared."
 */
import { act, cleanup, render, screen } from '@testing-library/react'
import { afterEach, describe, expect, it, vi } from 'vitest'
import { ShellStateProvider, projectKey, shellStateKey, useShellState } from './paneState'
import { DESK_DEFAULTS } from '../config/deskConfig'

afterEach(() => {
  cleanup()
  window.localStorage.clear()
})

const ROOT = '/home/someone/a-project'
const KEY = shellStateKey(projectKey(ROOT))

/** Past the record's write debounce, in real time. */
async function pastTheDebounce() {
  await act(async () => {
    await new Promise((resolve) => setTimeout(resolve, 400))
  })
}

function Panel() {
  const shell = useShellState()
  return (
    <div>
      <button type="button" onClick={shell.toggleConsole}>
        toggle the console
      </button>
      <button type="button" onClick={shell.toggleRail}>
        toggle the rail
      </button>
      <button type="button" onClick={() => shell.resetPanes()}>
        reset
      </button>
      <p data-testid="key-resolved">{String(shell.keyResolved)}</p>
      <p data-testid="console-open">{String(shell.console.open)}</p>
      <p data-testid="rail-mode">{shell.left.mode}</p>
      <p data-testid="inspector-open">{String(shell.inspector.open)}</p>
    </div>
  )
}

/**
 * `null` means "the chassis has not answered yet". Not `undefined`: a default
 * parameter takes over for an explicit `undefined`, and the provisional case
 * would silently get the resolved root and assert nothing.
 */
function renderProvider(projectIdentity: string | null = ROOT) {
  return render(
    <ShellStateProvider
      projectIdentity={projectIdentity ?? undefined}
      viewport={{ railIsDrawer: false, inspectorIsDrawer: false }}
    >
      <Panel />
    </ShellStateProvider>
  )
}

describe('the shell state provider', () => {
  it('cancels a write already on its way when the panes are reset', async () => {
    renderProvider()
    act(() => screen.getByRole('button', { name: 'toggle the console' }).click())
    // Inside the debounce window, so the write is pending rather than done.
    expect(window.localStorage.getItem(KEY)).toBeNull()
    act(() => screen.getByRole('button', { name: 'reset' }).click())
    await pastTheDebounce()
    expect(window.localStorage.getItem(KEY)).toBeNull()
  })

  it('puts the live layout back on its defaults, not only the record', async () => {
    renderProvider()
    act(() => screen.getByRole('button', { name: 'toggle the console' }).click())
    expect(screen.getByTestId('console-open').textContent).toBe('true')
    act(() => screen.getByRole('button', { name: 'reset' }).click())
    expect(screen.getByTestId('console-open').textContent).toBe('false')
  })

  it('reports the key as provisional until the chassis names the project', () => {
    renderProvider(null)
    expect(screen.getByTestId('key-resolved').textContent).toBe('false')
    cleanup()
    renderProvider()
    expect(screen.getByTestId('key-resolved').textContent).toBe('true')
  })
})

describe('the provisional key is not read either', () => {
  // The write gate alone was not enough. Until the chassis names the project
  // the key is the literal `default`, and some earlier build of this desk may
  // well have written there — so reading it applied one project's layout to
  // another while the listing was in flight, and permanently where the
  // listing failed, since nothing then arrives to correct it.
  const STALE = JSON.stringify({
    v: 1,
    left: { mode: 'icons' },
    inspector: { open: true },
    console: { open: true, tab: 'files' }
  })

  it('ignores a stale default record while the listing is still pending', () => {
    window.localStorage.setItem(shellStateKey('default'), STALE)
    renderProvider(null)
    expect(screen.getByTestId('rail-mode').textContent).toBe('expanded')
    expect(screen.getByTestId('inspector-open').textContent).toBe('false')
    expect(screen.getByTestId('console-open').textContent).toBe('false')
    // And it is still there afterwards: not reading it is not deleting it.
    expect(window.localStorage.getItem(shellStateKey('default'))).toBe(STALE)
  })

  it('still ignores it when the listing never answers at all', () => {
    // The failed-listing case is the same provider that simply never gets a
    // resolved identity. Nothing arrives to correct a layout applied from
    // another project's record, which is why it must never be applied.
    window.localStorage.setItem(shellStateKey('default'), STALE)
    const { rerender } = renderProvider(null)
    rerender(
      <ShellStateProvider
        projectIdentity={undefined}
        panes={DESK_DEFAULTS.panes}
        viewport={{ railIsDrawer: false, inspectorIsDrawer: false }}
      >
        <Panel />
      </ShellStateProvider>
    )
    expect(screen.getByTestId('rail-mode').textContent).toBe('expanded')
    expect(screen.getByTestId('inspector-open').textContent).toBe('false')
  })

  it('reads this project’s record the moment the chassis names it', () => {
    window.localStorage.setItem(KEY, STALE)
    renderProvider()
    expect(screen.getByTestId('rail-mode').textContent).toBe('icons')
    expect(screen.getByTestId('inspector-open').textContent).toBe('true')
  })
})

describe('a reset the storage refuses', () => {
  it('changes nothing at all, which is what Admin says it did', () => {
    // The live layout used to be cleared regardless, so Admin's "the layout is
    // unchanged" was false for the session and the retained record came back
    // on the next reload anyway.
    const backing = new Map<string, string>()
    vi.stubGlobal('localStorage', {
      getItem: (key: string) => backing.get(key) ?? null,
      setItem: (key: string, value: string) => void backing.set(key, value),
      removeItem: () => {},
      clear: () => {}
    })
    renderProvider()
    act(() => screen.getByRole('button', { name: 'toggle the console' }).click())
    act(() => screen.getByRole('button', { name: 'toggle the rail' }).click())
    expect(screen.getByTestId('console-open').textContent).toBe('true')
    expect(screen.getByTestId('rail-mode').textContent).toBe('icons')

    backing.set(KEY, '{"v":1}')
    act(() => screen.getByRole('button', { name: 'reset' }).click())
    // Refused: the panes are exactly where the viewer left them.
    expect(screen.getByTestId('console-open').textContent).toBe('true')
    expect(screen.getByTestId('rail-mode').textContent).toBe('icons')
    vi.unstubAllGlobals()
  })
})
