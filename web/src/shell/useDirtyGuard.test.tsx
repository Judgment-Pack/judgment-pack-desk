/**
 * The two exits, and the one that must not fire.
 *
 * `?edit` is a search parameter precisely so a mode toggle is not a
 * navigation the blocker sees. That is a claim about the predicate, and it is
 * held here rather than in the editor that depends on it.
 */
import { cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react'
import { RouterProvider, createMemoryRouter, useNavigate } from 'react-router-dom'
import { afterEach, describe, expect, it, vi } from 'vitest'
import { useDirtyGuard } from './useDirtyGuard'

afterEach(() => {
  cleanup()
  vi.unstubAllGlobals()
})

function Guarded({ dirty }: { dirty: boolean }) {
  useDirtyGuard(dirty, 'Leave anyway?')
  const navigate = useNavigate()
  return (
    <>
      <button type="button" onClick={() => navigate('/packs/a?edit=1')}>
        toggle edit
      </button>
      <button type="button" onClick={() => navigate('/packs/a?at=/rules/0')}>
        select a member
      </button>
      <button type="button" onClick={() => navigate('/elsewhere')}>
        go elsewhere
      </button>
      <p>guarded</p>
    </>
  )
}

function draw(dirty: boolean) {
  const router = createMemoryRouter(
    [
      { path: '/packs/:packId', element: <Guarded dirty={dirty} /> },
      { path: '/elsewhere', element: <p>elsewhere</p> }
    ],
    { initialEntries: ['/packs/a'] }
  )
  render(<RouterProvider router={router} />)
  return router
}

describe('the dirty guard', () => {
  it('never asks about a search-parameter change', async () => {
    const confirm = vi.fn(() => true)
    vi.stubGlobal('confirm', confirm)
    const router = draw(true)
    fireEvent.click(screen.getByRole('button', { name: 'toggle edit' }))
    await waitFor(() => expect(router.state.location.search).toBe('?edit=1'))
    fireEvent.click(screen.getByRole('button', { name: 'select a member' }))
    await waitFor(() => expect(router.state.location.search).toContain('at='))
    // Entering edit mode, leaving it, and choosing what to inspect are all one
    // page. A predicate comparing whole locations would ask three times.
    expect(confirm).not.toHaveBeenCalled()
  })

  it('asks about a pathname change while the buffer is dirty', async () => {
    const confirm = vi.fn(() => false)
    vi.stubGlobal('confirm', confirm)
    const router = draw(true)
    fireEvent.click(screen.getByRole('button', { name: 'go elsewhere' }))
    await waitFor(() => expect(confirm).toHaveBeenCalledTimes(1))
    // Refused: the navigation is reset and the editor is still on screen.
    expect(router.state.location.pathname).toBe('/packs/a')
    expect(screen.getByText('guarded')).toBeTruthy()
  })

  it('lets the navigation through when the viewer says so', async () => {
    vi.stubGlobal('confirm', vi.fn(() => true))
    const router = draw(true)
    fireEvent.click(screen.getByRole('button', { name: 'go elsewhere' }))
    await waitFor(() => expect(router.state.location.pathname).toBe('/elsewhere'))
  })

  it('asks nothing at all with a clean buffer', async () => {
    const confirm = vi.fn(() => true)
    vi.stubGlobal('confirm', confirm)
    const router = draw(false)
    fireEvent.click(screen.getByRole('button', { name: 'go elsewhere' }))
    await waitFor(() => expect(router.state.location.pathname).toBe('/elsewhere'))
    expect(confirm).not.toHaveBeenCalled()
  })

  it('installs the browser’s own guard exactly while the buffer is dirty', () => {
    const add = vi.spyOn(window, 'addEventListener')
    const remove = vi.spyOn(window, 'removeEventListener')
    const { unmount } = render(
      <RouterProvider
        router={createMemoryRouter([{ path: '/', element: <Guarded dirty /> }], {
          initialEntries: ['/']
        })}
      />
    )
    expect(add.mock.calls.some(([event]) => event === 'beforeunload')).toBe(true)
    unmount()
    expect(remove.mock.calls.some(([event]) => event === 'beforeunload')).toBe(true)
    add.mockRestore()
    remove.mockRestore()
  })
})
