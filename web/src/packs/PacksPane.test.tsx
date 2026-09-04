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

afterEach(() => {
  cleanup()
  unmeasure?.()
  unmeasure = undefined
})

/**
 * A viewport with a height, for the one case that needs real virtualisation.
 *
 * jsdom lays nothing out, so every other case here takes the hook's
 * render-everything fallback — which is deliberate, and is also why the pane's
 * own keyboard path over a *windowed* list was never exercised: with all the
 * rows rendered, focusing the destination never has to wait for one.
 */
let unmeasure: (() => void) | undefined
function measured(height: number) {
  const original = Object.getOwnPropertyDescriptor(HTMLElement.prototype, 'clientHeight')
  Object.defineProperty(HTMLElement.prototype, 'clientHeight', {
    configurable: true,
    get: () => height
  })
  unmeasure = () => {
    if (original === undefined) delete (HTMLElement.prototype as unknown as Record<string, unknown>).clientHeight
    else Object.defineProperty(HTMLElement.prototype, 'clientHeight', original)
  }
}

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
    // **The destination element itself, not text that contains its name.**
    // Focus lost to `document.body` leaves `activeElement.textContent`
    // carrying every rendered row, so `toContain('b-pack')` passed with focus
    // on nothing at all: taking focus and dropping it again left all 1,043
    // tests green. Identity is the only assertion that says focus is *there*.
    draw(packs(['a-pack', 'b-pack', 'c-pack']))
    const first = await screen.findByRole('link', { name: /a-pack/ })
    const row = (name: string) => screen.getByRole('link', { name: new RegExp(name) })
    first.focus()
    fireEvent.keyDown(first, { key: 'ArrowDown' })
    expect(document.activeElement).toBe(row('b-pack'))
    fireEvent.keyDown(document.activeElement!, { key: 'End' })
    expect(document.activeElement).toBe(row('c-pack'))
    fireEvent.keyDown(document.activeElement!, { key: 'Home' })
    expect(document.activeElement).toBe(row('a-pack'))
    fireEvent.keyDown(document.activeElement!, { key: 'ArrowUp' })
    // Already at the top: focus stays rather than wrapping to the bottom.
    expect(document.activeElement).toBe(row('a-pack'))
  })

  it('reaches the last row of a windowed list, and focuses it there', async () => {
    // **The integrated path, over a list that really is windowed.** `moveFocus`
    // and `scrollRowIntoView` each had a test and the thing between them did
    // not: the destination is not rendered when End is pressed, so it is
    // scrolled to and focused in the render that brings it in. With 300 rows in
    // a 400px viewport that is the ordinary case, and under jsdom's zero
    // heights it never happens at all.
    const many = Array.from({ length: 300 }, (_, index) => `pack-${String(index).padStart(3, '0')}`)
    measured(400)
    draw(packs(many))
    await screen.findByRole('link', { name: /pack-000/ })
    fireEvent.click(screen.getByRole('button', { name: 'Show all 300' }))
    // A screenful and its overscan, not three hundred: this is the arithmetic
    // and not the fallback.
    await waitFor(() => expect(screen.getAllByRole('link').length).toBeLessThan(40))
    expect(screen.queryByRole('link', { name: /pack-299/ })).toBeNull()

    const first = screen.getByRole('link', { name: /pack-000/ })
    first.focus()
    fireEvent.keyDown(first, { key: 'End' })
    // The row that was brought in, and focus on that element — a window whose
    // rows are all inside `document.body` makes a text match true the moment
    // the destination is *rendered*, whether or not anything focused it.
    await waitFor(() =>
      expect(document.activeElement).toBe(screen.getByRole('link', { name: /pack-299/ }))
    )

    fireEvent.keyDown(document.activeElement!, { key: 'Home' })
    await waitFor(() =>
      expect(document.activeElement).toBe(screen.getByRole('link', { name: /pack-000/ }))
    )
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

  it('claims no version for a pack whose document the listing could not read', async () => {
    // `list_packs` still lists such a pack — with `packId` and `packVersion` as
    // **empty strings** — and puts the reason in `detail`. The row used to
    // render a bare "v" beside the name: a version member asserted for a
    // document nothing could read.
    const stub = stubClient({
      list_packs: () => ({
        text: JSON.stringify({
          status: 'valid',
          packs: [
            { id: 'good-pack', packId: 'good-pack', packVersion: '1.0.0' },
            {
              id: 'broken-json',
              packId: '',
              packVersion: '',
              detail: 'The file could not be used: pack document is not acceptable JSON.'
            }
          ]
        })
      })
    })
    draw(stub)
    const row = await screen.findByRole('link', { name: /broken-json/ })
    expect(row.textContent).not.toContain('v')
    // The runtime's own sentence, quoted rather than summarised.
    expect(row.textContent).toContain('not acceptable JSON')
    expect(screen.getByRole('link', { name: /good-pack/ }).textContent).toContain('v1.0.0')
  })

  it('is a named navigation, because it is a list of navigations', async () => {
    draw(packs(['a-pack']))
    expect(screen.getByRole('navigation', { name: 'Packs' })).toBeTruthy()
  })
})

