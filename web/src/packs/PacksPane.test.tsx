import {DeskNotices, DeskNoticesProvider} from '../shell/DeskNotices'
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
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { McpContext } from '../mcp/McpProvider'
import { connected, stubClient, testQueryClient } from '../testing/harness'
import { PacksPane } from './PacksPane'

function PaneWithNotices() {return <DeskNoticesProvider><PacksPane/><footer><DeskNotices/></footer></DeskNoticesProvider>}

const packLinks = () => screen.queryAllByRole('link').filter(link => link.getAttribute('href')?.startsWith('/packs/') && link.getAttribute('href') !== '/packs/new')
beforeEach(()=>vi.stubGlobal('fetch',async(input:RequestInfo|URL)=>{
  if(['/api/input-preparations','/api/dialect-drafts'].includes(String(input)))return Response.json({items:[]})
  throw new Error('No file fixture for this test.')
}))

afterEach(() => {
  cleanup()
  unmeasure?.()
  unmeasure = undefined
  vi.unstubAllGlobals()
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

function draw(stub: ReturnType<typeof stubClient>, path = '/library?type=pack', graphInventorySupported = false) {
  const router = createMemoryRouter(
    [
      {
        path: '*',
        element: (
          <McpContext.Provider value={connected({ client: stub.client, graphInventorySupported })}>
            <PaneWithNotices/>
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
    expect(link.textContent).toContain('1.0.0')
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
    expect(packLinks()).toHaveLength(18)
    expect(screen.getByRole('link', { name: /pack-17/ })).toBeTruthy()
  })

  it('narrows by a substring of the id', async () => {
    draw(packs(['intake-triage', 'vendor-onboarding', 'access-review']))
    await screen.findByRole('link', { name: /intake-triage/ })
    fireEvent.change(screen.getByLabelText('Search library'), { target: { value: 'ven' } })
    await waitFor(() => expect(packLinks()).toHaveLength(1))
    expect(screen.getByRole('link', { name: /vendor-onboarding/ })).toBeTruthy()

    // A filter that matches nothing is not an empty project.
    fireEvent.change(screen.getByLabelText('Search library'), { target: { value: 'zzz' } })
    await screen.findByText('No matching library items')
  })

  it('reorders on the sort control, and offers only orders it has data for', async () => {
    draw(packs(['b-pack', 'a-pack', 'c-pack']))
    await screen.findByRole('link', { name: /a-pack/ })
    const names = () => packLinks().map((link) => link.textContent)
    expect(names()[0]).toContain('a-pack')

    fireEvent.keyDown(screen.getByLabelText('Sort library'), { key: 'Enter' })
    const options = await screen.findAllByRole('menuitemradio')
    // ID ascending and descending, and nothing else: `list_packs` reports no
    // date and no size, so any other order would be the desk inventing one.
    expect(options.map((option) => option.textContent?.replace('✓', '').trim())).toEqual(['Name: A–Z', 'Name: Z–A'])
    fireEvent.click(options[1]!)
    await waitFor(() => expect(names()[0]).toContain('c-pack'))
  })

  it('makes every pack available without a second expansion step', async () => {
    const many = Array.from({ length: 26 }, (_, index) => `pack-${String(index).padStart(2, '0')}`)
    draw(packs(many))
    await screen.findByRole('link', { name: /pack-00/ })
    expect(screen.queryByRole('button', { name: /Show all/ })).toBeNull()
    await waitFor(() => expect(packLinks()).toHaveLength(26))
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

  it('reaches the last row of a windowed page, and focuses it there', async () => {
    // **The integrated path, over a list that really is windowed.** `moveFocus`
    // and `scrollRowIntoView` each had a test and the thing between them did
    // not: the destination is not rendered when End is pressed, so it is
    // scrolled to and focused in the render that brings it in. With a 50-row page in
    // a 400px viewport that is the ordinary case, and under jsdom's zero
    // heights it never happens at all.
    const many = Array.from({ length: 300 }, (_, index) => `pack-${String(index).padStart(3, '0')}`)
    measured(400)
    draw(packs(many))
    await screen.findByRole('link', { name: /pack-000/ })
    // A screenful and its overscan, not three hundred: this is the arithmetic
    // and not the fallback.
    await waitFor(() => expect(packLinks().length).toBeLessThan(40))
    expect(screen.queryByRole('link', { name: /pack-049/ })).toBeNull()

    const first = screen.getByRole('link', { name: /pack-000/ })
    first.focus()
    fireEvent.keyDown(first, { key: 'End' })
    // The row that was brought in, and focus on that element — a window whose
    // rows are all inside `document.body` makes a text match true the moment
    // the destination is *rendered*, whether or not anything focused it.
    await waitFor(() =>
      expect(document.activeElement).toBe(screen.getByRole('link', { name: /pack-049/ }))
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
    fireEvent.click(await screen.findByRole('button', {name:'Packs unavailable'}))
    await screen.findByText(/the runtime refused the listing/)
    expect(screen.queryByText('No library items yet')).toBeNull()
  })

  it('shows no matches when an empty listing is restricted by type', async () => {
    draw(packs([]), '/library?type=pack')
    await screen.findByText('No matching library items')
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
    expect(screen.getByRole('link', { name: /good-pack/ }).textContent).toContain('1.0.0')
  })

  it('is a named navigation, because it is a list of navigations', async () => {
    draw(packs(['a-pack']))
    await screen.findByRole('link', { name: /a-pack/ })
    expect(screen.getByRole('navigation', { name: 'Library' })).toBeTruthy()
  })
})

describe('a listing that failed after it had succeeded', () => {
  it('retains the last listing with refresh failure in the bottom strip', async () => {
    // A refresh failure retains the last known rows and explains their staleness
    // in the same bottom-strip notice used by the application shell.
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
              <PaneWithNotices/>
            </McpContext.Provider>
          )
        }
      ],
      { initialEntries: ['/library'] }
    )
    render(
      <QueryClientProvider client={queryClient}>
        <RouterProvider router={router} />
      </QueryClientProvider>
    )
    expect(await screen.findByRole('link', { name: /pack-00/ })).toBeTruthy()

    // The same listing, refused this time.
    answer = () => {
      throw new Error('the project could not be read')
    }
    await queryClient.refetchQueries({ queryKey: ['list_packs'] })

    fireEvent.click(await screen.findByRole('button', {name:'Packs unavailable'}))
    expect(await screen.findByText(/the project could not be read/)).toBeTruthy()
    expect(screen.getByText('Packs could not be refreshed. Previously loaded packs may be out of date.')).toBeTruthy()
    expect(packLinks()).toHaveLength(30)
    expect(document.querySelector('[data-collection-footer]')).toBeTruthy()
  })

  it('comes back when the same listing answers again', async () => {
    // A successful refresh clears the failure without replacing the retained list.
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
              <PaneWithNotices/>
            </McpContext.Provider>
          )
        }
      ],
      { initialEntries: ['/library'] }
    )
    render(
      <QueryClientProvider client={queryClient}>
        <RouterProvider router={router} />
      </QueryClientProvider>
    )
    await screen.findByRole('link', { name: /pack-00/ })

    fail = true
    await queryClient.refetchQueries({ queryKey: ['list_packs'] })
    expect(await screen.findByRole('button', {name:'Packs unavailable'})).toBeTruthy()
    expect(packLinks()).toHaveLength(30)

    fail = false
    await queryClient.refetchQueries({ queryKey: ['list_packs'] })
    await waitFor(() =>
      expect(screen.getByRole('link', { name: /pack-00/ })).toBeTruthy()
    )
    expect(screen.getByRole('link', { name: /pack-00/ })).toBeTruthy()
    await waitFor(()=>expect(screen.queryByRole('button',{name:'Packs unavailable'})).toBeNull())
  })
})


