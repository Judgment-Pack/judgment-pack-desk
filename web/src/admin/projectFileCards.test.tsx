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
import { NO_CONTROL_CHARACTERS } from '../config/deskConfig'
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

/** A desk that reads one revision and lets every write land on it. */
function servesLandingWrites(content: string): { bodies: Record<string, unknown>[] } {
  const seen = { bodies: [] as Record<string, unknown>[], latest: content, digest: 'a'.repeat(64) }
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
      seen.bodies.push(body)
      seen.latest = String(body.content)
      seen.digest = 'b'.repeat(64)
    }
    return answered({
      path: 'jpack-desk.json',
      bytes: seen.latest.length,
      sha256: seen.digest,
      content: seen.latest
    })
  })
  return seen
}

/**
 * A desk that refuses the write as stale and then cannot be read again.
 *
 * The first read answers; the write is a 409; every read after that is a 413,
 * which is the chassis speaking about a file it found and could not use.
 */
function servesRefusedThenUnreadable(): { puts: number } {
  const seen = { puts: 0, reads: 0 }
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
      seen.puts += 1
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
    if (seen.reads > 1) {
      return answered({ error: 'the file is too large to read', code: 'too-large' }, 413)
    }
    return answered({
      path: 'jpack-desk.json',
      bytes: ON_DISK.length,
      sha256: 'a'.repeat(64),
      content: ON_DISK
    })
  })
  return seen
}

/**
 * A desk with three revisions of the project file: one read, one refusal, and
 * then whatever the caller asks for next.
 *
 * `next(content, digest)` moves what a later read answers, which is how a case
 * puts a *third* revision underneath a form that has already reloaded onto the
 * second.
 */
