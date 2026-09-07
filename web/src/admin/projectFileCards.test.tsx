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
import { cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react'
import { afterEach, describe, expect, it, vi } from 'vitest'
import { DeskConfigProvider } from '../config/DeskConfigProvider'
import { testQueryClient } from '../testing/harness'
import { OrganizationForm, StorageForm } from './projectFileCards'
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
function servesAMovedFile(deskFile?: object): { puts: number } {
  const seen = { puts: 0, reads: 0 }
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

function renderForm(form: ReactElement) {
  return render(
    <QueryClientProvider client={testQueryClient()}>
      <DeskConfigProvider>{form}</DeskConfigProvider>
    </QueryClientProvider>
  )
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
    // The read lands — the file on disk now says something else — and the
    // value that was typed is still in the field.
    await waitFor(() =>
      expect(screen.queryByText('The file changed on disk — nothing was written.')).toBeNull()
    )
    expect(screen.getByDisplayValue('What I typed')).toBeTruthy()
    expect(screen.queryByDisplayValue('Renamed elsewhere')).toBeNull()
  })

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