it('browses packs and graphs with the same id without confusing their links or running them', async () => {
  const stub = stubClient({
    list_packs: () => ({text: JSON.stringify({status:'valid',packs:[{id:'same',packVersion:'1.0.0'}]})}),
    experimental_list_graphs: () => ({text: JSON.stringify({status:'valid',graphs:[{id:'same',graphVersion:'2.0.0',path:'same.graph.json',rowsDeclared:false}]})})
  })
  draw(stub, '/library', true)
  await waitFor(() => expect(document.querySelector('a[href="/graphs/same"]')).toBeTruthy())
  expect(document.querySelector('a[href="/packs/same"]')).toBeTruthy()
  fireEvent.keyDown(screen.getByRole('button', {name:'Item type'}), {key:'Enter'})
  fireEvent.click(await screen.findByRole('menuitemradio', {name:'Graphs'}))
  expect(document.querySelector('a[href="/packs/same"]')).toBeNull()
  expect(document.querySelector('a[href="/graphs/same"]')).toBeTruthy()
  expect(stub.calls.map(call=>call.name).sort()).toEqual(['experimental_list_graphs','list_packs'])
})
it('composes only explicitly selected saved packs', async () => {
  draw(packs(['alpha','beta']))
  fireEvent.click(await screen.findByRole('checkbox', {name:'Select alpha'}))
  fireEvent.keyDown(screen.getByRole('button',{name:'1 selected'}),{key:'Enter'})
  const link=await screen.findByRole('menuitem', {name:'Compose graph'})
  expect(link.getAttribute('href')).toBe('/graphs?view=compose&pack=alpha')
  fireEvent.keyDown(screen.getByRole('menu'),{key:'Escape'})
  await waitFor(()=>expect(screen.queryByRole('menu')).toBeNull())
  fireEvent.click(screen.getByRole('button', {name:'Clear selection'}))
  expect(screen.queryByRole('link', {name:/Compose graph/})).toBeNull()
})


