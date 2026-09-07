/**
 * The four project-file forms, as a reader drives them.
 *
 * `projectFileSave.test.ts` proves what is composed and `projectFileWrite.test.tsx`
 * proves what leaves the browser; this is the part in between — what a form
 * offers, what it refuses to offer, and what it does with a value somebody
 * typed when a save comes back refused.
 *
 * **Reload keeps the unsaved values**, which is the property a stale write
 * exists to make possible: nothing was written, the draft is intact, and the
 * only thing that has to change is the digest the next Save states. A form that
 * re-seeded from the fresh read would answer a refusal by discarding the work
 * the refusal protected.
 */
import { QueryClientProvider } from '@tanstack/react-query'
import type { ReactElement } from 'react'
import { act, cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react'
import { afterEach, describe, expect, it, vi } from 'vitest'
import { DeskConfigProvider, useEffectiveConfig } from '../config/DeskConfigProvider'
import { testQueryClient } from '../testing/harness'
import {
  AppearanceForm,
  OrganizationForm,
  PanesForm,
  StorageForm
} from './projectFileCards'
import { FROM_THE_DESK_FILE } from './useProjectFileSave'

afterEach(() => {
  cleanup()
  vi.unstubAllGlobals()
})

const ON_DISK = `{
  "deskConfigVersion": 1,
  "organization": { "name": "Unveil", "mark": null }
}
`
/** The same file after somebody else edited it, which a reload finds. */
const MOVED = ON_DISK.replace('"Unveil"', '"Renamed elsewhere"')

interface Response {
  ok: boolean
  status: number
  statusText: string
  text: () => Promise<string>
}

function answered(body: unknown, status = 200): Response {
  return {
    ok: status >= 200 && status < 300,
    status,
    statusText: '',
    text: async () => JSON.stringify(body)
  }
}

/**
 * A desk whose project file is `ON_DISK` on the first read and `MOVED` on
 * every one after it, and whose write is refused as stale.
 */
function servesAMovedFile(deskFile?: object): {
  puts: number
  bodies: Record<string, unknown>[]
} {
  const seen = { puts: 0, reads: 0, bodies: [] as Record<string, unknown>[] }
  vi.stubGlobal('fetch', async (url: string, init?: RequestInit) => {
    if (url.includes('/api/desk-config')) {
      return answered({
        path: '/home/someone/.config/jpack-desk/desk.json',
        present: deskFile !== undefined,
        sha256: deskFile === undefined ? '' : 'd'.repeat(64),
        content:
          deskFile === undefined
            ? undefined
            : JSON.stringify({ deskConfigVersion: 1, ...deskFile }),
        project: { dir: '/p', file: '/p/jpack-desk.json' },
        runtime: { bin: 'jpack' }
      })
    }
    if (init?.method === 'PUT') {
      seen.puts += 1
      seen.bodies.push(JSON.parse(String(init.body)) as Record<string, unknown>)
      return answered(
        {
          error: 'the file on disk is not the file this edit started from',
          code: 'stale',
          path: 'jpack-desk.json',
          expectedSha256: 'a'.repeat(64),
          actualSha256: 'c'.repeat(64),
          exists: true
        },
        409
      )
    }
    seen.reads += 1
    const content = seen.reads === 1 ? ON_DISK : MOVED
    return answered({
      path: 'jpack-desk.json',
      bytes: content.length,
      sha256: seen.reads === 1 ? 'a'.repeat(64) : 'c'.repeat(64),
      content
    })
  })
  return seen
}

/**
 * What revision the **page** is on, which every case below has to be able to
 * see.
 *
 * Two of these cases proved nothing without it. A card that follows the
 * watcher and one that holds its revision look identical if the click lands
 * before the newer read has reached the component at all; and the stale notice
 * disappears the instant Reload is pressed, so waiting for it to go is waiting
 * for a button press rather than for a read. The digest the page is holding is
 * the one thing that says which of those has happened.
 */
function Spy() {
  const effective = useEffectiveConfig()
  return <span data-testid="live">{effective.sha256 ?? 'none'}</span>
}

function renderForm(form: ReactElement) {
  const client = testQueryClient()
  return {
    client,
    ...render(
      <QueryClientProvider client={client}>
        <DeskConfigProvider>
          <Spy />
          {form}
        </DeskConfigProvider>
      </QueryClientProvider>
    )
  }
}

/**
 * A desk with two revisions of the project file and one refusal between them.
 *
 * The first read answers `first`; the first write is refused as stale; every
 * read after that answers `second`, which is what another writer left on disk.
 * The writes after the refusal land, and their bodies are kept — that second
 * body is what the concurrent-edit cases are about.
 */
function servesTwoRevisions(first: string, second: string): {
  bodies: Record<string, unknown>[]
} {
  const seen = { reads: 0, bodies: [] as Record<string, unknown>[] }
  vi.stubGlobal('fetch', async (url: string, init?: RequestInit) => {
    if (url.includes('/api/desk-config')) {
      return answered({
        path: '/home/someone/.config/jpack-desk/desk.json',
        present: false,
        sha256: '',
        project: { dir: '/p', file: '/p/jpack-desk.json' },
        runtime: { bin: 'jpack' }
      })
    }
    if (init?.method === 'PUT') {
      const body = JSON.parse(String(init.body)) as Record<string, unknown>
      if (seen.bodies.length === 0) {
        seen.bodies.push(body)
        return answered(
          {
            error: 'the file on disk is not the file this edit started from',
            code: 'stale',
            path: 'jpack-desk.json',
            expectedSha256: 'a'.repeat(64),
            actualSha256: 'b'.repeat(64),
            exists: true
          },
          409
        )
      }
      seen.bodies.push(body)
      const content = String(body.content)
      return answered({
        path: 'jpack-desk.json',
        bytes: content.length,
        sha256: 'e'.repeat(64),
        content
      })
    }
    seen.reads += 1
    const content = seen.reads === 1 ? first : second
    return answered({
      path: 'jpack-desk.json',
      bytes: content.length,
      sha256: seen.reads === 1 ? 'a'.repeat(64) : 'b'.repeat(64),
      content
    })
  })
  return seen
}

/** The member one card wrote, as the request carried it. */
function memberOf(body: Record<string, unknown>, name: string): unknown {
  return (JSON.parse(String(body.content)) as Record<string, unknown>)[name]
}

describe('a project-file card’s form', () => {
  it('keeps every unsaved value when Reload takes a fresh read', async () => {
    const desk = servesAMovedFile()
    renderForm(<OrganizationForm />)
    const name = await screen.findByDisplayValue('Unveil')
    fireEvent.change(name, { target: { value: 'What I typed' } })
    fireEvent.click(screen.getByRole('button', { name: 'Save' }))

    // Refused, and nothing was written.
    await screen.findByText('The file changed on disk — nothing was written.')
    expect(desk.puts).toBe(1)
    expect(screen.getByDisplayValue('What I typed')).toBeTruthy()

    fireEvent.click(screen.getByRole('button', { name: 'Reload' }))
    // **Wait for the read to land, not for the notice to go.** The notice goes
    // the instant Reload is pressed, so asserting on it proves only that the
    // button was pressed — and a form that took the fresh read over the draft
    // would look exactly like one that keeps it.
    await waitFor(() => expect(screen.getByTestId('live').textContent).toBe('c'.repeat(64)))
    expect(screen.queryByText('The file changed on disk — nothing was written.')).toBeNull()
    // The file on disk now says something else, and the value that was typed is
    // still in the field.
    expect(screen.getByDisplayValue('What I typed')).toBeTruthy()
    expect(screen.queryByDisplayValue('Renamed elsewhere')).toBeNull()
  })

  /**
   * **The review's own sequence, on the card it was found on.**
   *
   * Edit Name; another writer adds a mark on disk; the Save is refused as
   * stale; Reload; Save again. The second request has to carry that mark — a
   * form holding a whole snapshot instead of the fields somebody typed into
   * writes `mark: null` here, erasing a change nobody on this page ever saw,
   * under a digest that is now perfectly true.
   */
  it('preserves a member another writer added while one field was being edited', async () => {
    const before = `{\n  "deskConfigVersion": 1,\n  "organization": { "name": "Unveil", "mark": null }\n}\n`
    const after = before.replace('"mark": null', '"mark": "<svg/>"')
    const desk = servesTwoRevisions(before, after)
    renderForm(<OrganizationForm />)
    fireEvent.change(await screen.findByDisplayValue('Unveil'), {
      target: { value: 'Renamed here' }
    })
    fireEvent.click(screen.getByRole('button', { name: 'Save' }))
    await screen.findByText('The file changed on disk — nothing was written.')
    expect(memberOf(desk.bodies[0]!, 'organization')).toEqual({
      name: 'Renamed here',
      mark: null
    })

    fireEvent.click(screen.getByRole('button', { name: 'Reload' }))
    await waitFor(() => expect(screen.getByTestId('live').textContent).toBe('b'.repeat(64)))
    // The field somebody typed into is theirs; the one they did not touch is
    // now whatever the file says.
    expect(screen.getByDisplayValue('Renamed here')).toBeTruthy()
    expect(screen.getByDisplayValue('<svg/>')).toBeTruthy()

    fireEvent.click(screen.getByRole('button', { name: 'Save' }))
    await waitFor(() => expect(desk.bodies).toHaveLength(2))
    expect(desk.bodies[1]!.baseSha256).toBe('b'.repeat(64))
    expect(memberOf(desk.bodies[1]!, 'organization')).toEqual({
      name: 'Renamed here',
      mark: '<svg/>'
    })
  })

  /**
   * The same claim on each of the four cards, because the rule lives in the
   * hook they share and a card that stopped using it would be the one place it
   * did not hold.
   *
   * Storage's first field is Kind, whose union has one member — there is no
   * other value to type — so the field touched there is the next one.
   */
  it.each([
    [
      'Organization',
      <OrganizationForm key="o" />,
      '"organization": { "name": "Unveil", "mark": null }',
      '"organization": { "name": "Unveil", "mark": "<svg/>" }',
      async () =>
        fireEvent.change(await screen.findByDisplayValue('Unveil'), {
          target: { value: 'Typed' }
        }),
      'organization',
      { name: 'Typed', mark: '<svg/>' }
    ],
    [
      'Appearance',
      <AppearanceForm key="a" />,
      '"appearance": { "theme": "system", "density": "comfortable" }',
      '"appearance": { "theme": "system", "density": "compact" }',
      async () => {
        fireEvent.click(await screen.findByRole('combobox', { name: 'Theme' }))
        fireEvent.click(await screen.findByRole('option', { name: 'dark' }))
      },
      'appearance',
      { theme: 'dark', density: 'compact' }
    ],
    [
      'Panes',
      <PanesForm key="p" />,
      '"panes": { "left": { "mode": "expanded", "width": 248 }, "inspector": { "open": false, "width": 360 } }',
      '"panes": { "left": { "mode": "expanded", "width": 248 }, "inspector": { "open": false, "width": 400 } }',
      async () =>
        fireEvent.change(await screen.findByLabelText('Rail width'), {
          target: { value: '300' }
        }),
      'panes',
      {
        left: { mode: 'expanded', width: 300 },
        inspector: { open: false, width: 400 }
      }
    ],
    [
      'Storage',
      <StorageForm key="s" dirSays="holds files" />,
      '"storage": { "packs": { "dir": "packs", "idBase": "https://acme.example/d/" } }',
      '"storage": { "packs": { "dir": "packs", "idBase": "https://acme.example/other/" } }',
      async () =>
        fireEvent.change(await screen.findByLabelText('Packs go to'), {
          target: { value: 'decisions' }
        }),
      'storage',
      { packs: { dir: 'decisions', idBase: 'https://acme.example/other/' } }
    ]
  ] as const)(
    'writes only the touched field on the %s card, whatever else moved',
    async (_card, form, before, after, touch, member, expected) => {
      const desk = servesTwoRevisions(
        `{\n  "deskConfigVersion": 1,\n  ${before}\n}\n`,
        `{\n  "deskConfigVersion": 1,\n  ${after}\n}\n`
      )
      renderForm(form)
      // **The first read has to land before anything is typed.** The labels
      // are on screen from the first paint, holding the built-in defaults, and
      // a Save pressed there is refused for having no bytes to write over — so
      // a case that typed straight away would prove nothing about what it
      // wrote.
      await waitFor(() => expect(screen.getByTestId('live').textContent).toBe('a'.repeat(64)))
      await touch()
      fireEvent.click(screen.getByRole('button', { name: 'Save' }))
      await screen.findByText('The file changed on disk — nothing was written.')
      fireEvent.click(screen.getByRole('button', { name: 'Reload' }))
      await waitFor(() => expect(screen.getByTestId('live').textContent).toBe('b'.repeat(64)))
      fireEvent.click(screen.getByRole('button', { name: 'Save' }))
      await waitFor(() => expect(desk.bodies).toHaveLength(2))
      expect(memberOf(desk.bodies[1]!, member)).toEqual(expected)
    }
  )

  it('offers no Save where the value comes from the desk-level file', async () => {
    // The card's Location names the file the value came from, and this page
    // has no write route for that one — so writing the project file here would
    // write a file the card does not name.
    servesAMovedFile({ storage: { packs: { dir: 'elsewhere' } } })
    const { container } = renderForm(<StorageForm dirSays="holds files" />)
    expect(await screen.findByText(FROM_THE_DESK_FILE)).toBeTruthy()
    expect((screen.getByRole('button', { name: 'Save' }) as HTMLButtonElement).disabled).toBe(true)
    // And the fields are shown rather than edited: the value in them is the
    // other file's, and typing over it would compose a write over this one.
    expect(container.querySelector('fieldset')?.disabled).toBe(true)
    expect(screen.getByDisplayValue('elsewhere')).toBeTruthy()
  })

  it('states the revision it was composed against, not the one that arrived under it', async () => {
    // The chassis watches the project and invalidates every query when it sees
    // this file change. A digest read off that query would move onto bytes
    // nobody saw, and this Save would then overwrite somebody's edit with no
    // refusal at all — which is the whole of what the conditional commit is
    // for. The bytes and the digest are held together and move only where the
    // reader acts.
    const desk = servesAMovedFile()
    const { client } = renderForm(<OrganizationForm />)
    const name = await screen.findByDisplayValue('Unveil')
    fireEvent.change(name, { target: { value: 'What I typed' } })

    // The file changes on disk and the desk says so: every query is refetched.
    await act(async () => {
      await client.invalidateQueries()
    })
    // **And the page has seen it.** Without this the case proves nothing: the
    // click lands before the newer revision ever reaches the component, and a
    // card that follows the watcher looks exactly like one that does not.
    await waitFor(() => expect(screen.getByTestId('live').textContent).toBe('c'.repeat(64)))
    // The field is untouched by that, and so is the revision behind it.
    expect(screen.getByDisplayValue('What I typed')).toBeTruthy()

    fireEvent.click(screen.getByRole('button', { name: 'Save' }))
    await waitFor(() => expect(desk.puts).toBe(1))
    const body = desk.bodies[0]!
    // The digest of the bytes this edit started from — so the desk refuses it —
    // and the member spliced into **those** bytes rather than the newer ones.
    expect(body.baseSha256).toBe('a'.repeat(64))
    expect(String(body.content)).toContain('"name":"What I typed"')
    expect(String(body.content)).not.toContain('Renamed elsewhere')
  })

  it('disables Save until the draft differs from the file, and again once it matches', async () => {
    servesAMovedFile()
    renderForm(<OrganizationForm />)
    const name = await screen.findByDisplayValue('Unveil')
    const save = screen.getByRole('button', { name: 'Save' }) as HTMLButtonElement
    expect(save.disabled).toBe(true)
    fireEvent.change(name, { target: { value: 'Something' } })
    expect(save.disabled).toBe(false)
    // Compared rather than remembered: an edit somebody undid is not an edit.
    fireEvent.change(name, { target: { value: 'Unveil' } })
    expect(save.disabled).toBe(true)
  })
})
