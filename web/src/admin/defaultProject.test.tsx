/**
 * The Project card's Save, driven against a stub of the route it calls.
 *
 * The assertions are about **what it sends**: `project` and nothing else, the
 * digest its read carried, and null for an empty field. A form that showed the
 * right thing and sent the wrong one is exactly what these exist for.
 */
import { QueryClientProvider } from '@tanstack/react-query'
import { cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react'
import { afterEach, describe, expect, it, vi } from 'vitest'
import { DeskConfigFixture } from '../config/DeskConfigProvider'
import { decodeDeskConfig, effectiveConfig, type EffectiveConfig } from '../config/deskConfig'
import { DESK_CONFIG_QUERY_KEY } from '../config/queries'
import { testQueryClient } from '../testing/harness'
import { CardField, SourceCard } from './SourceCard'
import { useDefaultProject } from './DefaultProject'

afterEach(() => {
  cleanup()
  vi.unstubAllGlobals()
})

const DESK_PATH = '/home/someone/.config/jpack-desk/desk.json'
const DIGEST = 'a'.repeat(64)
const FILE = '/home/someone/a-project/jpack-desk.json'

/** A desk-level read that answered, with whatever the file says. */
function read(project?: unknown): EffectiveConfig {
  return effectiveConfig(undefined, undefined, undefined, {
    path: DESK_PATH,
    present: true,
    sha256: DIGEST,
    chassis: {
      projectDir: '/this/launch',
      projectFile: '/this/launch/jpack-desk.json',
      runtimeBin: 'jpack'
    },
    decoded: decodeDeskConfig(
      JSON.stringify({ deskConfigVersion: 1, ...(project ? { project } : {}) }),
      'desk'
    )
  })
}

/** Every request the card made, and one scripted answer per attempt. */
function stubWrites(answers: { status?: number; body?: unknown }[]): {
  sent: { method: string; url: string; body?: string }[]
} {
  const sent: { method: string; url: string; body?: string }[] = []
  let attempt = 0
  vi.stubGlobal('fetch', async (url: string, init?: RequestInit) => {
    sent.push({
      method: init?.method ?? 'GET',
      url: String(url),
      body: init?.body as string | undefined
    })
    const answer = answers[Math.min(attempt, answers.length - 1)] ?? {}
    if ((init?.method ?? 'GET') === 'PUT') attempt += 1
    const status = answer.status ?? 200
    return {
      ok: status < 400,
      status,
      statusText: '',
      text: async () =>
        JSON.stringify(
          answer.body ?? { path: DESK_PATH, sha256: 'b'.repeat(64), project: { file: FILE } }
        )
    }
  })
  return { sent }
}

/** The card, with the two slots the hook fills. */
function Card() {
  const { field, save } = useDefaultProject()
  return (
    <SourceCard
      id="project"
      title="Project file"
      location={<code>/this/launch/jpack-desk.json</code>}
      status={{ state: 'read' }}
      fields={
        <>
          {field}
          <CardField label="Other">nothing</CardField>
        </>
      }
      save={save}
    />
  )
}

function renderCard(value: EffectiveConfig = read(), client = testQueryClient()) {
  return {
    client,
    ...render(
      <QueryClientProvider client={client}>
        <DeskConfigFixture value={value}>
          <Card />
        </DeskConfigFixture>
      </QueryClientProvider>
    )
  }
}

const field = () => screen.getByLabelText('Default project') as HTMLInputElement

describe('the default project', () => {
  it('seeds the field from the file, and says which project this launch is on', () => {
    renderCard(read({ file: FILE }))
    expect(field().value).toBe(FILE)
    // The root is pinned per process: saving this changes the next launch and
    // nothing about the desk in front of you, and the card says so.
    expect(screen.getByText(/Used on the next launch without a directory/)).toBeTruthy()
    expect(screen.getByText('/this/launch')).toBeTruthy()
  })

  it('sends project and nothing else, with the digest its read carried', async () => {
    // A body that also restated `assistant` would make this card an author of
    // a member it never read, and two cards writing one file would race.
    const { sent } = stubWrites([{}])
    renderCard()
    fireEvent.change(field(), { target: { value: FILE } })
    fireEvent.click(screen.getByRole('button', { name: 'Save' }))
    await waitFor(() => expect(sent.some((each) => each.method === 'PUT')).toBe(true))
    const put = sent.find((each) => each.method === 'PUT')!
    expect(put.url).toContain('/api/desk-config')
    expect(JSON.parse(put.body!)).toEqual({ project: { file: FILE }, ifMatch: DIGEST })
  })

  it('sends null for an empty field, which is a desk that configures none', async () => {
    const { sent } = stubWrites([
      { body: { path: DESK_PATH, sha256: 'c'.repeat(64), project: { file: null } } }
    ])
    renderCard(read({ file: FILE }))
    fireEvent.change(field(), { target: { value: '   ' } })
    fireEvent.click(screen.getByRole('button', { name: 'Save' }))
    await waitFor(() => expect(sent.some((each) => each.method === 'PUT')).toBe(true))
    expect(JSON.parse(sent.find((each) => each.method === 'PUT')!.body!).project).toEqual({
      file: null
    })
    expect(await screen.findByText(/configures no default project/)).toBeTruthy()
  })

  it('reports what landed rather than what it sent', async () => {
    // The answer is read back off the disk; a card that reported its own
    // request would say "saved" for a value that is not in the file.
    stubWrites([{}])
    renderCard()
    fireEvent.change(field(), { target: { value: FILE } })
    fireEvent.click(screen.getByRole('button', { name: 'Save' }))
    expect(await screen.findByText(/applies to the next launch/)).toBeTruthy()
  })

  it('writes nothing at all where this page never learned the digest', () => {
    // A write states the bytes it replaces. Writing with the empty string
    // would be claiming there is no file, which is a claim.
    const { sent } = stubWrites([{}])
    render(
      <QueryClientProvider client={testQueryClient()}>
        <DeskConfigFixture value={effectiveConfig(undefined)}>
          <Card />
        </DeskConfigFixture>
      </QueryClientProvider>
    )
    const save = screen.getByRole('button', { name: 'Save' }) as HTMLButtonElement
    expect(save.disabled).toBe(true)
    expect(screen.getByText(/a write states the bytes it replaces/)).toBeTruthy()
    fireEvent.click(save)
    expect(sent.filter((each) => each.method === 'PUT')).toHaveLength(0)
  })

  it('renders a refusal in the decoder’s own words, against its key', async () => {
    stubWrites([
      {
        status: 422,
        body: {
          error: 'the configuration this would write is not one this desk reads',
          code: 'desk-config-refused',
          problems: [{ key: 'project.file', reason: 'must be an absolute path' }]
        }
      }
    ])
    renderCard()
    fireEvent.change(field(), { target: { value: 'a-project/jpack-desk.json' } })
    fireEvent.click(screen.getByRole('button', { name: 'Save' }))
    expect(await screen.findByText('project.file: must be an absolute path')).toBeTruthy()
    // And what was typed is still there to repair.
    expect(field().value).toBe('a-project/jpack-desk.json')
  })

  it('keeps what was typed on a 409 and offers to read the file again', async () => {
    stubWrites([
      {
        status: 409,
        body: {
          error: 'the desk-level configuration on disk is not the one this page read',
          code: 'desk-config-changed',
          path: DESK_PATH,
          expectedSha256: DIGEST,
          actualSha256: 'd'.repeat(64),
          exists: true
        }
      }
    ])
    const client = testQueryClient()
    const refetch = vi.spyOn(client, 'refetchQueries')
    renderCard(read(), client)
    fireEvent.change(field(), { target: { value: FILE } })
    fireEvent.click(screen.getByRole('button', { name: 'Save' }))
    expect(await screen.findByText(/changed on disk/)).toBeTruthy()
    expect(field().value).toBe(FILE)
    // There is no "write anyway": the repair is to read it again.
    expect(screen.queryByRole('button', { name: /anyway|Overwrite/ })).toBeNull()
    // Reload has to *read the file again*: a button that only cleared the
    // alert would leave the next Save stating the same stale digest.
    fireEvent.click(screen.getByRole('button', { name: 'Reload' }))
    expect(refetch).toHaveBeenCalledWith({ queryKey: DESK_CONFIG_QUERY_KEY })
    await waitFor(() => expect(screen.queryByText(/changed on disk/)).toBeNull())
  })

  it('says a refusal that is neither a decode nor a stale write', async () => {
    stubWrites([
      {
        status: 409,
        body: { error: 'this desk will not keep a key here', code: 'assistant-unusable-store' }
      }
    ])
    renderCard()
    fireEvent.click(screen.getByRole('button', { name: 'Save' }))
    expect(await screen.findByText(/will not keep a key here/)).toBeTruthy()
  })
})
