/**
 * The Assistant tab, over one whole scripted session.
 *
 * The pane is rendered with the same scripted model and the same recorded
 * runtime the conformance session uses, so what is asserted here is what a
 * reader sees when the real scenario runs — not a rendering of fixtures
 * written to make a pane look right.
 */
import { QueryClientProvider } from '@tanstack/react-query'
import { act, cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react'
import { useMemo } from 'react'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { AssistantPane, DRAFT_SENTENCE, withDraft } from './AssistantPane'
import { EditingContext, type EditingSession } from '../packs/edit/editingContext'
import { useDocumentBuffer, type DocumentBuffer } from '../packs/edit/useDocumentBuffer'
import { buffered, bytesAt } from '../packs/edit/writes'
import { scriptedModel } from './conformance/scriptedModel'
import { scriptedWebSocket } from './conformance/scriptedServer'
import scenario from './conformance/scenario.json'
import { DeskConfigFixture } from '../config/DeskConfigProvider'
import { decodeDeskConfig, effectiveConfig, type EffectiveConfig } from '../config/deskConfig'
import { McpContext } from '../mcp/McpProvider'
import { connected, stubClient, testQueryClient } from '../testing/harness'

const ENDPOINT = {
  url: 'https://api.example.invalid/v1',
  kind: 'openai-compatible',
  model: 'a-model',
  tools: scenario.scenarioTools
}

function config(assistant: unknown): EffectiveConfig {
  return effectiveConfig(undefined, undefined, undefined, {
    path: '/home/someone/.config/jpack-desk/desk.json',
    present: true,
    decoded: decodeDeskConfig(JSON.stringify({ deskConfigVersion: 1, assistant }), 'desk')
  })
}

let runtime: ReturnType<typeof scriptedWebSocket> | null = null
/** The buffer the draft harness is holding, for a case that drives Undo. */
let held: DocumentBuffer | null = null
/** How many times the pane wrote, which is the undo-entry claim's other half. */
let wrote = 0

/**
 * The pane over a **real** buffer, through the editing session the route builds.
 *
 * `useDocumentBuffer` is the production hook, so the undo stack, the dirty
 * comparison and the coalescing are the page's own rather than a fixture's —
 * which is what makes "one undo entry" a measurement rather than a claim.
 */
function DraftHarness({
  initial,
  editing,
  diagnostics
}: {
  initial: string
  editing: boolean
  diagnostics?: { code?: string; instancePath?: string; message?: string }[]
}) {
  const buffer = useDocumentBuffer({
    path: 'packs/vendor-onboarding.pack.json',
    bytes: initial.length,
    sha256: 'a'.repeat(64),
    content: initial
  })
  held = buffer
  const text = buffer.text
  const read = useMemo(() => (text === undefined ? undefined : buffered(text)), [text])
  const commit = buffer.commit
  const session: EditingSession = useMemo(
    () => ({
      editing,
      buffer: read ?? { text: '', index: { spans: new Map(), duplicates: [] } },
      write: (edit, options) => {
        if (read === undefined) return
        wrote += 1
        commit(edit(read).text, options)
      },
      diagnosticsAt: () => [],
      ids: { outcomes: [], evidence: [], sources: [], rules: [], factPaths: [] },
      pending: new Map(),
      hold: () => {}
    }),
    [editing, read, commit]
  )
  return (
    <EditingContext.Provider value={session}>
      <AssistantPane draft={text} editing={editing} diagnostics={diagnostics} />
    </EditingContext.Provider>
  )
}

/**
 * Everything the pane needs, and nothing it does not.
 *
 * The desk's own connection is a stub client that serves the `author_pack`
 * prompt; the key read answers from a `fetch` stub; the model is the scripted
 * one; and the assistant's own connection is the page's **real**
 * `DeskWebSocketTransport` talking to the recorded runtime through a stand-in
 * `WebSocket`. So the socket the pane opens, the gate on it and the SDK client
 * above it are all the production ones.
 */
async function draw(options: {
  assistant?: unknown
  keyPresent?: boolean
  prompts?: Record<string, { text: string }>
  /** Leave the model's answer in flight, so a run is still open. */
  hang?: boolean
  /** Answer every model request with the chassis' own refusal envelope. */
  refuse?: boolean
  /** A socket that opens and then answers nothing, not even `initialize`. */
  deafSocket?: boolean
  /** The bytes the page is about, which the diff is computed against. */
  draft?: string
  /** True where the page is being edited, which is what Accept needs. */
  editing?: boolean
  /** Draw the pane over a real buffer instead, and edit that. */
  buffer?: { text: string; editing?: boolean }
  /** The runtime's diagnostics for the bytes on this page. */
  diagnostics?: { code?: string; instancePath?: string; message?: string }[]
} = {}) {
  const model = scriptedModel({ api: 'openai-compatible', answerAs: 'stream' })
  const keyRead = JSON.stringify({
    present: options.keyPresent ?? true,
    fingerprint: options.keyPresent === false ? '' : 'sk-a…wxyz'
  })
  const relayed: { url: string; headerNames: string[]; body: string }[] = []
  vi.stubGlobal('fetch', async (input: unknown, init?: RequestInit) => {
    const url = String(input)
    if (url.startsWith('/api/assistant/relay/')) {
      relayed.push({
        url,
        headerNames: Object.keys((init?.headers ?? {}) as Record<string, string>),
        body: String(init?.body ?? '')
      })
      if (options.hang) return new Promise<Response>(() => {})
      if (options.refuse) {
        return new Response(JSON.stringify({ error: 'no key stored', code: 'assistant-no-key' }), {
          status: 409,
          headers: { 'content-type': 'application/json' }
        })
      }
      return model.fetch(input as string, init)
    }
    return { ok: true, status: 200, statusText: '', text: async () => keyRead } as unknown as Response
  })
  runtime = scriptedWebSocket({ deaf: options.deafSocket ?? false })
  vi.stubGlobal('WebSocket', runtime.WebSocket)

  const { client, prompted } = stubClient(
    {},
    {
      prompts: options.prompts ?? {
        author_pack: { text: 'The runtime’s authoring prompt, with the policy in it.' },
        fix_pack: { text: 'The runtime’s repair prompt, with the diagnostics in it.' }
      }
    }
  )
  render(
    <QueryClientProvider client={testQueryClient()}>
      <McpContext.Provider value={connected({ client })}>
        <DeskConfigFixture value={config(options.assistant ?? { endpoint: ENDPOINT })}>
          {options.buffer === undefined ? (
            <AssistantPane
              draft={options.draft}
              editing={options.editing ?? false}
              diagnostics={options.diagnostics}
            />
          ) : (
            <DraftHarness
              initial={options.buffer.text}
              editing={options.buffer.editing ?? true}
              diagnostics={options.diagnostics}
            />
          )}
        </DeskConfigFixture>
      </McpContext.Provider>
    </QueryClientProvider>
  )
  return { model, relayed, prompted }
}

beforeEach(() => {
  window.sessionStorage.setItem('jpack-desk-token', 'a-token')
})

afterEach(() => {
  cleanup()
  vi.restoreAllMocks()
  vi.unstubAllGlobals()
  runtime = null
  held = null
  wrote = 0
  window.sessionStorage.clear()
})

describe('where there is no assistant to run', () => {
  it('says where an endpoint is configured, and offers no control', async () => {
    await draw({ assistant: { endpoint: null } })
    expect(await screen.findByText(/No assistant is configured on this desk/)).toBeTruthy()
    expect(screen.getByText(/Admin › Assistant/)).toBeTruthy()
    expect(screen.queryByRole('button', { name: 'Run' })).toBeNull()
  })

  it('says where a key goes where an endpoint is configured and none is stored', async () => {
    await draw({ keyPresent: false })
    expect(await screen.findByText(/no key is stored on this machine/)).toBeTruthy()
    expect(screen.queryByRole('button', { name: 'Run' })).toBeNull()
  })
})

describe('the tab before a run', () => {
  it('names the engine, the endpoint’s model and the tier as stored', async () => {
    await draw({ assistant: { endpoint: ENDPOINT, engine: 'builtin', thinking: 'ultra' } })
    expect(await screen.findByText('builtin · a-model · thinking ultra')).toBeTruthy()
  })

  it('says which engine ran where the configured one is not certified here', async () => {
    await draw({ assistant: { endpoint: ENDPOINT, engine: 'vercel' } })
    expect(
      await screen.findByText('vercel is not certified in this build; running builtin')
    ).toBeTruthy()
    expect(screen.getByText('builtin · a-model · thinking off')).toBeTruthy()
  })

  it('will not run with an empty policy', async () => {
    await draw()
    await waitFor(() =>
      expect(screen.getByRole('button', { name: 'Run' }).hasAttribute('disabled')).toBe(true)
    )
    fireEvent.change(screen.getByLabelText('What should this pack decide?'), {
      target: { value: '  ' }
    })
    expect(screen.getByRole('button', { name: 'Run' }).hasAttribute('disabled')).toBe(true)
  })

  it('says so where the runtime advertises no author_pack prompt', async () => {
    await draw({ prompts: {} })
    expect(await screen.findByText(/advertises no author_pack prompt/)).toBeTruthy()
    expect(screen.getByRole('button', { name: 'Run' }).hasAttribute('disabled')).toBe(true)
  })
})

describe('one whole run', () => {
  async function runIt(already?: Awaited<ReturnType<typeof draw>>) {
    const drawn = already ?? (await draw())
    await waitFor(() =>
      expect(screen.getByRole('button', { name: 'Run' }).hasAttribute('disabled')).toBe(true)
    )
    fireEvent.change(screen.getByLabelText('What should this pack decide?'), {
      target: { value: scenario.policy }
    })
    fireEvent.click(screen.getByRole('button', { name: 'Run' }))
    await screen.findByRole('region', { name: 'The proposal' }, { timeout: 15_000 })
    return drawn
  }

  it('opens one connection of its own, and calls the relay with no credential', async () => {
    const { relayed } = await runIt()
    // The assistant's socket, not the desk's: one, and it carries the session
    // token the chassis authenticates every request with.
    expect(runtime!.opened).toHaveLength(1)
    expect(runtime!.opened[0]).toContain('/ws?token=a-token')
    expect(relayed.length).toBeGreaterThan(0)
    for (const request of relayed) {
      expect(request.url.startsWith('/api/assistant/relay/v1/chat/completions')).toBe(true)
      for (const forbidden of ['authorization', 'x-api-key', 'cookie']) {
        expect(request.headerNames.map((name) => name.toLowerCase())).not.toContain(forbidden)
      }
    }
  })

  it('sends the runtime’s prompt, and adds only the desk’s own system line', async () => {
    const { model } = await runIt()
    const messages = (
      JSON.parse(
        JSON.stringify(model.requests.length > 0 ? model.requests[0] : {})
      ) as { messageCount?: number }
    ).messageCount
    expect(messages).toBe(2)
  })

  it('reports each tool call, each answer’s byte count, and the guardrails', async () => {
    await runIt()
    const stream = screen.getByRole('list', { name: 'What the assistant did' })
    expect(stream.textContent).toContain('called get_schema(spec_version)')
    expect(stream.textContent).toContain('validate answered')
    expect(stream.textContent).toContain('bytes')
    // The desk's own two guardrail lines, in the words the gate wrote.
    expect(stream.textContent).toContain('rewrote experimental_evaluate')
    expect(stream.textContent).toContain('refused write_file')
    expect(stream.textContent).toContain('the session ended')
  })

  it('renders the proposal as a document, with its unknowns', async () => {
    await runIt()
    const document = screen.getByLabelText('The proposed document') as HTMLTextAreaElement
    expect(document.readOnly).toBe(true)
    expect(JSON.parse(document.value)).toEqual(scenario.documents.DRAFT_V2)
    for (const unknown of scenario.unknowns) {
      expect(screen.getByText(unknown)).toBeTruthy()
    }
  })

  it('shows the proposal as a diff against the draft it was given', async () => {
    const drawn = await draw({ draft: JSON.stringify(scenario.documents.DRAFT_V1, null, 2) })
    await runIt(drawn)
    const diff = screen.getByRole('region', { name: 'The proposal as a diff' })
    expect(diff.textContent).toContain('Compared with the draft on this page')
    // The two members that moved, the one that arrived, and the eleven that
    // did not — under one line with a count.
    expect(diff.textContent).toContain('/version')
    expect(diff.textContent).toContain('/rules')
    expect(diff.textContent).toContain('/exceptions')
    expect(diff.textContent).toContain('11 members unchanged')
    // The rule the proposal drops is named, and the three it keeps are not
    // redrawn as rewrites.
    expect(diff.textContent).toContain('/rules/3')
    expect(diff.textContent).toContain('3 elements unchanged, in the same place.')
    // And the draft's own bytes are quoted beside the proposal's.
    const before = screen.getByLabelText('/version, in the draft') as HTMLTextAreaElement
    expect(JSON.parse(before.value)).toBe('0.0.1')
  })

  it('says there was nothing to compare with where the page has no bytes', async () => {
    await runIt()
    const diff = screen.getByRole('region', { name: 'The proposal as a diff' })
    expect(diff.textContent).toContain('There was nothing to compare with')
    expect(diff.textContent).toContain('no draft on this page')
  })

  it('quotes the runtime’s checks beside it rather than summarising them', async () => {
    await runIt()
    const validate = screen.getByLabelText('validate, as the runtime wrote it') as HTMLTextAreaElement
    const evaluate = screen.getByLabelText(
      'experimental_evaluate, as the runtime wrote it'
    ) as HTMLTextAreaElement
    // The last validate is the one over DRAFT_V2, and it is quoted whole.
    expect(JSON.parse(validate.value).status).toBe('valid')
    expect(JSON.parse(evaluate.value).rehearsal).toBe(true)
    expect(JSON.parse(evaluate.value).disposition.outcomeId).toBe('approve')
    // And the pane states no verdict of its own beside them.
    expect(screen.getByRole('region', { name: 'The proposal' }).textContent).toContain(
      'Nothing has been written.'
    )
  })

  it('offers no Accept on the reading route, and says where one is', async () => {
    await runIt()
    expect(screen.queryByRole('button', { name: /Accept/ })).toBeNull()
    expect(screen.getByText('Open Edit to accept.')).toBeTruthy()
    // Reject is the desk's other action on the proposal, and it is the desk's
    // wherever the proposal is shown.
    expect(screen.getByRole('button', { name: 'Reject' }).hasAttribute('disabled')).toBe(false)
  })

  it('never lets write_file reach the runtime', async () => {
    await runIt()
    expect(runtime!.seen.map((call) => call.name)).not.toContain('write_file')
    expect(runtime!.seen.filter((call) => call.refusal !== '')).toEqual([])
  })
})

describe('exactly one end, on every path', () => {
  /** The terminal events on the stream the pane rendered. */
  const ends = () =>
    [...document.querySelectorAll('[aria-label="What the assistant did"] li')].filter(
      (line) => line.textContent === 'the session ended'
    )

  async function typeAndRun() {
    await waitFor(() =>
      expect(screen.getByRole('button', { name: 'Run' }).hasAttribute('disabled')).toBe(true)
    )
    fireEvent.change(screen.getByLabelText('What should this pack decide?'), {
      target: { value: scenario.policy }
    })
    fireEvent.click(screen.getByRole('button', { name: 'Run' }))
  }

  it('one, on the path that reaches a proposal', async () => {
    await draw()
    await typeAndRun()
    await screen.findByRole('region', { name: 'The proposal' }, { timeout: 15_000 })
    expect(ends()).toHaveLength(1)
  })

  it('one, when Stop is pressed with the model still answering', async () => {
    // The defect: Stop cleared the run's identity before the engine handled
    // the abort, so the engine's own `end` was discarded and the stream simply
    // stopped — status said finished and the contract's terminal event was
    // nowhere.
    await draw({ hang: true })
    await typeAndRun()
    // The engine has to have started: Stop is enabled during the prompt read
    // too, and that is a different phase with nothing to end.
    await waitFor(() => expect(runtime!.opened.length).toBe(1))
    fireEvent.click(screen.getByRole('button', { name: 'Stop' }))
    await waitFor(() => expect(ends()).toHaveLength(1))
    // And the engine's own `end`, which arrives once the abort has propagated,
    // does not become a second one.
    await new Promise((resolve) => setTimeout(resolve, 50))
    expect(ends()).toHaveLength(1)
  })

  it('one, when the endpoint refuses before the engine gets anywhere', async () => {
    await draw({ refuse: true })
    await typeAndRun()
    await waitFor(() => expect(ends()).toHaveLength(1))
    const stream = screen.getByRole('list', { name: 'What the assistant did' })
    expect(stream.textContent).toContain('409')
  })

  it('one, when the connection itself never comes up', async () => {
    // Nothing answers `initialize`, so `ready` never resolves on its own. Stop
    // has to end the run and close the socket even though setup never finished.
    await draw({ deafSocket: true })
    await typeAndRun()
    // The engine has to have started: Stop is enabled during the prompt read
    // too, and that is a different phase with nothing to end.
    await waitFor(() => expect(runtime!.opened.length).toBe(1))
    fireEvent.click(screen.getByRole('button', { name: 'Stop' }))
    await waitFor(() => expect(ends()).toHaveLength(1))
    await waitFor(() => expect(runtime!.closed).toBeGreaterThan(0))
  })
})

describe('running the same policy twice', () => {
  it('runs again with the text unchanged', async () => {
    // Pressing Run with the same policy set the same state value, React
    // changed nothing, and the enabled button did nothing at all.
    await draw()
    await waitFor(() =>
      expect(screen.getByRole('button', { name: 'Run' }).hasAttribute('disabled')).toBe(true)
    )
    fireEvent.change(screen.getByLabelText('What should this pack decide?'), {
      target: { value: scenario.policy }
    })
    fireEvent.click(screen.getByRole('button', { name: 'Run' }))
    await screen.findByRole('region', { name: 'The proposal' }, { timeout: 15_000 })
    const first = runtime!.opened.length

    fireEvent.click(screen.getByRole('button', { name: 'Run' }))
    // A second run: a second connection, and a stream that starts again.
    await waitFor(() => expect(runtime!.opened.length).toBe(first + 1))
    await screen.findByRole('region', { name: 'The proposal' }, { timeout: 15_000 })
    expect(
      [...document.querySelectorAll('[aria-label="What the assistant did"] li')].filter(
        (line) => line.textContent === 'the session ended'
      )
    ).toHaveLength(1)
  })
})

describe('stopping', () => {
  it('stops on Escape while a session is running', async () => {
    await draw()
    await waitFor(() =>
      expect(screen.getByRole('button', { name: 'Run' }).hasAttribute('disabled')).toBe(true)
    )
    fireEvent.change(screen.getByLabelText('What should this pack decide?'), {
      target: { value: scenario.policy }
    })
    fireEvent.click(screen.getByRole('button', { name: 'Run' }))
    await waitFor(() => expect(runtime!.opened.length).toBe(1))
    fireEvent.keyDown(document, { key: 'Escape' })
    await waitFor(() =>
      expect(screen.getByRole('button', { name: 'Stop' }).hasAttribute('disabled')).toBe(true)
    )
  })

  it('closes the session’s socket when the run ends', async () => {
    await draw()
    await waitFor(() =>
      expect(screen.getByRole('button', { name: 'Run' }).hasAttribute('disabled')).toBe(true)
    )
    fireEvent.change(screen.getByLabelText('What should this pack decide?'), {
      target: { value: scenario.policy }
    })
    fireEvent.click(screen.getByRole('button', { name: 'Run' }))
    await screen.findByRole('region', { name: 'The proposal' }, { timeout: 15_000 })
    // The connection lives exactly as long as the session, and the chassis
    // spawns one `jpack mcp` behind it.
    await waitFor(() => expect(runtime!.closed).toBe(1))
  })

  it('closes it on unmount while a run is still in flight', async () => {
    // The model never answers here, so the session is open when the route
    // changes. A socket left behind is a runtime subprocess nobody is watching.
    await draw({ hang: true })
    await waitFor(() =>
      expect(screen.getByRole('button', { name: 'Run' }).hasAttribute('disabled')).toBe(true)
    )
    fireEvent.change(screen.getByLabelText('What should this pack decide?'), {
      target: { value: scenario.policy }
    })
    fireEvent.click(screen.getByRole('button', { name: 'Run' }))
    await waitFor(() => expect(runtime!.opened).toHaveLength(1))
    expect(runtime!.closed).toBe(0)
    cleanup()
    await waitFor(() => expect(runtime!.closed).toBeGreaterThan(0))
  })
})

describe('accepting the proposal into the draft', () => {
  /** The draft as an author's file: four spaces, so a rewrite is visible. */
  const DRAFT = `${JSON.stringify(scenario.documents.DRAFT_V1, null, 4)}\n`

  async function runOver(options: { editing?: boolean } = {}) {
    await draw({ buffer: { text: DRAFT, editing: options.editing ?? true } })
    await waitFor(() =>
      expect(screen.getByRole('button', { name: 'Run' }).hasAttribute('disabled')).toBe(true)
    )
    fireEvent.change(screen.getByLabelText('What should this pack decide?'), {
      target: { value: scenario.policy }
    })
    fireEvent.click(screen.getByRole('button', { name: 'Run' }))
    await screen.findByRole('region', { name: 'The proposal' }, { timeout: 15_000 })
  }

  const acceptNow = () =>
    fireEvent.click(screen.getByRole('button', { name: 'Accept into draft' }))

  it('leaves a buffer that parses to the proposal, with the untouched members byte for byte', async () => {
    await runOver()
    const before = buffered(held!.text!)
    acceptNow()
    await waitFor(() => expect(held!.text).not.toBe(DRAFT))
    const after = buffered(held!.text!)
    expect(JSON.parse(after.text)).toEqual(scenario.documents.DRAFT_V2)
    for (const name of Object.keys(scenario.documents.DRAFT_V1)) {
      if (name === 'version' || name === 'rules') continue
      expect(bytesAt(after, `/${name}`)).toBe(bytesAt(before, `/${name}`))
    }
    // And the author's own four spaces are still the document's layout.
    expect(after.text).toContain('\n    "specVersion"')
  })

  it('is one write, one undo entry, and one step back to where it started', async () => {
    await runOver()
    acceptNow()
    await waitFor(() => expect(held!.dirty).toBe(true))
    expect(wrote).toBe(1)
    expect(held!.canUndo).toBe(true)
    act(() => held!.undo())
    await waitFor(() => expect(held!.text).toBe(DRAFT))
    expect(held!.dirty).toBe(false)
    expect(held!.canUndo).toBe(false)
  })

  it('says it is in the draft and nothing is saved, and disables both controls', async () => {
    await runOver()
    acceptNow()
    await waitFor(() =>
      expect(screen.getByRole('button', { name: 'Accept into draft' }).hasAttribute('disabled')).toBe(
        true
      )
    )
    expect(screen.getByRole('button', { name: 'Reject' }).hasAttribute('disabled')).toBe(true)
    const region = screen.getByRole('region', { name: 'The proposal' })
    expect(region.textContent).toContain('Accepted into the draft.')
    expect(region.textContent).toContain('Nothing has been saved.')
    expect(
      screen.getByRole('button', { name: 'Accept into draft' }).getAttribute('title')
    ).toContain('already in the draft')
  })

  it('does not write twice when Accept is pressed twice', async () => {
    await runOver()
    acceptNow()
    await waitFor(() => expect(held!.dirty).toBe(true))
    const settled = held!.text
    fireEvent.click(screen.getByRole('button', { name: 'Accept into draft' }))
    expect(wrote).toBe(1)
    expect(held!.text).toBe(settled)
  })

  it('writes nothing at all on the reading route', async () => {
    await runOver({ editing: false })
    expect(screen.queryByRole('button', { name: /Accept/ })).toBeNull()
    expect(screen.getByText('Open Edit to accept.')).toBeTruthy()
    expect(wrote).toBe(0)
    expect(held!.dirty).toBe(false)
  })

  it('rejects the proposal, keeps the stream, and writes nothing', async () => {
    await runOver()
    fireEvent.click(screen.getByRole('button', { name: 'Reject' }))
    await waitFor(() => expect(screen.queryByRole('region', { name: 'The proposal' })).toBeNull())
    expect(screen.getByRole('list', { name: 'What the assistant did' })).toBeTruthy()
    expect(screen.getByText(/The proposal was rejected/)).toBeTruthy()
    expect(wrote).toBe(0)
    expect(held!.dirty).toBe(false)
  })

  it('offers the next run its own proposal after one was accepted', async () => {
    await runOver()
    acceptNow()
    await waitFor(() => expect(held!.dirty).toBe(true))
    fireEvent.click(screen.getByRole('button', { name: 'Run' }))
    await waitFor(() =>
      expect(
        screen.getByRole('button', { name: 'Accept into draft' }).hasAttribute('disabled')
      ).toBe(false)
    )
  })
})

describe('the draft the session is given', () => {
  const DRAFT = `${JSON.stringify(scenario.documents.DRAFT_V1, null, 4)}\n`

  /** The first user message of the first request the pane made. */
  const firstUserMessage = (relayed: { body: string }[]) => {
    const body = JSON.parse(relayed[0]!.body) as { messages: { role: string; content: string }[] }
    return body.messages.find((message) => message.role === 'user')!.content
  }

  async function runOver(options: { buffer?: { text: string }; draft?: string } = {}) {
    const drawn = await draw({ ...options, editing: true })
    await waitFor(() =>
      expect(screen.getByRole('button', { name: 'Run' }).hasAttribute('disabled')).toBe(true)
    )
    fireEvent.change(screen.getByLabelText('What should this pack decide?'), {
      target: { value: scenario.policy }
    })
    fireEvent.click(screen.getByRole('button', { name: 'Run' }))
    await screen.findByRole('region', { name: 'The proposal' }, { timeout: 15_000 })
    return drawn
  }

  it('carries the bytes in the editor, verbatim and fenced, after the prompt', async () => {
    const { relayed } = await runOver({ buffer: { text: DRAFT } })
    const sent = firstUserMessage(relayed)
    // The runtime's own prompt first, then the sentence, then the draft as it
    // is — byte for byte, not a re-serialization of it.
    expect(sent.startsWith('The runtime’s authoring prompt, with the policy in it.')).toBe(true)
    expect(sent).toContain(DRAFT_SENTENCE)
    expect(sent).toContain(`\`\`\`json\n${DRAFT}\n\`\`\``)
    expect(sent.indexOf(DRAFT_SENTENCE)).toBeGreaterThan(0)
  })

  it('calls the proposal an update where a draft was sent', async () => {
    await runOver({ buffer: { text: DRAFT } })
    expect(screen.getByRole('region', { name: 'The proposal' }).textContent).toContain(
      'an update to the draft it was given'
    )
  })

  it('sends the prompt alone where the page has no bytes, and calls it a new document', async () => {
    const { relayed } = await runOver()
    expect(firstUserMessage(relayed)).toBe('The runtime’s authoring prompt, with the policy in it.')
    expect(screen.getByRole('region', { name: 'The proposal' }).textContent).toContain(
      'a new document'
    )
  })

  it('sends the saved document on the reading route too', async () => {
    const drawn = await draw({ draft: DRAFT })
    await waitFor(() =>
      expect(screen.getByRole('button', { name: 'Run' }).hasAttribute('disabled')).toBe(true)
    )
    fireEvent.change(screen.getByLabelText('What should this pack decide?'), {
      target: { value: scenario.policy }
    })
    fireEvent.click(screen.getByRole('button', { name: 'Run' }))
    await screen.findByRole('region', { name: 'The proposal' }, { timeout: 15_000 })
    expect(firstUserMessage(drawn.relayed)).toContain(DRAFT)
  })

  it('adds nothing where there is nothing to add', () => {
    expect(withDraft('the prompt', undefined)).toBe('the prompt')
    expect(withDraft('the prompt', '   ')).toBe('the prompt')
    expect(withDraft('the prompt', '{"a":1}')).toBe(
      `the prompt\n\n${DRAFT_SENTENCE}\n\n\`\`\`json\n{"a":1}\n\`\`\``
    )
  })
})

describe('fixing what the check refused', () => {
  const DIAGNOSTICS = [
    {
      code: 'JPS-STRUCTURAL-REQUIRED',
      layer: 'structural',
      severity: 'error',
      instancePath: '/rules/0/outcomeId',
      message: 'the member is required'
    },
    {
      code: 'JPS-SEMANTIC-UNKNOWN-OUTCOME',
      layer: 'semantic',
      severity: 'error',
      instancePath: '/rules/1/outcomeId',
      message: 'no outcome declares this id'
    }
  ]

  it('is offered only where the check reports something to fix', async () => {
    await draw()
    await waitFor(() =>
      expect(screen.getByRole('button', { name: 'Fix' }).hasAttribute('disabled')).toBe(true)
    )
    expect(screen.getByRole('button', { name: 'Fix' }).getAttribute('title')).toContain(
      'no diagnostic to fix'
    )
    cleanup()
    await draw({ diagnostics: DIAGNOSTICS })
    await waitFor(() =>
      expect(screen.getByRole('button', { name: 'Fix' }).hasAttribute('disabled')).toBe(false)
    )
  })

  it('is refused where the runtime advertises no fix_pack prompt', async () => {
    await draw({
      diagnostics: DIAGNOSTICS,
      prompts: { author_pack: { text: 'the authoring prompt' } }
    })
    await waitFor(() =>
      expect(screen.getByRole('button', { name: 'Fix' }).hasAttribute('disabled')).toBe(true)
    )
    expect(screen.getByRole('button', { name: 'Fix' }).getAttribute('title')).toContain(
      'advertises no fix_pack'
    )
  })

  it('runs fix_pack with the runtime’s diagnostics, as the runtime wrote them', async () => {
    const { prompted } = await draw({ diagnostics: DIAGNOSTICS })
    await waitFor(() =>
      expect(screen.getByRole('button', { name: 'Fix' }).hasAttribute('disabled')).toBe(false)
    )
    fireEvent.click(screen.getByRole('button', { name: 'Fix' }))
    await screen.findByRole('region', { name: 'The proposal' }, { timeout: 15_000 })
    const asked = prompted.filter((entry) => entry.name === 'fix_pack')
    expect(asked).toHaveLength(1)
    // Byte for byte the report's own array. Not a message list, not a count,
    // not a severity filter: the runtime's words, whole.
    expect(asked[0]!.args.diagnostics).toBe(JSON.stringify(DIAGNOSTICS, null, 2))
    expect(JSON.parse(asked[0]!.args.diagnostics!)).toEqual(DIAGNOSTICS)
  })

  it('says which prompt ran, and over how many diagnostics', async () => {
    await draw({ diagnostics: DIAGNOSTICS })
    await waitFor(() =>
      expect(screen.getByRole('button', { name: 'Fix' }).hasAttribute('disabled')).toBe(false)
    )
    fireEvent.click(screen.getByRole('button', { name: 'Fix' }))
    expect(await screen.findByText(/Running the runtime’s fix_pack prompt/)).toBeTruthy()
    expect(screen.getByText(/over 2 diagnostics/)).toBeTruthy()
  })

  it('names author_pack where that is what ran', async () => {
    await draw({ diagnostics: DIAGNOSTICS })
    await waitFor(() =>
      expect(screen.getByRole('button', { name: 'Run' }).hasAttribute('disabled')).toBe(true)
    )
    fireEvent.change(screen.getByLabelText('What should this pack decide?'), {
      target: { value: scenario.policy }
    })
    fireEvent.click(screen.getByRole('button', { name: 'Run' }))
    expect(await screen.findByText(/Running the runtime’s author_pack prompt/)).toBeTruthy()
  })

  it('sends the draft with the repair prompt too', async () => {
    const DRAFT = '{\n    "specVersion": "0.2.0-draft"\n}\n'
    const { relayed } = await draw({ diagnostics: DIAGNOSTICS, buffer: { text: DRAFT } })
    await waitFor(() =>
      expect(screen.getByRole('button', { name: 'Fix' }).hasAttribute('disabled')).toBe(false)
    )
    fireEvent.click(screen.getByRole('button', { name: 'Fix' }))
    await screen.findByRole('region', { name: 'The proposal' }, { timeout: 15_000 })
    const body = JSON.parse(relayed[0]!.body) as { messages: { role: string; content: string }[] }
    const sent = body.messages.find((message) => message.role === 'user')!.content
    expect(sent).toContain('The runtime’s repair prompt, with the diagnostics in it.')
    expect(sent).toContain(DRAFT)
  })
})
