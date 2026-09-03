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
import { afterEach, describe, expect, it } from 'vitest'
import { ShellStateProvider, projectKey, shellStateKey, useShellState } from './paneState'

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
      <button type="button" onClick={() => shell.resetPanes()}>
        reset
      </button>
      <p data-testid="key-resolved">{String(shell.keyResolved)}</p>
      <p data-testid="console-open">{String(shell.console.open)}</p>
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