it('does not claim that an unsupported graph inventory is empty', async () => {
  const stub = packs([])
  draw(stub, '/library?type=graph')
  await screen.findByText('This runtime cannot list saved graphs. Connect a newer runtime to browse all packs and graphs.')
  expect(screen.queryByText('No library items yet')).toBeNull()
  expect(stub.calls.map(call => call.name)).not.toContain('experimental_list_graphs')
})

it('keeps graphs usable when only the pack inventory fails', async () => {
  const stub = stubClient({
    list_packs: () => { throw new Error('pack inventory unavailable') },
    experimental_list_graphs: () => ({text: JSON.stringify({status:'valid',graphs:[{id:'flow',graphVersion:'1.0.0'}]})})
  })
  draw(stub, '/library?type=graph', true)
  expect(await screen.findByRole('link', {name:/flow/})).toHaveProperty('href', expect.stringContaining('/graphs/flow'))
  expect(screen.queryByText('pack inventory unavailable')).toBeNull()
})

it('retains saved graph rows with a refresh warning instead of claiming an empty project', async () => {
  let fail = false
  const stub = stubClient({
    list_packs: () => ({text:'{"status":"valid","packs":[]}'}),
    experimental_list_graphs: () => {
      if (fail) throw new Error('graph inventory unavailable')
      return {text: JSON.stringify({status:'valid',graphs:[{id:'flow',graphVersion:'1.0.0'}]})}
    }
  })
  const queryClient = testQueryClient()
  const router = createMemoryRouter([{path:'*',element:<McpContext.Provider value={connected({client:stub.client,graphInventorySupported:true})}><PaneWithNotices/></McpContext.Provider>}], {initialEntries:['/library?type=graph']})
  render(<QueryClientProvider client={queryClient}><RouterProvider router={router}/></QueryClientProvider>)
  await screen.findByRole('link',{name:/flow/})
  fail = true
  await queryClient.refetchQueries({queryKey:['experimental_list_graphs']})
  fireEvent.click(await screen.findByRole('button',{name:'Graphs unavailable'}))
  await screen.findByText(/graph inventory unavailable/)
  expect(screen.getByText('Graphs could not be refreshed. Previously loaded graphs may be out of date.')).toBeTruthy()
  expect(document.querySelector('a[href="/graphs/flow"]')).toBeTruthy()
  expect(screen.queryByText('No library items yet')).toBeNull()
})


