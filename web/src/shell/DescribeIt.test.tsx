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
import { act, cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react'
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
    loadEngine: async (id: 'vercel' | 'vercel') => injected ?? original.loadEngine(id)
  }
})

import { scriptedModel } from '../assistant/conformance/scriptedModel'
import { scriptedWebSocket } from '../assistant/conformance/scriptedServer'
import scenario from '../assistant/conformance/scenario.json'
import { DeskConfigFixture } from '../config/DeskConfigProvider'
import { decodeDeskConfig, effectiveConfig, type EffectiveConfig } from '../config/deskConfig'
import { McpContext } from '../mcp/McpProvider'
import { ASSISTANT_KEY_QUERY_KEY } from '../assistant/queries'
import { connected, stubClient, testQueryClient } from '../testing/harness'
import { AppShell } from './AppShell'
import { CreatePackDialog } from './CreatePackDialog'
import { SLOT_LOST } from './DescribeIt'
import { forgetAuthorBridge } from './authorBridge'
import { forgetSession, giveThisPageASessionForTesting } from '../mcp/session'
import { forgetConsole } from './consoleLog'

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

/** The runtime's schema, as far as this desk reads it: the format version. */
const SCHEMA = JSON.stringify({
  required: ['specVersion', 'id', 'version', 'title', 'outcomes', 'rules'],
  properties: {
    specVersion: { const: '0.2.0-draft' },
    id: { type: 'string' },
    version: { type: 'string' },
    title: { type: 'string' },
    outcomes: { type: 'array' },
    rules: { type: 'array' }
  }
})

/** Every member the format declares, which is all of them. */
const DECLARED = new Set([
  'specVersion',
  'id',
  'version',
  'title',
  'description',
  'decision',
  'applicability',
  'evidenceRequirements',
  'sources',
  'outcomes',
  'rules',
  'exceptions',
  'fallbackOutcome',
  'escalation',
  'metadata',
  'extensions'
])

/**
 * A stand-in for the runtime's validator, and honest about being one.
 *
 * It answers the one question these cases are about — would the runtime call
 * this a pack — on the two grounds the review names: the schema is
 * `additionalProperties: false`, and `specVersion` is a `const`. The real
 * validator runs over the real thing in the live drive; what is held here is
 * that the dialog **asks before it writes** and refuses what comes back.
 */
function validateStub(text: string): { text: string } {
  const document = JSON.parse(text) as Record<string, unknown>
  const diagnostics = [
    ...Object.keys(document)
      .filter((member) => !DECLARED.has(member))
      .map((member) => ({
        code: 'additional-property',
        codeStability: 'stable',
        layer: 'structural',
        severity: 'error',
        instancePath: `/${member}`,
        message: `additional properties are not allowed: "${member}" — remove it, or say what it is.`
      })),
    ...(document.specVersion === '0.2.0-draft'
      ? []
      : [
          {
            code: 'const-mismatch',
            codeStability: 'stable',
            layer: 'carrier',
            severity: 'error',
            instancePath: '/specVersion',
            message: 'specVersion must be "0.2.0-draft"; this document says otherwise.'
          }
        ])
  ]
  return {
    text: JSON.stringify({
      status: diagnostics.length === 0 ? 'valid' : 'invalid',
      layers: [
        { name: 'carrier', status: 'passed' },
        { name: 'structural', status: diagnostics.length === 0 ? 'passed' : 'failed' },
        ...(diagnostics.length === 0 ? [{ name: 'semantic', status: 'passed' }] : [])
      ],
      diagnostics
    })
  }
}

interface Sent {
  path: string
  body: Record<string, unknown>
}

