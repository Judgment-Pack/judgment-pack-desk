/**
 * The Project card's one control, driven against a stub of the route it calls.
 *
 * What is asserted is **what it may ask for**: this project's own file as the
 * chassis spells it, or null, and no third value — because a page that could
 * name any path could hand the next launch a root outside the authority this
 * page had. The chassis refuses anything else; the control is the shape that
 * cannot ask for it.
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
/** This project, as the chassis spells it. The only value the route accepts. */
const HERE = '/this/launch/jpack-desk.json'

/** A desk-level read that answered, with whatever the file says. */
function read(project?: unknown, chassis = true): EffectiveConfig {
  return effectiveConfig(undefined, undefined, undefined, {
    path: DESK_PATH,
    present: true,
    sha256: DIGEST,
    ...(chassis
      ? { chassis: { projectDir: '/this/launch', projectFile: HERE, runtimeBin: 'jpack' } }
      : {}),
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
          answer.body ?? { path: DESK_PATH, sha256: 'b'.repeat(64), project: { file: HERE } }
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
      location={<code>{HERE}</code>}
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

const nominate = () => screen.getByRole('button', { name: 'Use this project as the default' })
const clear = () => screen.getByRole('button', { name: 'Clear the default' })

describe('the default project', () => {
  it('offers a nomination and no path field at all', () => {
    // **The shape is the fix.** A free-text path is a persistent expansion of
    // authority: the value chooses the root of the next launch, and a page
    // that could write any path could hand its successor a root it never had.
    renderCard()
    expect(nominate()).toBeTruthy()
    expect(screen.queryByLabelText('Default project')).toBeNull()
    expect(screen.queryByRole('textbox')).toBeNull()
  })

  it('sends this project’s own file, exactly as the chassis spelled it', async () => {
    const { sent } = stubWrites([{}])
    renderCard()
    fireEvent.click(nominate())
    await waitFor(() => expect(sent.some((each) => each.method === 'PUT')).toBe(true))
    const put = sent.find((each) => each.method === 'PUT')!
    expect(put.url).toContain('/api/desk-config')
    expect(JSON.parse(put.body!)).toEqual({ project: { file: HERE }, ifMatch: DIGEST })
  })

  it('offers to withdraw the default once it is this project, and sends null', async () => {
    const { sent } = stubWrites([
      { body: { path: DESK_PATH, sha256: 'c'.repeat(64), project: { file: null } } }
    ])
    renderCard(read({ file: HERE }))
    expect(screen.getByText('this project')).toBeTruthy()
    fireEvent.click(clear())
    await waitFor(() => expect(sent.some((each) => each.method === 'PUT')).toBe(true))
    expect(JSON.parse(sent.find((each) => each.method === 'PUT')!.body!).project).toEqual({
      file: null
    })
    expect(await screen.findByText(/configures no default project/)).toBeTruthy()
  })

  it('shows a default somebody else set, and still offers only this project', () => {
    // An operator may name any project by editing the file; the page may not,
    // so what it offers beside a foreign default is the nomination and not a
    // way to change that value to a third one.
    renderCard(read({ file: '/somewhere/else/jpack-desk.json' }))
    expect(screen.getByText('/somewhere/else/jpack-desk.json')).toBeTruthy()
    expect(nominate()).toBeTruthy()
    expect(screen.queryByRole('button', { name: 'Clear the default' })).toBeNull()
  })

  it('names the file it writes, what the value is for, and this launch', () => {
    // The card's Location is the project's own file; this control writes the
    // desk-level one, and the line says so from the chassis' answer.
    renderCard()
    const rule = screen.getByText(/used on the next launch without a directory/)
    expect(rule.textContent).toContain(DESK_PATH)
    expect(rule.textContent).toContain('/this/launch')
  })

  it('writes nothing at all where this page never learned the digest', () => {
    const { sent } = stubWrites([{}])
    render(
      <QueryClientProvider client={testQueryClient()}>
        <DeskConfigFixture value={effectiveConfig(undefined)}>
          <Card />
        </DeskConfigFixture>
      </QueryClientProvider>
    )
    expect((nominate() as HTMLButtonElement).disabled).toBe(true)
    expect(screen.getByText(/a write states the bytes it replaces/)).toBeTruthy()
    fireEvent.click(nominate())
    expect(sent.filter((each) => each.method === 'PUT')).toHaveLength(0)
  })

  it('writes nothing where the chassis has not named this project', () => {
    // The one value the route accepts is the chassis'. Without it the control
    // could only compose a path, which is the thing it exists not to do.
    const { sent } = stubWrites([{}])
    renderCard(read(undefined, false))
    expect((nominate() as HTMLButtonElement).disabled).toBe(true)
    expect(screen.getByText(/has not said where its own configuration file is/)).toBeTruthy()
    fireEvent.click(nominate())
    expect(sent.filter((each) => each.method === 'PUT')).toHaveLength(0)
  })

  it('renders the chassis’ refusal in its own words, against its key', async () => {
    // The route refuses any value but this project's; the page renders that
    // sentence rather than one of its own about it.
    stubWrites([
      {
        status: 422,
        body: {
          error: 'the configuration this would write is not one this page may write',
          code: 'desk-config-refused',
          problems: [
            {
              key: 'project.file',
              reason: 'the page may nominate only the project this desk is running in'
            }
          ]
        }
      }
    ])
    renderCard()
    fireEvent.click(nominate())
    expect(
      await screen.findByText(/project.file: the page may nominate only the project/)
    ).toBeTruthy()
  })

  it('offers to read the file again on a 409, and never to write anyway', async () => {
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
    fireEvent.click(nominate())
    expect(await screen.findByText(/changed on disk/)).toBeTruthy()
    expect(screen.queryByRole('button', { name: /anyway|Overwrite/ })).toBeNull()
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
    fireEvent.click(nominate())
    expect(await screen.findByText(/will not keep a key here/)).toBeTruthy()
  })
})
