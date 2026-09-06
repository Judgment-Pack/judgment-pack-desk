/**
 * **Describe it**, inside the dialog that would write what it proposes.
 *
 * The section is never mounted alone here: what it is for is the Create dialog,
 * and the two claims worth holding — *nothing is written until Create is
 * pressed*, and *closing this dialog ends the session* — are claims about the
 * pair. So every case stands the real dialog up, with the real file API stub
 * beside the real assistant harness: the page's own `DeskWebSocketTransport`
 * talking to the recorded runtime through a stand-in `WebSocket`, the scripted
 * model behind the relay, and every write recorded with its parsed body.
 */
import { QueryClientProvider } from '@tanstack/react-query'
import { cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react'
import { useState } from 'react'
import { RouterProvider, createMemoryRouter } from 'react-router-dom'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import type { AssistantEvent, Engine } from '../assistant/engine'

/**
 * An engine this suite puts in the registry's place, where a case needs one.
 *
 * Null by default, so the end-to-end cases run the build's real default engine
 * against the scripted model exactly as the page does.
 */
let injected: Engine | null = null

vi.mock('../assistant/engines', async (importOriginal) => {
  const original = await importOriginal<typeof import('../assistant/engines')>()
  return {
    ...original,
    loadEngine: async (id: 'builtin' | 'vercel') => injected ?? original.loadEngine(id)
  }
})

import { scriptedModel } from '../assistant/conformance/scriptedModel'
import { scriptedWebSocket } from '../assistant/conformance/scriptedServer'
import scenario from '../assistant/conformance/scenario.json'
import { DeskConfigFixture } from '../config/DeskConfigProvider'
import { decodeDeskConfig, effectiveConfig, type EffectiveConfig } from '../config/deskConfig'
import { McpContext } from '../mcp/McpProvider'
import { connected, stubClient, testQueryClient } from '../testing/harness'
import { CreatePackDialog } from './CreatePackDialog'

const ENDPOINT = {
  url: 'https://api.example.invalid/v1',
  kind: 'openai-compatible',
  model: 'a-model',
  tools: scenario.scenarioTools
}

const PROJECT = `{
  "configVersion": "2",
  "packs": {
    "sanctions-screening": { "path": "sanctions-screening-0.1.0.pack.json" }
  }
}
`
const PROJECT_SHA = 'ab'.repeat(32)

const EXAMPLES = JSON.stringify({
  status: 'valid',
  examples: [{ name: 'minimal-expense-approval', focus: 'complete minimal pack' }]
})
const TEMPLATE = JSON.stringify({
  specVersion: '0.2.0-draft',
  id: 'https://served.example/examples/minimal',
  version: '9.9.9',
  title: 'The example’s own title',
  outcomes: [{ id: 'approve' }, { id: 'decline' }],
  rules: [{ id: 'r1' }]
})

interface Sent {
  path: string
  body: Record<string, unknown>
}

let runtime: ReturnType<typeof scriptedWebSocket> | null = null

function config(assistant: unknown): EffectiveConfig {
  return effectiveConfig(undefined, undefined, undefined, {
    path: '/home/someone/.config/jpack-desk/desk.json',
    present: true,
    decoded: decodeDeskConfig(JSON.stringify({ deskConfigVersion: 1, assistant }), 'desk')
  })
}

/**
 * The project's files, this desk's key, and the model — one stub.
 *
 * The relay's answers are the scripted model's; everything else is the file
 * API's. `sent` is every write, so "nothing is written until Create" is a
 * count rather than an impression.
 */
function serve(
  options: { keyPresent?: boolean; hang?: boolean; refuse?: boolean; files?: string[] } = {}
) {
  const sent: Sent[] = []
  const model = scriptedModel({ api: 'openai-compatible', answerAs: 'stream' })
  const files = options.files ?? ['jpack.json']
  vi.stubGlobal('fetch', async (input: unknown, init?: RequestInit) => {
    const url = String(input)
    const ok = (body: unknown) =>
      ({
        ok: true,
        status: 200,
        statusText: '',
        text: async () => JSON.stringify(body)
      }) as unknown as Response
    if (url.startsWith('/api/assistant/relay/')) {
      if (options.hang) return new Promise<Response>(() => {})
      if (options.refuse) {
        return new Response(JSON.stringify({ error: 'no key stored', code: 'assistant-no-key' }), {
          status: 409,
          headers: { 'content-type': 'application/json' }
        })
      }
      return model.fetch(url, init)
    }
    if (url.startsWith('/api/assistant/key')) {
      const present = options.keyPresent ?? true
      return ok({ present, fingerprint: present ? 'sk-a…wxyz' : '' })
    }
    if (url.includes('/api/files')) {
      return ok({ root: '/p', files: files.map((path) => ({ path, bytes: 1, sha256: 'aa' })) })
    }
    if (init?.method === 'PUT') {
      const body = JSON.parse(String(init.body)) as Record<string, unknown>
      sent.push({ path: String(body.path), body })
      return ok({ path: body.path, bytes: 2, sha256: 'cc', content: body.content, created: true })
    }
    if (url.includes(`path=${encodeURIComponent('jpack.json')}`) || url.includes('path=jpack.json')) {
      return ok({ path: 'jpack.json', bytes: PROJECT.length, sha256: PROJECT_SHA, content: PROJECT })
    }
    return {
      ok: false,
      status: 404,
      statusText: 'Not Found',
      text: async () => '{"error":"no such file in the project","code":"not-found"}'
    } as unknown as Response
  })
  runtime = scriptedWebSocket()
  vi.stubGlobal('WebSocket', runtime.WebSocket)
  return { sent, model }
}

/**
 * The dialog, in one of its two real shapes.
 *
 * The rail renders `{creating && <CreatePackDialog …/>}`, so closing normally
 * **unmounts** it — which is the default here. `persist` keeps the component
 * mounted across a close, and exists for exactly one claim: that the close
 * path stops the run *through the hook* rather than leaving the unmount to
 * abort it. With the component gone there is nothing left to ask, and an
 * unmount releases a connection whether or not anything ended the run.
 */
function Mounted({
  deskConfig,
  persist
}: {
  deskConfig: EffectiveConfig
  persist: boolean
}) {
  const [open, setOpen] = useState(true)
  const [stub] = useState(() =>
    stubClient(
      {
        list_examples: () => ({ text: EXAMPLES }),
        get_example: () => ({ text: TEMPLATE })
      },
      {
        prompts: {
          author_pack: { text: 'The runtime’s authoring prompt, with the policy in it.' }
        }
      }
    )
  )
  const [connection] = useState(() =>
    connected({ client: stub.client, exampleSupported: true, schemaSupported: false })
  )
  return (
    <McpContext.Provider value={connection}>
      <DeskConfigFixture value={deskConfig}>
        {persist ? (
          <>
            <button type="button" onClick={() => setOpen(true)}>
              Reopen
            </button>
            <CreatePackDialog open={open} onOpenChange={setOpen} />
          </>
        ) : (
          open && <CreatePackDialog open onOpenChange={setOpen} />
        )}
      </DeskConfigFixture>
    </McpContext.Provider>
  )
}

function draw(
  assistant: unknown = { endpoint: ENDPOINT },
  options: { persist?: boolean } = {}
) {
  const deskConfig = config(assistant)
  const router = createMemoryRouter(
    [
      {
        path: '*',
        element: <Mounted deskConfig={deskConfig} persist={options.persist ?? false} />
      }
    ],
    { initialEntries: ['/'] }
  )
  return render(
    <QueryClientProvider client={testQueryClient()}>
      <RouterProvider router={router} />
    </QueryClientProvider>
  )
}

/** Open the disclosure. Its content is behind a summary, as in the page. */
async function openIt() {
  const summary = await screen.findByText('Describe it instead')
  fireEvent.click(summary)
  return summary
}

/** Type a policy and press Propose. */
async function propose(policy: string = scenario.policy) {
  await openIt()
  fireEvent.change(await screen.findByLabelText(/What should this pack decide/), {
    target: { value: policy }
  })
  await waitFor(() =>
    expect(screen.getByRole('button', { name: 'Propose' }).hasAttribute('disabled')).toBe(false)
  )
  fireEvent.click(screen.getByRole('button', { name: 'Propose' }))
}

/** The terminal events on the stream the section rendered. */
const ends = () =>
  [...document.querySelectorAll('[aria-label="What the assistant did"] li')].filter(
    (line) => line.textContent === 'the session ended'
  )

beforeEach(() => {
  window.sessionStorage.setItem('jpack-desk-token', 'a-token')
})

afterEach(() => {
  cleanup()
  vi.restoreAllMocks()
  vi.unstubAllGlobals()
  runtime = null
  injected = null
  window.sessionStorage.clear()
})

describe('where there is no assistant to run', () => {
  it('says where an endpoint is configured, and offers no control', async () => {
    serve()
    draw({ endpoint: null })
    expect(await screen.findByText(/No assistant is configured on this desk/)).toBeTruthy()
    expect(screen.queryByRole('button', { name: 'Propose' })).toBeNull()
    // And the disclosure is not drawn either: there is nothing to disclose.
    expect(screen.queryByText('Describe it instead')).toBeNull()
  })

  it('says where a key goes where an endpoint is configured and none is stored', async () => {
    serve({ keyPresent: false })
    draw()
    expect(await screen.findByText(/no key is stored on this machine/)).toBeTruthy()
    expect(screen.queryByRole('button', { name: 'Propose' })).toBeNull()
  })
})

describe('the section before a run', () => {
  it('is a disclosure beside the template choice, and asks one question', async () => {
    serve()
    draw()
    await openIt()
    expect(await screen.findByLabelText(/What should this pack decide/)).toBeTruthy()
    // The template choice is still there: this is a third way to start, not a
    // replacement for the two the runtime serves.
    expect(screen.getByLabelText('Template')).toBeTruthy()
  })

  it('will not propose with an empty policy', async () => {
    serve()
    draw()
    await openIt()
    await waitFor(() =>
      expect(screen.getByRole('button', { name: 'Propose' }).hasAttribute('disabled')).toBe(true)
    )
    fireEvent.change(screen.getByLabelText(/What should this pack decide/), {
      target: { value: '   ' }
    })
    expect(screen.getByRole('button', { name: 'Propose' }).hasAttribute('disabled')).toBe(true)
  })

  it('names the engine, the model and the tier as stored', async () => {
    serve()
    draw({ endpoint: ENDPOINT, engine: 'builtin', thinking: 'ultra' })
    await openIt()
    expect(await screen.findByText('builtin · a-model · thinking ultra')).toBeTruthy()
  })
})

describe('one whole run, in the dialog', () => {
  async function runIt() {
    await propose()
    await screen.findByRole('region', { name: 'The proposal' }, { timeout: 15_000 })
  }

  it('reports each tool call and each of the desk’s own guardrails', async () => {
    serve()
    draw()
    await runIt()
    const stream = screen.getByRole('list', { name: 'What the assistant did' })
    expect(stream.textContent).toContain('called get_schema(spec_version)')
    expect(stream.textContent).toContain('validate answered')
    expect(stream.textContent).toContain('rewrote experimental_evaluate')
    expect(stream.textContent).toContain('refused write_file')
    expect(stream.textContent).toContain('the session ended')
  })

  it('summarises the document, lists the unknowns, and quotes the checks whole', async () => {
    serve()
    draw()
    await runIt()
    const proposal = screen.getByRole('region', { name: 'The proposal' })
    expect(proposal.textContent).toContain(scenario.documents.DRAFT_V2.title)
    expect(proposal.textContent).toContain(scenario.documents.DRAFT_V2.id)
    expect(proposal.textContent).toContain(`${scenario.documents.DRAFT_V2.rules.length} rules`)
    expect(proposal.textContent).toContain(
      `${scenario.documents.DRAFT_V2.outcomes.length} outcomes`
    )
    for (const unknown of scenario.unknowns) expect(screen.getByText(unknown)).toBeTruthy()
    const validate = screen.getByLabelText('validate, as the runtime wrote it') as HTMLTextAreaElement
    const evaluate = screen.getByLabelText(
      'experimental_evaluate, as the runtime wrote it'
    ) as HTMLTextAreaElement
    expect(JSON.parse(validate.value).status).toBe('valid')
    expect(JSON.parse(evaluate.value).rehearsal).toBe(true)
  })

  it('shows the whole document, read only, behind a disclosure', async () => {
    serve()
    draw()
    await runIt()
    fireEvent.click(screen.getByText('Show document'))
    const document = screen.getByLabelText('The proposed document') as HTMLTextAreaElement
    expect(document.readOnly).toBe(true)
    expect(JSON.parse(document.value)).toEqual(scenario.documents.DRAFT_V2)
  })

  it('writes nothing at all until Create is pressed', async () => {
    const { sent } = serve()
    draw()
    await runIt()
    expect(sent).toEqual([])
  })

  it('never lets write_file reach the runtime, and rehearses every evaluate', async () => {
    serve()
    draw()
    await runIt()
    expect(runtime!.seen.map((call) => call.name)).not.toContain('write_file')
    expect(runtime!.seen.filter((call) => call.refusal !== '')).toEqual([])
  })
})

describe('stopping', () => {
  it('ends the run with exactly one terminal event when Stop is pressed', async () => {
    serve({ hang: true })
    draw()
    await propose()
    await waitFor(() => expect(runtime!.opened.length).toBe(1))
    fireEvent.click(screen.getByRole('button', { name: 'Stop' }))
    await waitFor(() => expect(ends()).toHaveLength(1))
    await new Promise((resolve) => setTimeout(resolve, 50))
    expect(ends()).toHaveLength(1)
  })

  it('ends the run and closes its one socket when the dialog closes', async () => {
    // **Measured with the component still mounted**, because an unmount
    // releases a connection whether or not anything ended the run: with the
    // dialog gone, "the close path stopped it" and "the unmount aborted it"
    // look identical from outside. Here nothing unmounts, so the close is the
    // only thing that can have done it.
    serve({ hang: true })
    draw({ endpoint: ENDPOINT }, { persist: true })
    await propose()
    await waitFor(() => expect(runtime!.opened.length).toBe(1))
    fireEvent.click(screen.getByRole('button', { name: 'Cancel' }))
    await waitFor(() => expect(runtime!.closed).toBe(1))
    await new Promise((resolve) => setTimeout(resolve, 50))
    expect(runtime!.closed, 'the socket was closed twice').toBe(1)
  })

  it('writes the run’s terminal event on the way out, so the next one can start', async () => {
    // The run hook refuses to start a run while one is open and has not ended,
    // so a second session **is** the terminal event, observed. A close that
    // only aborted would leave the first run open for ever and this second
    // Propose would open no socket at all.
    serve({ hang: true })
    draw({ endpoint: ENDPOINT }, { persist: true })
    await propose()
    await waitFor(() => expect(runtime!.opened.length).toBe(1))
    fireEvent.click(screen.getByRole('button', { name: 'Cancel' }))
    await waitFor(() => expect(runtime!.closed).toBe(1))
    fireEvent.click(screen.getByRole('button', { name: 'Reopen' }))
    await propose()
    await waitFor(() => expect(runtime!.opened.length).toBe(2))
  })

  it('discards the proposal when the dialog closes: reopening is a new session', async () => {
    serve()
    draw({ endpoint: ENDPOINT }, { persist: true })
    await propose()
    await screen.findByRole('region', { name: 'The proposal' }, { timeout: 15_000 })
    fireEvent.click(screen.getByRole('button', { name: 'Cancel' }))
    fireEvent.click(screen.getByRole('button', { name: 'Reopen' }))
    await openIt()
    expect(screen.queryByRole('region', { name: 'The proposal' })).toBeNull()
    expect(document.querySelector('[aria-label="What the assistant did"]')).toBeNull()
    expect((screen.getByLabelText(/What should this pack decide/) as HTMLTextAreaElement).value).toBe('')
  })
})


/** Type a name and wait until the dialog is willing to act on it. */
async function nameIt(name: string) {
  fireEvent.change(screen.getByLabelText('Name (required)'), { target: { value: name } })
  await waitFor(() => expect(createButton().disabled).toBe(false))
}

const createButton = () => screen.getByRole('button', { name: 'Create pack' }) as HTMLButtonElement

/** An engine that proposes exactly this, and then ends. */
function proposing(document: unknown, unknowns: string[] = []): Engine {
  return {
    id: 'builtin',
    start: async function* (): AsyncIterable<AssistantEvent> {
      yield { type: 'proposal', document, unknowns }
      yield { type: 'end' }
    }
  }
}

describe('Create writes the proposal', () => {
  async function runIt() {
    await propose()
    await screen.findByRole('region', { name: 'The proposal' }, { timeout: 15_000 })
  }

  it('writes it under the slug, with the desk’s id and the name that was typed', async () => {
    const { sent } = serve()
    draw()
    await runIt()
    await nameIt('Vendor Onboarding')
    fireEvent.click(createButton())
    await waitFor(() => expect(sent.length).toBe(2))
    expect(sent[0]!.path).toBe('packs/vendor-onboarding.pack.json')
    // The bytes that were sent, read back and compared with the document that
    // was shown: the file is the shaped snapshot and nothing else.
    const written = JSON.parse(String(sent[0]!.body.content)) as Record<string, unknown>
    expect(written.id).toBe('https://example.invalid/judgment-packs/vendor-onboarding')
    expect(written.title).toBe('Vendor Onboarding')
    expect(written.version).toBe('0.1.0')
    expect(written.specVersion).toBe(scenario.documents.DRAFT_V2.specVersion)
    expect(written.rules).toEqual(scenario.documents.DRAFT_V2.rules)
    expect(written.outcomes).toEqual(scenario.documents.DRAFT_V2.outcomes)
    // And it is registered under the same id, exactly as a template create is.
    expect(sent[1]!.path).toBe('jpack.json')
    expect(String(sent[1]!.body.content)).toContain('vendor-onboarding')
  })

  it('chooses the proposal as the source, and says the name field won', async () => {
    serve()
    draw()
    await runIt()
    await nameIt('Vendor Onboarding')
    expect(screen.getByLabelText('Template').textContent).toContain('The assistant’s proposal')
    expect(screen.getByText('Named from the field above, not from the proposal.')).toBeTruthy()
  })

  it('writes what was shown, even where the document reads differently each time', async () => {
    // The canonicalization the run hook does, measured from the far end: an
    // engine may put a live object on `document`, and three readings of one
    // getter are three documents. What is written has to be the reading that
    // was displayed.
    let reads = 0
    injected = proposing({
      specVersion: '0.2.0-draft',
      outcomes: [{ id: 'approve' }, { id: 'decline' }],
      rules: [{ id: 'r1' }],
      get question() {
        reads += 1
        return `read ${reads}`
      }
    })
    const { sent } = serve()
    draw()
    await runIt()
    fireEvent.click(screen.getByText('Show document'))
    const shown = JSON.parse(
      (screen.getByLabelText('The proposed document') as HTMLTextAreaElement).value
    ) as { question: string }
    await nameIt('Vendor Onboarding')
    fireEvent.click(createButton())
    await waitFor(() => expect(sent.length).toBe(2))
    const written = JSON.parse(String(sent[0]!.body.content)) as { question: string }
    expect(written.question).toBe(shown.question)
  })

  it('will not create while the assistant is still running, and says why', async () => {
    serve({ hang: true })
    draw()
    await propose()
    await waitFor(() => expect(runtime!.opened.length).toBe(1))
    fireEvent.change(screen.getByLabelText('Name (required)'), {
      target: { value: 'Vendor Onboarding' }
    })
    await waitFor(() => expect(createButton().disabled).toBe(true))
    expect(createButton().title).toBe(
      'The assistant is still running. Stop it or wait for it to end.'
    )
  })

  it('will not create a proposal that could not be read, and quotes the reason', async () => {
    // A document with a cycle in it: the run hook refuses it as unreadable and
    // puts its own sentence on the stream, and no proposal arrives. The source
    // is still the proposal — a run that ended with nothing is a state this
    // dialog reports rather than hides — so Create refuses and says so.
    const cyclic: Record<string, unknown> = { specVersion: '0.2.0-draft' }
    cyclic.self = cyclic
    injected = proposing(cyclic)
    const { sent } = serve()
    draw()
    await propose()
    await waitFor(() =>
      expect(screen.getByLabelText('Template').textContent).toContain('The assistant’s proposal')
    )
    fireEvent.change(screen.getByLabelText('Name (required)'), {
      target: { value: 'Vendor Onboarding' }
    })
    await waitFor(() => expect(createButton().disabled).toBe(true))
    expect(createButton().title).toContain('could not be read as JSON data')
    expect(sent).toEqual([])
  })

  it('will not create where the key went away mid-run, and quotes the refusal', async () => {
    serve({ refuse: true })
    draw()
    await propose()
    await waitFor(() =>
      expect(screen.getByLabelText('Template').textContent).toContain('The assistant’s proposal')
    )
    fireEvent.change(screen.getByLabelText('Name (required)'), {
      target: { value: 'Vendor Onboarding' }
    })
    await waitFor(() => expect(createButton().disabled).toBe(true))
    // The endpoint's own status, carried through: the dialog quotes the run
    // rather than inventing a sentence about a refusal it did not make.
    expect(createButton().title).toContain('409')
  })

  it('still writes a template where the author switches back to one', async () => {
    const { sent } = serve()
    draw()
    await runIt()
    // Back to the runtime's own example, with the proposal still on offer.
    fireEvent.click(screen.getByLabelText('Template'))
    fireEvent.click(await screen.findByRole('option', { name: 'minimal-expense-approval' }))
    await nameIt('Vendor Onboarding')
    fireEvent.click(createButton())
    await waitFor(() => expect(sent.length).toBe(2))
    const written = JSON.parse(String(sent[0]!.body.content)) as Record<string, unknown>
    // The example's members, under the desk's own identity.
    expect(written.id).toBe('https://example.invalid/judgment-packs/vendor-onboarding')
    expect(written.rules).toEqual([{ id: 'r1' }])
    expect(screen.queryByText('Named from the field above, not from the proposal.')).toBeNull()
  })
})