function servesThreeRevisions(first: string, second: string): {
  bodies: Record<string, unknown>[]
  next: (content: string, digest: string) => void
} {
  const seen = {
    reads: 0,
    bodies: [] as Record<string, unknown>[],
    later: second,
    digest: 'b'.repeat(64)
  }
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
    seen.reads += 1
    const content = seen.reads === 1 ? first : seen.later
    return answered({
      path: 'jpack-desk.json',
      bytes: content.length,
      sha256: seen.reads === 1 ? 'a'.repeat(64) : seen.digest,
      content
    })
  })
  return {
    get bodies() {
      return seen.bodies
    },
    next: (content: string, digest: string) => {
      seen.later = content
      seen.digest = digest
    }
  }
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

  /**
   * **A save that lands is over**, and the normalisation is what makes that
   * worth asserting. The decoder adds the separator an `idBase` was missing and
   * takes the one a `dir` ended with, so the file afterwards says something the
   * reader did not type — and a form still holding the raw input would stay
   * dirty for ever over a save that succeeded, offering to write again what the
   * file already says and never showing the value its own hint promised.
   */
  it('shows the decoded value a landed save produced, and disables Save', async () => {
    const before = `{\n  "deskConfigVersion": 1,\n  "storage": { "packs": { "dir": "packs", "idBase": "https://acme.example/d/" } }\n}\n`
    const desk = servesLandingWrites(before)
    renderForm(<StorageForm dirSays="holds files" />)
    await waitFor(() => expect(screen.getByTestId('live').textContent).toBe('a'.repeat(64)))

    // Two values the decoder accepts and then normalises.
    fireEvent.change(screen.getByLabelText('Id prefix'), {
      target: { value: 'https://acme.example/packs' }
    })
    fireEvent.change(screen.getByLabelText('Packs go to'), { target: { value: 'decisions/' } })
    fireEvent.click(screen.getByRole('button', { name: 'Save' }))
    await waitFor(() => expect(desk.bodies).toHaveLength(1))

    // What the file says now, which is not what was typed.
    await waitFor(() => expect(screen.getByDisplayValue('https://acme.example/packs/')).toBeTruthy())
    expect(screen.getByDisplayValue('decisions')).toBeTruthy()
    expect((screen.getByRole('button', { name: 'Save' }) as HTMLButtonElement).disabled).toBe(true)
  })

  /**
   * **The refusal belongs at the Save, not at the create that eventually
   * fails.** A `dir` carrying a NUL used to decode clean: Admin reported it as
   * the pack location, and every later create failed at the chassis with a
   * sentence about a path nobody chose to look at.
   */
  it('refuses a pack location the desk could never write, before it is written', async () => {
    const before = `{\n  "deskConfigVersion": 1,\n  "storage": { "packs": { "dir": "packs", "idBase": "https://acme.example/d/" } }\n}\n`
    const desk = servesLandingWrites(before)
    renderForm(<StorageForm dirSays="holds files" />)
    await waitFor(() => expect(screen.getByTestId('live').textContent).toBe('a'.repeat(64)))

    fireEvent.change(screen.getByLabelText('Packs go to'), {
      target: { value: 'packs\u0000hidden' }
    })
    fireEvent.click(screen.getByRole('button', { name: 'Save' }))
    // The decoder's own sentence, on the field its key path names — read
    // through the field's own error element rather than by text, because the
    // hint states the same rule and both are meant to.
    const field = screen.getByLabelText('Packs go to')
    await waitFor(() => expect(field.getAttribute('aria-invalid')).toBe('true'))
    const error = document.getElementById(`${field.id}-error`)
    expect(error?.textContent).toContain(NO_CONTROL_CHARACTERS)
    // And nothing was written: the desk was never asked to.
    expect(desk.bodies).toEqual([])
  })

  /**
   * **A reload that failed changed nothing, so nothing it was about may go.**
   * The refusal used to be cleared on the button press: a read that then
   * failed left the card with only the read's own error, no digests and no
   * Reload, while the revision behind it had not moved — so the next Save was
   * refused again for a reason nothing on screen still said.
   */
  it('keeps the refusal and its digests when the reload itself fails', async () => {
    const desk = servesRefusedThenUnreadable()
    renderForm(<OrganizationForm />)
    fireEvent.change(await screen.findByDisplayValue('Unveil'), {
      target: { value: 'What I typed' }
    })
    fireEvent.click(screen.getByRole('button', { name: 'Save' }))
    await screen.findByText('The file changed on disk — nothing was written.')

    fireEvent.click(screen.getByRole('button', { name: 'Reload' }))
    // The read fails, and says so — beside the refusal, which still stands.
    expect(await screen.findByText(/the file is too large to read/)).toBeTruthy()
    expect(screen.getByText('The file changed on disk — nothing was written.')).toBeTruthy()
    fireEvent.click(screen.getByText('digests'))
    // Two announcements, which is the shape: the refusal that still stands,
    // and beside it the read that could not replace it.
    const [panel, beside] = screen.getAllByRole('alert')
    expect(panel!.textContent).toContain('a'.repeat(12))
    expect(panel!.textContent).toContain('c'.repeat(12))
    expect(beside!.textContent).toContain('the file is too large to read')
    // Reload is still there to press, and the field still holds the draft.
    expect(screen.getByRole('button', { name: 'Reload' })).toBeTruthy()
    expect(screen.getByDisplayValue('What I typed')).toBeTruthy()
    expect(desk.puts).toBe(1)
  })

  /**
   * **A field the file has caught up with is not a field anybody is holding.**
   * Round 2 found the entry surviving the agreement: hold `B`, take a 409,
   * Reload finds `B` and the form goes clean — and then another writer makes it
   * `C`. Without pruning, the retained `B` resurfaces as dirty against the newer
   * seed and offers to write it over `C`, with nobody having typed anything
   * since `B` became the accepted value.
   */
  it.each([
    [
      'a top-level field',
      <OrganizationForm key="o" />,
      (name: string) =>
        `{\n  "deskConfigVersion": 1,\n  "organization": { "name": ${JSON.stringify(name)}, "mark": null }\n}\n`,
      'Name',
      'A',
      'B',
      'C'
    ],
    [
      'a nested leaf',
      <PanesForm key="p" />,
      (width: string) =>
        `{\n  "deskConfigVersion": 1,\n  "panes": { "left": { "mode": "expanded", "width": ${width} } }\n}\n`,
      'Rail width',
      '248',
      '300',
      '360'
    ]
  ] as const)(
    'follows a later revision of %s it has caught up with, and writes nothing',
    async (_where, form, file, label, first, typed, later) => {
      const desk = servesThreeRevisions(file(first), file(typed))
      const { client } = renderForm(form)
      await waitFor(() => expect(screen.getByTestId('live').textContent).toBe('a'.repeat(64)))

      // Typed, refused as stale, and then the file turns out to say exactly
      // what was typed.
      fireEvent.change(screen.getByLabelText(label), { target: { value: typed } })
      fireEvent.click(screen.getByRole('button', { name: 'Save' }))
      await screen.findByText('The file changed on disk — nothing was written.')
      fireEvent.click(screen.getByRole('button', { name: 'Reload' }))
      await waitFor(() => expect(screen.getByTestId('live').textContent).toBe('b'.repeat(64)))
      expect((screen.getByLabelText(label) as HTMLInputElement).value).toBe(typed)
      expect((screen.getByRole('button', { name: 'Save' }) as HTMLButtonElement).disabled).toBe(true)
      const refused = desk.bodies.length

      // A third revision, from another writer, on the same field.
      desk.next(file(later), 'c'.repeat(64))
      await act(async () => {
        await client.invalidateQueries()
      })
      await waitFor(() => expect(screen.getByTestId('live').textContent).toBe('c'.repeat(64)))

      // The form follows it, stays clean, and has nothing to send.
      expect((screen.getByLabelText(label) as HTMLInputElement).value).toBe(later)
      expect((screen.getByRole('button', { name: 'Save' }) as HTMLButtonElement).disabled).toBe(true)
      fireEvent.click(screen.getByRole('button', { name: 'Save' }))
      await waitFor(() => expect(desk.bodies).toHaveLength(refused))
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
