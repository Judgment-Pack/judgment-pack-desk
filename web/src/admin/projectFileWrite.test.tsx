/**
 * What a project-file card's Save puts on the wire, and what it does with the
 * answer.
 *
 * The compose is proved on its own in `projectFileSave.test.ts`; this is the
 * half that leaves the browser. Four properties, and each of them is a way a
 * configuration form is ordinarily wrong:
 *
 * - **The request states the bytes it replaces**, with the digest the read
 *   carried. A write without one is a page overwriting whatever it finds.
 * - **It never asks to overwrite.** The file API offers an override; a card
 *   that always sent it would have no concurrency story, only an unstated one.
 * - **A value this desk would refuse to read never reaches the request.** The
 *   file API forms no opinion about what a file means, so the opinion is this
 *   page's and it is asked before the request rather than after it.
 * - **The answer moves the cache, and the re-read only confirms it.** The
 *   second read is deliberately left hanging here, because a page that needed
 *   it would then be describing what it had just replaced.
 */
import { QueryClientProvider } from '@tanstack/react-query'
import { cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react'
import { afterEach, describe, expect, it, vi } from 'vitest'
import { DeskConfigProvider, useEffectiveConfig } from '../config/DeskConfigProvider'
import { testQueryClient } from '../testing/harness'
import { useProjectFileSave } from './useProjectFileSave'

afterEach(() => {
  cleanup()
  vi.unstubAllGlobals()
})

const FILE = `{
  "deskConfigVersion": 1,
  "organization": { "name": "Unveil", "mark": null },
  "panes": { "left": { "mode": "expanded", "width": 248 } }
}
`
const DIGEST = 'a'.repeat(64)

/** Every request the probe made, and what the desk answered. */
interface Desk {
  puts: { url: string; body: Record<string, unknown> }[]
  reads: number
}

/**
 * One desk that answers the two reads the configuration query makes and one
 * write.
 *
 * The **second** read of the project file never resolves, on purpose: a page
 * that reflected a save only after re-reading would show the value it had just
 * replaced for as long as that read took, and here it would show it for ever.
 */
function servesDesk(options: { write: (body: Record<string, unknown>) => Response }): Desk {
  const desk: Desk = { puts: [], reads: 0 }
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
      desk.puts.push({ url, body })
      return options.write(body)
    }
    desk.reads += 1
    if (desk.reads > 1) return new Promise(() => {})
    return answered({
      path: 'jpack-desk.json',
      bytes: FILE.length,
      sha256: DIGEST,
      content: FILE
    })
  })
  return desk
}

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

/** The read-back the chassis answers a write with: the bytes off the disk. */
function readBack(body: Record<string, unknown>): Response {
  const content = String(body.content)
  return answered({
    path: 'jpack-desk.json',
    bytes: content.length,
    sha256: 'b'.repeat(64),
    content
  })
}

/** One card's save, and the one value a save changes, with nothing else on screen. */
function Probe({ value }: { value: unknown }) {
  const { config } = useEffectiveConfig()
  const save = useProjectFileSave('/organization')
  return (
    <>
      <span data-testid="name">{config.organization.name ?? 'none'}</span>
      <span data-testid="problems">
        {save.problems.map((problem) => `${problem.key}: ${problem.reason}`).join(' | ')}
      </span>
      <span data-testid="stale">{save.stale === undefined ? '' : save.stale.actualSha256}</span>
      <span data-testid="said">{save.said ?? ''}</span>
      <button type="button" onClick={() => save.save([{ path: ['name'], value }])}>
        Save
      </button>
    </>
  )
}

function renderProbe(value: unknown) {
  return render(
    <QueryClientProvider client={testQueryClient()}>
      <DeskConfigProvider>
        <Probe value={value} />
      </DeskConfigProvider>
    </QueryClientProvider>
  )
}

describe('a project-file card’s save', () => {
  it('states the digest its read carried, and the project-relative path', async () => {
    const desk = servesDesk({ write: readBack })
    renderProbe('Renamed')
    await screen.findByText('Unveil')
    fireEvent.click(screen.getByRole('button', { name: 'Save' }))
    await waitFor(() => expect(desk.puts).toHaveLength(1))

    const body = desk.puts[0]!.body
    expect(body.path).toBe('jpack-desk.json')
    // The digest the read carried, and not the empty string, which is a claim
    // that there is no file.
    expect(body.baseSha256).toBe(DIGEST)
    // The member this card wrote, on the one line the file already gave it.
    expect(String(body.content)).toContain('"organization": {"name":"Renamed","mark":null}')
    // And every other member, byte for byte, in the request itself.
    expect(String(body.content)).toContain('"panes": { "left": { "mode": "expanded", "width": 248 } }')
    // The address and nothing else: this page holds no credential to put on
    // it, and the session cookie the browser attaches is not the page's.
    expect(desk.puts[0]!.url).toBe('/api/file')
  })

  it('never asks the desk to overwrite what it did not read', async () => {
    const desk = servesDesk({ write: readBack })
    renderProbe('Renamed')
    await screen.findByText('Unveil')
    fireEvent.click(screen.getByRole('button', { name: 'Save' }))
    await waitFor(() => expect(desk.puts).toHaveLength(1))
    // Both are the file API's opt-ins, and a configuration card offers neither:
    // there is no "write anyway", and the file it writes is already there.
    expect(desk.puts[0]!.body.override).toBe(false)
    expect(desk.puts[0]!.body.createParents).toBe(false)
  })

  it('sends nothing where the decoder refuses the value, and says which key', async () => {
    const desk = servesDesk({ write: readBack })
    renderProbe('')
    await screen.findByText('Unveil')
    fireEvent.click(screen.getByRole('button', { name: 'Save' }))
    await waitFor(() =>
      expect(screen.getByTestId('problems').textContent).toContain('organization.name')
    )
    // The decoder's own sentence, and not one written here.
    expect(screen.getByTestId('problems').textContent).toContain(
      'must be a non-empty string or null'
    )
    // Nothing was written, and the desk was never asked to.
    expect(desk.puts).toEqual([])
  })

  it('reflects a write that landed without waiting for a second read', async () => {
    // The re-read never answers in this desk. A page that depended on it would
    // go on showing the name it had just replaced.
    const desk = servesDesk({ write: readBack })
    renderProbe('Renamed')
    await screen.findByText('Unveil')
    fireEvent.click(screen.getByRole('button', { name: 'Save' }))
    await waitFor(() => expect(screen.getByTestId('name').textContent).toBe('Renamed'))
    expect(screen.getByTestId('said').textContent).toContain('Saved.')
    expect(desk.puts).toHaveLength(1)
  })

  it('reports a file that moved underneath it, and writes nothing', async () => {
    const desk = servesDesk({
      write: () =>
        answered(
          {
            error: 'the file on disk is not the file this edit started from',
            code: 'stale',
            path: 'jpack-desk.json',
            expectedSha256: DIGEST,
            actualSha256: 'c'.repeat(64),
            exists: true
          },
          409
        )
    })
    renderProbe('Renamed')
    await screen.findByText('Unveil')
    fireEvent.click(screen.getByRole('button', { name: 'Save' }))
    await waitFor(() =>
      expect(screen.getByTestId('stale').textContent).toBe('c'.repeat(64))
    )
    // The value on screen is what the file still says, because nothing landed.
    expect(screen.getByTestId('name').textContent).toBe('Unveil')
    expect(screen.getByTestId('said').textContent).toBe('')
    expect(desk.puts).toHaveLength(1)
  })
})
