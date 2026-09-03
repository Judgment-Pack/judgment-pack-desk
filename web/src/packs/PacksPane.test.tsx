/**
 * The packs pane, which is where the rail's list went.
 *
 * The three cases the rail used to hold move here — the list, its cap, and the
 * refused listing — and three arrive with the pane: the filter, the sort, and
 * the windowing fallback that makes all of the others visible at all.
 */
import { cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react'
import { QueryClientProvider } from '@tanstack/react-query'
import { RouterProvider, createMemoryRouter } from 'react-router-dom'
import { afterEach, describe, expect, it } from 'vitest'
import { McpContext } from '../mcp/McpProvider'
import { connected, stubClient, testQueryClient } from '../testing/harness'
import { PacksPane } from './PacksPane'

afterEach(cleanup)

function packs(ids: string[]) {
  return stubClient({
    list_packs: () => ({
      text: JSON.stringify({
        status: 'valid',
        packs: ids.map((id) => ({ id, packVersion: '1.0.0' }))
      })
    })
  })
}

function draw(stub: ReturnType<typeof stubClient>, path = '/packs') {
  const router = createMemoryRouter(
    [
      {
        path: '*',
        element: (
          <McpContext.Provider value={connected({ client: stub.client })}>
            <PacksPane />
          </McpContext.Provider>
        )
      }
    ],
    { initialEntries: [path] }
  )
  return render(
    <QueryClientProvider client={testQueryClient()}>
      <RouterProvider router={router} />
    </QueryClientProvider>
  )
}

describe('the packs pane', () => {
  it('lists the project’s packs with their versions, and links each one', async () => {
    draw(packs(['intake-triage', 'vendor-onboarding']))
    const link = await screen.findByRole('link', { name: /intake-triage/ })
    expect(link.getAttribute('href')).toBe('/packs/intake-triage')
    expect(link.textContent).toContain('v1.0.0')
    expect(screen.getByRole('link', { name: /vendor-onboarding/ })).toBeTruthy()
  })

  it('renders every row where no viewport can be measured', async () => {
    // jsdom performs no layout, so every measured height is zero. A window
    // computed from zero renders zero rows, and every case in this file would
    // then assert against an empty list and pass for the wrong reason. This is
    // the fallback that makes the rest of the suite mean anything.
    const many = Array.from({ length: 18 }, (_, index) => `pack-${index}`)
    draw(packs(many))
    await screen.findByRole('link', { name: /pack-0/ })
    expect(screen.getAllByRole('link')).toHaveLength(18)
    expect(screen.getByRole('link', { name: /pack-17/ })).toBeTruthy()
  })

  it('narrows by a substring of the id', async () => {
    draw(packs(['intake-triage', 'vendor-onboarding', 'access-review']))
    await screen.findByRole('link', { name: /intake-triage/ })
    fireEvent.change(screen.getByLabelText('Filter'), { target: { value: 'ven' } })
    await waitFor(() => expect(screen.getAllByRole('link')).toHaveLength(1))
    expect(screen.getByRole('link', { name: /vendor-onboarding/ })).toBeTruthy()

    // A filter that matches nothing is not an empty project.
    fireEvent.change(screen.getByLabelText('Filter'), { target: { value: 'zzz' } })
    await screen.findByText('No pack id contains that.')
  })

  it('reorders on the sort control, and offers only orders it has data for', async () => {
    draw(packs(['b-pack', 'a-pack', 'c-pack']))
    await screen.findByRole('link', { name: /a-pack/ })
    const names = () => screen.getAllByRole('link').map((link) => link.textContent)
    expect(names()[0]).toContain('a-pack')

    fireEvent.keyDown(screen.getByLabelText('Sort'), { key: 'Enter' })
    const options = await screen.findAllByRole('option')
    // Name ascending and descending, and nothing else: `list_packs` reports no
    // date and no size, so any other order would be the desk inventing one.
    expect(options.map((option) => option.textContent)).toEqual(['Name A–Z', 'Name Z–A'])
    fireEvent.click(options[1]!)
    await waitFor(() => expect(names()[0]).toContain('c-pack'))
  })

  it('holds the rest back behind “Show all N”', async () => {
    const many = Array.from({ length: 26 }, (_, index) => `pack-${String(index).padStart(2, '0')}`)
    draw(packs(many))
    await screen.findByRole('link', { name: /pack-00/ })
    expect(screen.getAllByRole('link')).toHaveLength(20)
    fireEvent.click(screen.getByRole('button', { name: 'Show all 26' }))
    await waitFor(() => expect(screen.getAllByRole('link')).toHaveLength(26))
  })

  it('moves focus between rows with the arrow keys, Home and End', async () => {
    draw(packs(['a-pack', 'b-pack', 'c-pack']))
    const first = await screen.findByRole('link', { name: /a-pack/ })
    first.focus()
    fireEvent.keyDown(first, { key: 'ArrowDown' })
    expect(document.activeElement?.textContent).toContain('b-pack')
    fireEvent.keyDown(document.activeElement!, { key: 'End' })
    expect(document.activeElement?.textContent).toContain('c-pack')
    fireEvent.keyDown(document.activeElement!, { key: 'Home' })
    expect(document.activeElement?.textContent).toContain('a-pack')
    fireEvent.keyDown(document.activeElement!, { key: 'ArrowUp' })
    // Already at the top: focus stays rather than wrapping to the bottom.
    expect(document.activeElement?.textContent).toContain('a-pack')
  })

  it('shows a refused listing as the failure, not as an empty project', async () => {
    const stub = stubClient({
      list_packs: () => {
        throw new Error('the runtime refused the listing')
      }
    })
    draw(stub)
    await screen.findByText(/the runtime refused the listing/)
    expect(screen.queryByText('This project declares no packs.')).toBeNull()
  })

  it('says a project declares none where the listing said so', async () => {
    draw(packs([]))
    await screen.findByText('This project declares no packs.')
  })

  it('is a named navigation, because it is a list of navigations', async () => {
    draw(packs(['a-pack']))
    expect(screen.getByRole('navigation', { name: 'Packs' })).toBeTruthy()
  })
})