it('moves collection totals to the footer and omits page controls for a small list', async () => {
  draw(packs(['alpha', 'beta', 'gamma']))
  await screen.findByRole('link', { name: /alpha/ })
  expect(document.querySelector('[data-collection-location]')?.textContent).toBe('Library')
  expect(document.querySelector('[data-collection-footer]')?.textContent).toBe('3 items')
  expect(screen.queryByRole('button', { name: 'Next page' })).toBeNull()
  fireEvent.change(screen.getByRole('searchbox'), { target: { value: 'beta' } })
  expect(document.querySelector('[data-collection-footer]')?.textContent).toBe('1 item')
  fireEvent.change(screen.getByRole('searchbox'), { target: { value: 'missing' } })
  expect(document.querySelector('[data-collection-footer]')?.textContent).toBe('0 items')
})

it('pages the sorted matches, keeps selection across pages, and resets on filtering', async () => {
  const ids = Array.from({ length: 113 }, (_, index) => `pack-${String(index).padStart(3, '0')}`)
  const stub = packs(ids)
  draw(stub)
  await screen.findByRole('link', { name: /pack-000/ })
  expect(packLinks()).toHaveLength(50)
  expect(screen.getByText('1–50 of 113 items')).toBeTruthy()
  expect(screen.getByRole('button', { name: 'Previous page' })).toHaveProperty('disabled', true)
  fireEvent.click(screen.getByRole('checkbox', { name: 'Select all matches' }))
  expect(screen.getByRole('button', { name: '113 selected' })).toBeTruthy()
  fireEvent.click(screen.getByRole('button', { name: 'Next page' }))
  expect(screen.getByRole('link', { name: /pack-050/ })).toBeTruthy()
  expect(screen.queryByRole('link', { name: /pack-000/ })).toBeNull()
  expect(screen.getByRole('checkbox', { name: 'Select pack-050' })).toHaveProperty('checked', true)
  expect(screen.getByText('Page 2 of 3')).toBeTruthy()
  fireEvent.click(screen.getByRole('button', { name: 'Next page' }))
  expect(packLinks()).toHaveLength(13)
  expect(screen.getByText('101–113 of 113 items')).toBeTruthy()
  expect(screen.getByRole('button', { name: 'Next page' })).toHaveProperty('disabled', true)
  fireEvent.click(screen.getByRole('checkbox', { name: 'Clear selection' }))
  expect(screen.queryByRole('button', { name: '113 selected' })).toBeNull()
  fireEvent.change(screen.getByRole('searchbox'), { target: { value: 'pack-01' } })
  expect(packLinks()).toHaveLength(10)
  expect(screen.getByRole('link', { name: /pack-010/ })).toBeTruthy()
  expect(screen.queryByRole('button', { name: 'Next page' })).toBeNull()
  fireEvent.change(screen.getByRole('searchbox'), { target: { value: '' } })
  expect(screen.getByText('Page 1 of 3')).toBeTruthy()
  fireEvent.click(screen.getByRole('button', { name: 'Next page' }))
  fireEvent.keyDown(screen.getByLabelText('Sort library'), { key: 'Enter' })
  fireEvent.click(await screen.findByRole('menuitemradio', { name: 'Name: Z–A' }))
  expect(screen.getByText('Page 1 of 3')).toBeTruthy()
  expect(packLinks()[0]?.textContent).toContain('pack-112')
  expect(stub.calls.map(call => call.name)).toEqual(['list_packs'])
})