describe('a listing that failed after it had succeeded', () => {
  it('takes its “Show all” offer away with it', async () => {
    // react-query keeps the last good data through a refetch error, so the
    // pane printed the failure sentence and left a button underneath offering
    // to show all N of a listing it had just said it could not read.
    let answer: () => { text: string } = () => ({
      text: JSON.stringify({
        status: 'valid',
        packs: Array.from({ length: 30 }, (_, index) => ({
          id: `pack-${String(index).padStart(2, '0')}`,
          packVersion: '1.0.0'
        }))
      })
    })
    const stub = stubClient({ list_packs: () => answer() })
    const queryClient = testQueryClient()
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
      { initialEntries: ['/packs'] }
    )
    render(
      <QueryClientProvider client={queryClient}>
        <RouterProvider router={router} />
      </QueryClientProvider>
    )
    expect(await screen.findByRole('button', { name: 'Show all 30' })).toBeTruthy()

    // The same listing, refused this time.
    answer = () => {
      throw new Error('the project could not be read')
    }
    await queryClient.refetchQueries({ queryKey: ['list_packs'] })

    await waitFor(() =>
      expect(screen.getByText(/the project could not be read/)).toBeTruthy()
    )
    // And no offer to show all of something the pane cannot read.
    expect(screen.queryByRole('button', { name: /Show all/ })).toBeNull()
  })

  it('comes back when the same listing answers again', async () => {
    // The retry mounts a new list node at the same length. The scroll listener
    // used to stay on the detached one, so the pane came back and scrolled no
    // more; `useWindowedRows.test.tsx` holds that half directly.
    let fail = false
    const rows = Array.from({ length: 30 }, (_, index) => ({
      id: `pack-${String(index).padStart(2, '0')}`,
      packVersion: '1.0.0'
    }))
    const stub = stubClient({
      list_packs: () => {
        if (fail) throw new Error('the project could not be read')
        return { text: JSON.stringify({ status: 'valid', packs: rows }) }
      }
    })
    const queryClient = testQueryClient()
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
      { initialEntries: ['/packs'] }
    )
    render(
      <QueryClientProvider client={queryClient}>
        <RouterProvider router={router} />
      </QueryClientProvider>
    )
    await screen.findByRole('button', { name: 'Show all 30' })

    fail = true
    await queryClient.refetchQueries({ queryKey: ['list_packs'] })
    await waitFor(() => expect(screen.queryByRole('button', { name: /Show all/ })).toBeNull())

    fail = false
    await queryClient.refetchQueries({ queryKey: ['list_packs'] })
    await waitFor(() =>
      expect(screen.getByRole('button', { name: 'Show all 30' })).toBeTruthy()
    )
    expect(screen.getByRole('link', { name: /pack-00/ })).toBeTruthy()
  })
})