let runtime: ReturnType<typeof scriptedWebSocket> | null = null
/** Every tool call the desk's own connection made, for the cases about that. */
let calls: { name: string; args: Record<string, unknown> }[] = []
const stubCalls = () => calls

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
  persist,
  secondPrompt = 'ok',
  validateSupported = true,
  testPrompt = 'none'
}: {
  deskConfig: EffectiveConfig
  persist: boolean
  /** What the runtime does with the **second** `prompts/get` it is asked. */
  secondPrompt?: 'ok' | 'reject' | 'hang'
  /** Whether the connection advertises the tool the check needs. */
  validateSupported?: boolean
  /**
   * What this runtime does about `test_pack`, which the refutation pass needs.
   *
   * `none` is the ordinary case here — most of this file runs at tier `off`.
   * `reject` advertises it and refuses the read, which is the state that
   * deadlocked a session: the authoring prompt had arrived, the wait never
   * settled, and this section said "running" with nothing running.
   */
  testPrompt?: 'none' | 'ok' | 'reject'
}) {
  const [open, setOpen] = useState(true)
  /** The desk-level file, as an admin might rewrite it while this is open. */
  const [live, setLive] = useState(deskConfig)
  const [stub] = useState(() =>
    stubClient(
      {
        list_examples: () => ({ text: EXAMPLES }),
        get_example: () => ({ text: TEMPLATE }),
        get_schema: () => ({ text: SCHEMA }),
        validate: (args) => validateStub(String(args.document))
      },
      {
        prompts: {
          author_pack: { text: 'The runtime’s authoring prompt, with the policy in it.' },
          ...(testPrompt === 'none'
            ? {}
            : {
                test_pack: {
                  text: 'The runtime’s testing prompt.',
                  ...(testPrompt === 'reject' ? { fails: 'the runtime refused test_pack' } : {})
                }
              })
        }
      }
    )
  )
  calls = stub.calls
  const [connection] = useState(() => {
    // A runtime that answers the first prompt and then does not. The second
    // Propose is the case the association between an offered proposal and the
    // submission that produced it is about.
    const inner = stub.client as unknown as {
      getPrompt: (params: { name: string; arguments?: Record<string, string> }) => Promise<unknown>
    }
    let asked = 0
    const client = {
      ...(stub.client as unknown as Record<string, unknown>),
      getPrompt: async (params: { name: string; arguments?: Record<string, string> }) => {
        asked += 1
        if (asked > 1 && secondPrompt === 'reject') {
          throw new Error('the runtime refused to serve author_pack')
        }
        if (asked > 1 && secondPrompt === 'hang') return new Promise(() => {})
        return inner.getPrompt(params)
      }
    }
    return connected({
      client: client as never,
      exampleSupported: true,
      schemaSupported: true,
      validateSupported
    })
  })
  return (
    <McpContext.Provider value={connection}>
      <button type="button" onClick={() => setLive(config({ endpoint: null }))}>
        Drop the endpoint
      </button>
      <DeskConfigFixture value={live}>
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

/** A desk whose own configuration read did not produce a file. */
function drawUnread() {
  return drawWith(
    effectiveConfig(undefined, undefined, undefined, {
      path: '/home/someone/.config/jpack-desk/desk.json',
      present: false,
      readFailure: {
        reason: 'the desk could not read it',
        responseReceived: true,
        status: 503,
        source: 'chassis'
      }
    })
  )
}

function draw(
  assistant: unknown = { endpoint: ENDPOINT },
  options: {
    persist?: boolean
    secondPrompt?: 'ok' | 'reject' | 'hang'
    validateSupported?: boolean
    testPrompt?: 'none' | 'ok' | 'reject'
  } = {}
) {
  return drawWith(config(assistant), options)
}

function drawWith(
  deskConfig: EffectiveConfig,
  options: {
    persist?: boolean
    secondPrompt?: 'ok' | 'reject' | 'hang'
    validateSupported?: boolean
    testPrompt?: 'none' | 'ok' | 'reject'
  } = {}
) {
  const router = createMemoryRouter(
    [
      {
        path: '*',
        element: (
          <Mounted
            deskConfig={deskConfig}
            persist={options.persist ?? false}
            secondPrompt={options.secondPrompt}
            validateSupported={options.validateSupported}
            testPrompt={options.testPrompt}
          />
        )
      }
    ],
    { initialEntries: ['/'] }
  )
  const queryClient = testQueryClient()
  return {
    ...render(
      <QueryClientProvider client={queryClient}>
        <RouterProvider router={router} />
      </QueryClientProvider>
    ),
    /** The key store, as the desk reports it. Tests move it. */
    setKey: (present: boolean) =>
      act(() => {
        queryClient.setQueryData(ASSISTANT_KEY_QUERY_KEY, {
          present,
          fingerprint: present ? 'sk-a…wxyz' : ''
        })
      })
  }
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
  // This file ends the desk's session on purpose, and the module state that
  // records the ending outlives a test. `testing/setup.ts` hands every test a
  // session; this hands it back after one has been ended, rather than clearing
  // it — clearing would leave every test here with no session at all.
  giveThisPageASessionForTesting('a-session-this-test-was-given')
})

afterEach(() => {
  cleanup()
  forgetConsole()
  forgetAuthorBridge()
  vi.restoreAllMocks()
  vi.unstubAllGlobals()
  runtime = null
  injected = null
  calls = []
  window.sessionStorage.clear()
  window.localStorage.clear()
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

  it('will not run against a configuration this desk could not read', async () => {
    // **Not "no assistant is configured".** A read that did not produce a file
    // establishes nothing about what is in it, and offering the configure-one
    // repair there sends a reader to a form that will not write either. The
    // control is withheld for the same reason: there is no slot to run
    // against, only an absence nobody established.
    serve()
    drawUnread()
    expect(await screen.findByText(/could not read its own configuration/)).toBeTruthy()
    expect(screen.queryByText(/No assistant is configured on this desk/)).toBeNull()
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
    draw({ endpoint: ENDPOINT, engine: 'vercel', thinking: 'ultra' })
    await openIt()
    expect(await screen.findByText('vercel · a-model · thinking ultra')).toBeTruthy()
  })

  it('offers the enabled set here too, and names what it would run', async () => {
    // **One preference, two surfaces.** The dialog and the Assistant tab read
    // the same hook under the same key, so a pick made in one is the pick the
    // other opens on — and the standing line names it rather than the file's
    // default.
    serve()
    draw({
      endpoint: { ...ENDPOINT, model: 'a-model', models: ['a-model', 'a-second-model'] }
    })
    await openIt()
    const picker = await screen.findByRole('combobox', { name: 'Model' })
    fireEvent.click(picker)
    fireEvent.click(await screen.findByRole('option', { name: 'a-second-model' }))
    expect(await screen.findByText('vercel · a-second-model · thinking off')).toBeTruthy()
  })

  it('offers no picker where nothing is enabled', async () => {
    serve()
    draw({ endpoint: { ...ENDPOINT, model: null, models: [] } })
    await openIt()
    await screen.findByRole('button', { name: 'Propose' })
    expect(screen.queryByRole('combobox', { name: 'Model' })).toBeNull()
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

  it('discards a proposal that had already arrived when the session ends', async () => {
    // **Stopping the run is not enough.** A proposal that arrived before the
    // refusal is state this dialog holds: without a discard, `offered` stays
    // true, the document stays selected, and Create stays willing to write a
    // document the assistant produced over a session the chassis has since
    // refused.
    serve()
    draw()
    await runIt()
    expect(screen.getByLabelText('The proposed document')).toBeTruthy()

    act(() => {
      forgetSession()
    })

    await waitFor(() => expect(screen.queryByLabelText('The proposed document')).toBeNull())
    expect(screen.queryByRole('button', { name: 'Create' })).toBeNull()
    expect(document.body.textContent).toContain(SLOT_LOST)
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
    id: 'vercel',
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
      // A member the format declares, so the check has no quarrel with it, and
      // one the shaping never touches — so what is written is what was read.
      get metadata() {
        reads += 1
        return { note: `read ${reads}` }
      }
    })
    const { sent } = serve()
    draw()
    await runIt()
    fireEvent.click(screen.getByText('Show document'))
    const shown = JSON.parse(
      (screen.getByLabelText('The proposed document') as HTMLTextAreaElement).value
    ) as { metadata: { note: string } }
    await nameIt('Vendor Onboarding')
    fireEvent.click(createButton())
    await waitFor(() => expect(sent.length).toBe(2))
    const written = JSON.parse(String(sent[0]!.body.content)) as { metadata: { note: string } }
    expect(written.metadata.note).toBe(shown.metadata.note)
  })

  it('will not create while the assistant is still running, and says why', async () => {
    // **Measured as a change**, not as a state. A dialog that has not yet been
    // given a name, or whose template has not arrived, is disabled for its own
    // reasons — so the first `waitFor` here would succeed against a Create
    // button that was about to become enabled, and a row that removed the
    // run's own condition would report nothing failing. The claim is that a
    // Create which *was* offered stops being offered while a run is in flight.
    serve({ hang: true })
    draw()
    await nameIt('Vendor Onboarding')
    expect(createButton().disabled).toBe(false)
    await propose()
    await waitFor(() => expect(runtime!.opened.length).toBe(1))
    await waitFor(() => expect(createButton().disabled).toBe(true))
    expect(createButton().title).toBe(
      'The assistant is still running. Stop it or wait for it to end.'
    )
  })

  it('will not create a proposal that could not be read, and quotes the reason', async () => {
    // A document with a cycle in it: the run hook refuses it as unreadable and
    // puts its own sentence on the stream, and no proposal arrives. There is no
    // source to offer — a document that cannot be written is not a choice — and
    // the whole dialog is held with the reason rather than quietly falling back
    // to a template nobody picked.
    const cyclic: Record<string, unknown> = { specVersion: '0.2.0-draft' }
    cyclic.self = cyclic
    injected = proposing(cyclic)
    const { sent } = serve()
    draw()
    await propose()
    await waitFor(() =>
      expect(screen.getAllByText(/could not be read as JSON data/).length).toBeGreaterThan(0)
    )
    expect(screen.getByLabelText('Template').textContent).not.toContain('The assistant’s proposal')
    fireEvent.change(screen.getByLabelText('Name (required)'), {
      target: { value: 'Vendor Onboarding' }
    })
    await waitFor(() => expect(createButton().disabled).toBe(true))
    expect(createButton().title).toContain('could not be read as JSON data')
    fireEvent.click(createButton())
    await new Promise((resolve) => setTimeout(resolve, 50))
    expect(sent).toEqual([])
  })

  it('will not create where the relay refused the model call, and quotes the status', async () => {
    // The chassis answers 409 with its own envelope; the run fails, no proposal
    // arrives, and the dialog quotes the run rather than inventing a sentence
    // about a refusal it did not make. (This is the *relay's* refusal, not the
    // slot going away — that transition is its own case below.)
    const { sent } = serve({ refuse: true })
    draw()
    await propose()
    await waitFor(() => expect(createButton().title).toContain('409'), { timeout: 15_000 })
    fireEvent.change(screen.getByLabelText('Name (required)'), {
      target: { value: 'Vendor Onboarding' }
    })
    await waitFor(() => expect(createButton().disabled).toBe(true))
    expect(sent).toEqual([])
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


describe('a proposal belongs to the submission that produced it', () => {
  /** Propose once, successfully, and name the pack so Create would be offered. */
  async function proposedOnce() {
    const served = serve()
    draw({ endpoint: ENDPOINT }, { secondPrompt: 'reject' })
    await propose()
    await screen.findByRole('region', { name: 'The proposal' }, { timeout: 15_000 })
    await nameIt('Vendor Onboarding')
    expect(createButton().disabled).toBe(false)
    return served
  }

  it('withdraws the first proposal the instant a second Propose is pressed', async () => {
    const { sent } = await proposedOnce()
    fireEvent.change(screen.getByLabelText(/What should this pack decide/), {
      target: { value: 'Something else entirely.' }
    })
    fireEvent.click(screen.getByRole('button', { name: 'Propose' }))
    // The old proposal is gone at the press, not when the next run starts.
    await waitFor(() => expect(screen.queryByRole('region', { name: 'The proposal' })).toBeNull())
    expect(createButton().disabled).toBe(true)
    expect(sent).toEqual([])
  })

  it('does not offer it again when the second submission’s prompt is refused', async () => {
    const { sent } = await proposedOnce()
    fireEvent.change(screen.getByLabelText(/What should this pack decide/), {
      target: { value: 'Something else entirely.' }
    })
    fireEvent.click(screen.getByRole('button', { name: 'Propose' }))
    // The refusal is reported, and the previous run's document is not the
    // answer to a question that was never answered.
    await screen.findByText(/refused to serve author_pack/)
    expect(screen.queryByRole('region', { name: 'The proposal' })).toBeNull()
    expect(createButton().disabled).toBe(true)
    expect(createButton().title).toContain('refused to serve author_pack')
    fireEvent.click(createButton())
    await new Promise((resolve) => setTimeout(resolve, 50))
    expect(sent).toEqual([])
  })

  it('does not offer it again when the second submission is stopped mid-prompt', async () => {
    const served = serve()
    draw({ endpoint: ENDPOINT }, { secondPrompt: 'hang' })
    await propose()
    await screen.findByRole('region', { name: 'The proposal' }, { timeout: 15_000 })
    await nameIt('Vendor Onboarding')
    fireEvent.change(screen.getByLabelText(/What should this pack decide/), {
      target: { value: 'Something else entirely.' }
    })
    fireEvent.click(screen.getByRole('button', { name: 'Propose' }))
    await waitFor(() =>
      expect(screen.getByRole('button', { name: 'Stop' }).hasAttribute('disabled')).toBe(false)
    )
    fireEvent.click(screen.getByRole('button', { name: 'Stop' }))
    await waitFor(() => expect(createButton().disabled).toBe(true))
    expect(screen.queryByRole('region', { name: 'The proposal' })).toBeNull()
    fireEvent.click(createButton())
    await new Promise((resolve) => setTimeout(resolve, 50))
    expect(served.sent).toEqual([])
  })

  it('withdraws a proposal the engine failed after its own end', async () => {
    // The contract's one `end` means the stream cannot carry what happens
    // after it — and an engine that yields `end` and then throws while
    // unwinding has not had a clean run. It used to read as one.
    injected = {
      id: 'vercel',
      start: async function* (): AsyncIterable<AssistantEvent> {
        yield {
          type: 'proposal',
          document: { specVersion: '0.2.0-draft', outcomes: [], rules: [] },
          unknowns: []
        }
        yield { type: 'end' }
        throw new Error('the session could not be closed')
      }
    }
    const { sent } = serve()
    draw()
    await propose()
    await waitFor(() =>
      expect(screen.getAllByText(/the session could not be closed/).length).toBeGreaterThan(0)
    )
    fireEvent.change(screen.getByLabelText('Name (required)'), {
      target: { value: 'Vendor Onboarding' }
    })
    await waitFor(() => expect(createButton().disabled).toBe(true))
    expect(screen.queryByRole('region', { name: 'The proposal' })).toBeNull()
    expect(screen.getByLabelText('Template').textContent).not.toContain('The assistant’s proposal')
    expect(createButton().title).toContain('the session could not be closed')
    fireEvent.click(createButton())
    await new Promise((resolve) => setTimeout(resolve, 50))
    expect(sent).toEqual([])
  })

  it('withdraws a proposal the run failed after', async () => {
    // The contract does not make `proposal` an engine's last non-terminal
    // event. One that proposes and then fails has shown its work and then said
    // the work does not stand.
    injected = {
      id: 'vercel',
      start: async function* (): AsyncIterable<AssistantEvent> {
        yield {
          type: 'proposal',
          document: { specVersion: '0.2.0-draft', outcomes: [], rules: [] },
          unknowns: []
        }
        yield { type: 'error', message: 'the final check did not complete' }
        yield { type: 'end' }
      }
    }
    const { sent } = serve()
    draw()
    await propose()
    await waitFor(() =>
      expect(screen.getAllByText(/the final check did not complete/).length).toBeGreaterThan(0)
    )
    fireEvent.change(screen.getByLabelText('Name (required)'), {
      target: { value: 'Vendor Onboarding' }
    })
    await waitFor(() => expect(createButton().disabled).toBe(true))
    expect(screen.queryByRole('region', { name: 'The proposal' })).toBeNull()
    expect(createButton().title).toContain('the final check did not complete')
    fireEvent.click(createButton())
    await new Promise((resolve) => setTimeout(resolve, 50))
    expect(sent).toEqual([])
  })
})


describe('losing the assistant ends the session', () => {
  it('stops a run in flight, closes its connection, and holds Create', async () => {
    serve({ hang: true })
    const { setKey } = draw({ endpoint: ENDPOINT }, { persist: true })
    await nameIt('Vendor Onboarding')
    await propose()
    await waitFor(() => expect(runtime!.opened.length).toBe(1))
    expect(runtime!.closed).toBe(0)

    // The key leaves the store on this machine, exactly as Remove key does.
    setKey(false)
    await waitFor(() => expect(runtime!.closed).toBe(1))
    expect(await screen.findByText(/no key is stored on this machine/)).toBeTruthy()
    expect(screen.queryByRole('button', { name: 'Propose' })).toBeNull()
    await new Promise((resolve) => setTimeout(resolve, 50))
    expect(runtime!.closed, 'the socket was closed twice').toBe(1)

    // And the run ended rather than merely being abandoned: the run hook
    // refuses a second run while the first is open, so a session that starts
    // once the key is back is the first one's terminal event, observed.
    setKey(true)
    await propose()
    await waitFor(() => expect(runtime!.opened.length).toBe(2))
  })

  it('discards a finished proposal when the key leaves the store', async () => {
    const { sent } = serve()
    const { setKey } = draw({ endpoint: ENDPOINT }, { persist: true })
    await propose()
    await screen.findByRole('region', { name: 'The proposal' }, { timeout: 15_000 })
    await nameIt('Vendor Onboarding')
    expect(createButton().disabled).toBe(false)

    setKey(false)
    await waitFor(() => expect(createButton().disabled).toBe(true))
    expect(screen.queryByRole('region', { name: 'The proposal' })).toBeNull()
    expect(screen.getByLabelText('Template').textContent).not.toContain('The assistant’s proposal')
    fireEvent.click(createButton())
    await new Promise((resolve) => setTimeout(resolve, 50))
    expect(sent).toEqual([])
  })

  it('discards a finished proposal when the endpoint leaves the file', async () => {
    const { sent } = serve()
    draw({ endpoint: ENDPOINT }, { persist: true })
    await propose()
    await screen.findByRole('region', { name: 'The proposal' }, { timeout: 15_000 })
    await nameIt('Vendor Onboarding')
    expect(createButton().disabled).toBe(false)

    // By text and not by role: Radix marks everything outside an open modal
    // `aria-hidden`, which is exactly right and takes this control out of the
    // accessibility tree while the dialog is up.
    fireEvent.click(screen.getByText('Drop the endpoint'))
    await waitFor(() => expect(createButton().disabled).toBe(true))
    expect(await screen.findByText(/No assistant is configured on this desk/)).toBeTruthy()
    expect(screen.queryByRole('region', { name: 'The proposal' })).toBeNull()
    fireEvent.click(createButton())
    await new Promise((resolve) => setTimeout(resolve, 50))
    expect(sent).toEqual([])
  })
})


describe('a route change is a dismissal', () => {
  /**
   * The whole shell, which is where this dialog actually lives.
   *
   * The rail mounts it **above** the route's own `<Routes>`, so a Back, a
   * Forward or any programmatic navigation changes the page underneath without
   * unmounting the dialog or telling it anything. Only the composition can show
   * that; the dialog mounted alone cannot.
   */
  function drawShell() {
    const stub = stubClient(
      {
        list_packs: () => ({ text: JSON.stringify({ status: 'valid', packs: [] }) }),
        list_examples: () => ({ text: EXAMPLES }),
        get_example: () => ({ text: TEMPLATE }),
        get_schema: () => ({ text: SCHEMA }),
        validate: (args) => validateStub(String(args.document))
      },
      {
        prompts: {
          author_pack: { text: 'The runtime’s authoring prompt, with the policy in it.' }
        }
      }
    )
    const deskConfig = config({ endpoint: ENDPOINT })
    const router = createMemoryRouter(
      [
        {
          path: '*',
          element: (
            <McpContext.Provider
              value={connected({
                client: stub.client,
                exampleSupported: true,
                schemaSupported: true,
                validateSupported: true
              })}
            >
              <DeskConfigFixture value={deskConfig}>
                <AppShell>
                  <h1>a route</h1>
                </AppShell>
              </DeskConfigFixture>
            </McpContext.Provider>
          )
        }
      ],
      { initialEntries: ['/'] }
    )
    render(
      <QueryClientProvider client={testQueryClient()}>
        <RouterProvider router={router} />
      </QueryClientProvider>
    )
    return router
  }

  it('closes the dialog and ends the run when the route changes under it', async () => {
    serve({ hang: true })
    const router = drawShell()
    fireEvent.click(await screen.findByRole('button', { name: 'Create a pack' }))
    await screen.findByRole('dialog', { name: 'Create a pack' })
    await propose()
    await waitFor(() => expect(runtime!.opened.length).toBe(1))
    expect(runtime!.closed).toBe(0)

    // A navigation the dialog is never told about: the rail sits above the
    // route, so nothing here unmounts.
    await act(async () => {
      await router.navigate('/packs')
    })
    await waitFor(() => expect(screen.queryByRole('dialog', { name: 'Create a pack' })).toBeNull())
    await waitFor(() => expect(runtime!.closed).toBe(1))
    await new Promise((resolve) => setTimeout(resolve, 50))
    expect(runtime!.closed, 'the socket was closed twice').toBe(1)

    // And the run ended rather than being abandoned: the hook refuses a second
    // run while the first is open, so the next session starting is the first
    // one's terminal event, observed.
    fireEvent.click(screen.getByRole('button', { name: 'Create a pack' }))
    await screen.findByRole('dialog', { name: 'Create a pack' })
    await propose()
    await waitFor(() => expect(runtime!.opened.length).toBe(2))
  })

  it('leaves it open on a hash-only change, which this desk calls no navigation', async () => {
    // Selecting a member writes a hash and stays on the page (`MemberOutline`
    // says so in as many words). A dialog that closed on one would stop a run
    // and discard a proposal over a page that had not changed.
    serve({ hang: true })
    const router = drawShell()
    fireEvent.click(await screen.findByRole('button', { name: 'Create a pack' }))
    await screen.findByRole('dialog', { name: 'Create a pack' })
    await propose()
    await waitFor(() => expect(runtime!.opened.length).toBe(1))
    await act(async () => {
      await router.navigate({ hash: '#/rules/0' })
    })
    await new Promise((resolve) => setTimeout(resolve, 100))
    expect(screen.queryByRole('dialog', { name: 'Create a pack' })).not.toBeNull()
    expect(runtime!.closed, 'the run was ended by a fragment').toBe(0)
  })

  it('closes it when the search changes, which is a different page', async () => {
    serve({ hang: true })
    const router = drawShell()
    fireEvent.click(await screen.findByRole('button', { name: 'Create a pack' }))
    await screen.findByRole('dialog', { name: 'Create a pack' })
    await propose()
    await waitFor(() => expect(runtime!.opened.length).toBe(1))
    await act(async () => {
      await router.navigate({ search: '?edit' })
    })
    await waitFor(() => expect(screen.queryByRole('dialog', { name: 'Create a pack' })).toBeNull())
    await waitFor(() => expect(runtime!.closed).toBe(1))
  })

  it('closes it on a history Back as well', async () => {
    serve({ hang: true })
    const router = drawShell()
    await act(async () => {
      await router.navigate('/packs')
    })
    fireEvent.click(await screen.findByRole('button', { name: 'Create a pack' }))
    await screen.findByRole('dialog', { name: 'Create a pack' })
    await propose()
    await waitFor(() => expect(runtime!.opened.length).toBe(1))
    await act(async () => {
      await router.navigate(-1)
    })
    await waitFor(() => expect(screen.queryByRole('dialog', { name: 'Create a pack' })).toBeNull())
    await waitFor(() => expect(runtime!.closed).toBe(1))
  })
})


describe('a proposal is checked before it is written', () => {
  it('refuses the review’s document before either write, with the runtime’s words', async () => {
    // An otherwise plausible pack carrying a `specVersion` the model invented
    // and a filename it made up. The desk's shaping fills the members the
    // dialog asked about and leaves the rest as written; the runtime is what
    // says whether the result is a pack, and it says no.
    injected = proposing({
      specVersion: '99',
      fileName: 'model-choice.pack.json',
      outcomes: [{ id: 'approve' }, { id: 'decline' }],
      rules: [{ id: 'r1' }]
    })
    const { sent } = serve()
    draw()
    await propose()
    await screen.findByRole('region', { name: 'The proposal' }, { timeout: 15_000 })
    fireEvent.change(screen.getByLabelText('Name (required)'), {
      target: { value: 'Vendor Onboarding' }
    })
    await waitFor(() =>
      expect(screen.getByText(/will not call this document a pack/)).toBeTruthy()
    )
    expect(createButton().disabled).toBe(true)
    // The button's own reason is the check strip's sentence; the runtime's
    // diagnostics are printed in full below it, in the Checks panel's own
    // rendering rather than squeezed into a `title`.
    expect(createButton().title).toContain('will not call this document a pack')
    expect(
      screen.getByRole('list', { name: 'What the runtime said about this document' }).textContent
    ).toContain('additional properties are not allowed: "fileName"')
    fireEvent.click(createButton())
    await new Promise((resolve) => setTimeout(resolve, 50))
    // Neither write. Not the pack, and not the registration.
    expect(sent).toEqual([])
  })

  it('sends the format version the proposal wrote, unchanged, and is refused on it', async () => {
    // **The desk does not repair a proposal into something creatable.** The
    // version a model wrote is what the runtime is asked about, and its
    // diagnostic is what the author reads.
    injected = proposing({
      specVersion: '99',
      outcomes: [{ id: 'approve' }, { id: 'decline' }],
      rules: [{ id: 'r1' }]
    })
    const { sent } = serve()
    draw()
    await propose()
    await screen.findByRole('region', { name: 'The proposal' }, { timeout: 15_000 })
    fireEvent.change(screen.getByLabelText('Name (required)'), {
      target: { value: 'Vendor Onboarding' }
    })
    await waitFor(() =>
      expect(screen.getByText(/will not call this document a pack/)).toBeTruthy()
    )
    // What reached the runtime carried the model's own version.
    const asked = stubCalls().filter((call) => call.name === 'validate')
    expect(asked.length).toBeGreaterThan(0)
    expect(
      asked.some((call) => (JSON.parse(String(call.args.document)) as { specVersion?: unknown }).specVersion === '99')
    ).toBe(true)
    expect(createButton().disabled).toBe(true)
    fireEvent.click(createButton())
    await new Promise((resolve) => setTimeout(resolve, 50))
    expect(sent).toEqual([])
  })

  it('checks the exact bytes it writes, once', async () => {
    const { sent } = serve()
    draw()
    await propose()
    await screen.findByRole('region', { name: 'The proposal' }, { timeout: 15_000 })
    await nameIt('Vendor Onboarding')
    fireEvent.click(createButton())
    await waitFor(() => expect(sent.length).toBe(2))
    // Every `validate` the desk made, and the bytes it was made with: the file
    // that was written is one of them, so what was checked is what was written.
    const checks = stubCalls().filter((call) => call.name === 'validate')
    expect(checks.length).toBeGreaterThan(0)
    expect(checks.map((call) => call.args.document)).toContain(String(sent[0]!.body.content))
  })

  it('will not create where the connection cannot check a document at all', async () => {
    const { sent } = serve()
    draw({ endpoint: ENDPOINT }, { validateSupported: false })
    await propose()
    await screen.findByRole('region', { name: 'The proposal' }, { timeout: 15_000 })
    fireEvent.change(screen.getByLabelText('Name (required)'), {
      target: { value: 'Vendor Onboarding' }
    })
    await waitFor(() => expect(createButton().disabled).toBe(true))
    expect(createButton().title).toContain('serves no validate')
    expect(sent).toEqual([])
  })

  it('does not check a template: the runtime served that document itself', async () => {
    const { sent } = serve()
    draw()
    await nameIt('Template Only')
    fireEvent.click(createButton())
    await waitFor(() => expect(sent.length).toBe(2))
    expect(stubCalls().filter((call) => call.name === 'validate')).toEqual([])
  })
})


describe('a refusal shows the runtime’s whole diagnosis', () => {
  it('prints every diagnostic, with its own code, layer, severity and pointer', async () => {
    // Two independent problems. A dialog that showed one of them would be
    // telling the author their document has one thing wrong with it.
    injected = proposing({
      specVersion: '99',
      fileName: 'model-choice.pack.json',
      outcomes: [{ id: 'approve' }, { id: 'decline' }],
      rules: [{ id: 'r1' }]
    })
    const { sent } = serve()
    draw()
    await propose()
    await screen.findByRole('region', { name: 'The proposal' }, { timeout: 15_000 })
    fireEvent.change(screen.getByLabelText('Name (required)'), {
      target: { value: 'Vendor Onboarding' }
    })
    const list = await screen.findByRole('list', {
      name: 'What the runtime said about this document'
    })
    // Both, and the runtime's own words — punctuation and all, unreworded.
    expect(list.textContent).toContain(
      'additional properties are not allowed: "fileName" — remove it, or say what it is.'
    )
    expect(list.textContent).toContain(
      'specVersion must be "0.2.0-draft"; this document says otherwise.'
    )
    // And the fields a message alone does not carry.
    expect(list.textContent).toContain('additional-property')
    expect(list.textContent).toContain('const-mismatch')
    expect(list.textContent).toContain('structural')
    expect(list.textContent).toContain('carrier')
    expect(list.textContent).toContain('/fileName')
    expect(list.textContent).toContain('/specVersion')
    // Before either write, and instead of one.
    expect(createButton().disabled).toBe(true)
    fireEvent.click(createButton())
    await new Promise((resolve) => setTimeout(resolve, 50))
    expect(sent).toEqual([])
  })
})

describe('the runtime’s testing prompt, which the refutation pass needs', () => {
  const THINKING = { endpoint: ENDPOINT, engine: 'vercel', thinking: 'on' }

  it('runs without a critic where the read is refused, rather than waiting for ever', async () => {
    // **A read that failed is a settled read.** `data` alone stays undefined on
    // the error state, so a `prompts/get` the runtime refused left this section
    // saying "running" with nothing running at all: the authoring prompt had
    // arrived and no engine ever started.
    serve()
    draw(THINKING, { testPrompt: 'reject' })
    await propose()
    // The session runs to its end…
    await waitFor(() => expect(ends()).toHaveLength(1), { timeout: 20000 })
    const stream = document.querySelector('[aria-label="What the assistant did"]')!
    // …and the refutation pass says it had no instructions to work from,
    // rather than a critic having run on this desk's sentence alone.
    expect(
      [...document.querySelectorAll('p')].some((line) =>
        /no test_pack prompt from the runtime/.test(line.textContent ?? '')
      )
    ).toBe(true)
    expect(stream.textContent).not.toContain('REFUTATION PASS')
  }, 30000)

  it('runs with a critic where the runtime serves it', async () => {
    serve()
    draw(THINKING, { testPrompt: 'ok' })
    await propose()
    await waitFor(() => expect(ends()).toHaveLength(1), { timeout: 20000 })
    expect(
      [...document.querySelectorAll('p')].some((line) =>
        /no test_pack prompt from the runtime/.test(line.textContent ?? '')
      )
    ).toBe(false)
  }, 30000)
})
