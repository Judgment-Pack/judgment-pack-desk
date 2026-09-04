/**
 * Running the draft, and the four facts about that call the pane may not get
 * wrong.
 *
 * `pack` XOR `pack_id`; `rehearsal` exactly where advertised and an explicit
 * click where it is not; a refusal rendered as the runtime's answer with no
 * disposition; and the foot printing the payload's **own** `packId` rather
 * than the project's decision id.
 */
import { cleanup, fireEvent, render, screen, waitFor, within } from '@testing-library/react'
import { QueryClientProvider } from '@tanstack/react-query'
import { readFileSync } from 'node:fs'
import { join } from 'node:path'
import { afterEach, describe, expect, it, vi } from 'vitest'
import { McpContext } from '../../mcp/McpProvider'
import { connected, stubClient, testQueryClient, type ToolHandler } from '../../testing/harness'
import { TryItPane, draftRequirements } from './TryItPane'

const PACK_TEXT = readFileSync(
  join(import.meta.dirname, '..', '__fixtures__', 'full.pack.json'),
  'utf8'
)

afterEach(() => {
  cleanup()
  vi.unstubAllGlobals()
})

/**
 * A completed run, in the shape the runtime answers with.
 *
 * `packId` is the pack document's **own** `id` — a URI — because that is what
 * the runtime echoes. Printing the project's decision id there would be the
 * desk substituting a value the payload does not carry.
 */
const PAYLOAD = {
  outputVersion: '2',
  tool: { name: 'jpack', version: '0.19.0' },
  command: 'evaluate',
  status: 'ok',
  experimental: true,
  rehearsal: true,
  conformanceClaimReference: 'CONFORMANCE.md',
  specVersion: '0.2.0-draft',
  evaluatorSpecVersion: '0.2.0-draft',
  packId: 'https://example.invalid/judgment-packs/vendor-onboarding',
  packVersion: '1.2.0',
  disposition: { kind: 'outcome', outcomeId: 'approve', reasons: [] },
  trace: []
}

function draw(
  handlers: Record<string, ToolHandler>,
  options: { rehearsalSupported?: boolean; buffer?: string } = {}
) {
  const stub = stubClient(handlers)
  const queryClient = testQueryClient()
  const paint = (buffer: string) => (
    <QueryClientProvider client={queryClient}>
      <McpContext.Provider
        value={connected({
          client: stub.client,
          rehearsalSupported: options.rehearsalSupported ?? true
        })}
      >
        <TryItPane
          buffer={buffer}
          packId="vendor-onboarding"
          rehearsalSupported={options.rehearsalSupported ?? true}
          connected
          onClose={() => {}}
        />
      </McpContext.Provider>
    </QueryClientProvider>
  )
  const view = render(paint(options.buffer ?? PACK_TEXT))
  // The buffer is the one input this pane does not own: the author is typing
  // in the editor beside it. A case that cannot move it cannot ask what the
  // pane does when it moves.
  return { ...view, calls: stub.calls, retype: (buffer: string) => view.rerender(paint(buffer)) }
}

const ANSWERS: Record<string, ToolHandler> = {
  experimental_evaluate: () => ({ text: JSON.stringify(PAYLOAD) })
}

describe('which pack a run is about', () => {
  it('sends the buffer as pack, and no pack_id beside it', async () => {
    const { calls } = draw(ANSWERS)
    fireEvent.click(screen.getByRole('button', { name: 'Run' }))
    await waitFor(() => expect(calls).toHaveLength(1))
    expect(calls[0]!.args.pack).toBe(PACK_TEXT)
    expect(calls[0]!.args).not.toHaveProperty('pack_id')
  })

  it('sends pack_id for the saved pack, and no pack beside it', async () => {
    const { calls } = draw(ANSWERS)
    fireEvent.click(screen.getByRole('radio', { name: 'the saved pack' }))
    fireEvent.click(screen.getByRole('button', { name: 'Run' }))
    await waitFor(() => expect(calls).toHaveLength(1))
    expect(calls[0]!.args.pack_id).toBe('vendor-onboarding')
    expect(calls[0]!.args).not.toHaveProperty('pack')
  })

  it('says which of the two a run was about', async () => {
    draw(ANSWERS)
    fireEvent.click(screen.getByRole('button', { name: 'Run' }))
    await waitFor(() => expect(screen.getByText('from the draft in the editor')).toBeTruthy())
  })
})

describe('the rehearsal declaration', () => {
  it('is sent where the runtime advertises the argument', async () => {
    const { calls } = draw(ANSWERS, { rehearsalSupported: true })
    fireEvent.click(screen.getByRole('button', { name: 'Run' }))
    await waitFor(() => expect(calls).toHaveLength(1))
    expect(calls[0]!.args.rehearsal).toBe(true)
  })

  it('requires an explicit second click where it is not, and says why', async () => {
    // `auditWriter` runs for **every** call including a text pack, and only the
    // declaration suppresses the record. A desk that probed a draft silently on
    // such a runtime would be appending decisions nobody took.
    const { calls } = draw(ANSWERS, { rehearsalSupported: false })
    fireEvent.click(screen.getByRole('button', { name: 'Run' }))
    expect(calls).toHaveLength(0)
    expect(screen.getByText(/appends one record to it/)).toBeTruthy()
    fireEvent.click(screen.getByRole('button', { name: 'Run and record it' }))
    await waitFor(() => expect(calls).toHaveLength(1))
    expect(calls[0]!.args).not.toHaveProperty('rehearsal')
  })

  it('disarms the confirmation when what would be sent changes', async () => {
    const { calls } = draw(ANSWERS, { rehearsalSupported: false })
    fireEvent.click(screen.getByRole('button', { name: 'Run' }))
    expect(screen.getByRole('button', { name: 'Run and record it' })).toBeTruthy()
    fireEvent.change(screen.getByLabelText('Facts'), { target: { value: '{"a":1}' } })
    // A confirmation cannot outlive the thing it confirmed.
    expect(screen.getByRole('button', { name: 'Run' })).toBeTruthy()
    expect(calls).toHaveLength(0)
  })

  it('disarms it when the editor moves, which is most of what would be sent', async () => {
    // The confirmation was a flag, and the flag survived every keystroke in
    // the editor beside the pane — so the second press recorded a run over
    // bytes nobody was asked about.
    const { calls, retype } = draw(ANSWERS, { rehearsalSupported: false })
    fireEvent.click(screen.getByRole('button', { name: 'Run' }))
    expect(screen.getByRole('button', { name: 'Run and record it' })).toBeTruthy()
    retype(`${PACK_TEXT}\n`)
    expect(screen.getByRole('button', { name: 'Run' })).toBeTruthy()
    expect(screen.queryByText(/appends one record to it/)).toBeNull()
    expect(calls).toHaveLength(0)
    // And the confirmation that follows is about the bytes on screen now.
    fireEvent.click(screen.getByRole('button', { name: 'Run' }))
    fireEvent.click(screen.getByRole('button', { name: 'Run and record it' }))
    await waitFor(() => expect(calls).toHaveLength(1))
    expect(calls[0]!.args.pack).toBe(`${PACK_TEXT}\n`)
  })
})

describe('the evidence rows', () => {
  it('come from the draft’s own requirements, not the saved pack’s', () => {
    const draft = PACK_TEXT.replace('"insurance-cert"', '"insurance-certificate"')
    draw(ANSWERS, { buffer: draft })
    expect(screen.getByText('screening-report')).toBeTruthy()
    expect(screen.getByText('insurance-certificate')).toBeTruthy()
    expect(screen.queryByText('insurance-cert')).toBeNull()
  })

  it('send only what was stated, and never a key for a requirement that is gone', async () => {
    const { calls } = draw(ANSWERS)
    const row = screen.getByRole('radiogroup', { name: 'screening-report' })
    fireEvent.click(within(row).getByRole('radio', { name: 'present' }))
    fireEvent.click(screen.getByRole('button', { name: 'Run' }))
    await waitFor(() => expect(calls).toHaveLength(1))
    expect(JSON.parse(String(calls[0]!.args.evidence))).toEqual({ 'screening-report': 'present' })
  })

  it('omits the key entirely where nothing was stated', async () => {
    const { calls } = draw(ANSWERS)
    fireEvent.click(screen.getByRole('button', { name: 'Run' }))
    await waitFor(() => expect(calls).toHaveLength(1))
    // Absence is the omitted key, which is what makes every requirement
    // unknown. An empty document is refused as malformed input.
    expect(calls[0]!.args).not.toHaveProperty('evidence')
  })

  it('reads nothing out of a buffer that does not parse, rather than throwing', () => {
    expect(draftRequirements('{"evidenceRequirements": [')).toEqual([])
    expect(draftRequirements('{"evidenceRequirements": {}}')).toEqual([])
    expect(draftRequirements('{"evidenceRequirements": [{}, {"id": "a"}]}')).toEqual(['a'])
  })
})

describe('what a run answers with', () => {
  it('prints the payload’s own packId, not the project’s decision id', async () => {
    draw(ANSWERS)
    fireEvent.click(screen.getByRole('button', { name: 'Run' }))
    // Twice on the page: `EvaluationView`'s own detail table prints it, and so
    // does the foot. Both are the payload's, and neither is the project's id.
    await waitFor(() =>
      expect(
        screen.getAllByText('https://example.invalid/judgment-packs/vendor-onboarding').length
      ).toBeGreaterThan(0)
    )
    const foot = screen.getByText('packVersion 1.2.0').parentElement!
    expect(foot.textContent).toContain('https://example.invalid/judgment-packs/vendor-onboarding')
    expect(foot.textContent).not.toContain('vendor-onboarding · ')
  })

  it('goes stale the moment the buffer moves', async () => {
    const stub = stubClient(ANSWERS)
    const Harness = ({ buffer }: { buffer: string }) => (
      <QueryClientProvider client={testQueryClient()}>
        <McpContext.Provider value={connected({ client: stub.client, rehearsalSupported: true })}>
          <TryItPane
            buffer={buffer}
            packId="vendor-onboarding"
            rehearsalSupported
            connected
            onClose={() => {}}
          />
        </McpContext.Provider>
      </QueryClientProvider>
    )
    const view = render(<Harness buffer={PACK_TEXT} />)
    fireEvent.click(screen.getByRole('button', { name: 'Run' }))
    await waitFor(() => expect(screen.getAllByText(/approve/).length).toBeGreaterThan(0))
    expect(screen.queryByText(/has changed since this ran/)).toBeNull()
    view.rerender(<Harness buffer={`${PACK_TEXT}\n`} />)
    // A disposition is the answer to the bytes that were sent, and nothing in
    // it says which of the bytes since then it would still be the answer to.
    expect(screen.getByText(/has changed since this ran/)).toBeTruthy()
  })

  it('goes stale when the facts move, which are sent too', async () => {
    // The bytes were the only input the staleness ever watched, and they are
    // not the only input: facts and evidence are sent with them, and an answer
    // to other facts is not an answer to these.
    const stub = stubClient(ANSWERS)
    render(
      <QueryClientProvider client={testQueryClient()}>
        <McpContext.Provider value={connected({ client: stub.client, rehearsalSupported: true })}>
          <TryItPane
            buffer={PACK_TEXT}
            packId="vendor-onboarding"
            rehearsalSupported
            connected
            onClose={() => {}}
          />
        </McpContext.Provider>
      </QueryClientProvider>
    )
    fireEvent.click(screen.getByRole('button', { name: 'Run' }))
    await waitFor(() => expect(screen.getAllByText(/approve/).length).toBeGreaterThan(0))
    expect(screen.queryByText(/has changed since this ran/)).toBeNull()
    fireEvent.change(screen.getByLabelText('Facts'), {
      target: { value: '{"request": {"amount": "1"}}' }
    })
    expect(screen.getByText(/has changed since this ran/)).toBeTruthy()
  })

  it('renders a preflight refusal as the runtime’s answer, with no disposition', async () => {
    draw({
      experimental_evaluate: () => ({
        isError: true,
        text: 'the pack is not conformant',
        structured: {
          evaluationError: {
            class: 'invalid-input',
            phase: 'preflight',
            evaluatorSpecVersion: '0.2.0-draft'
          },
          diagnostics: [
            {
              code: 'JPS-STRUCTURE-REQUIRED',
              severity: 'error',
              message: 'onUnknown is required.'
            }
          ]
        }
      })
    })
    fireEvent.click(screen.getByRole('button', { name: 'Run' }))
    await waitFor(() => expect(screen.getByText('class: invalid-input')).toBeTruthy())
    expect(screen.getByText('phase: preflight')).toBeTruthy()
    expect(screen.getByText('onUnknown is required.')).toBeTruthy()
    expect(screen.getByText(/A refusal carries no disposition/)).toBeTruthy()
    // Mid-edit a refusal is the ordinary answer. It is not dressed as one.
    expect(screen.queryByText(/outcomeId/)).toBeNull()
  })

  it('replaces the answer before it, rather than standing beside it', async () => {
    // **Two answers about two different sets of bytes, on screen at once.** The
    // result was held until the next *success* and the refusal came from the
    // mutation, so a run that was refused after one that was not printed a
    // disposition above "the runtime refused this run" — with nothing saying
    // which of them was current.
    let refuse = false
    const stub = stubClient({
      experimental_evaluate: () =>
        refuse
          ? {
              isError: true,
              text: 'the pack is not conformant',
              structured: {
                evaluationError: {
                  class: 'invalid-input',
                  phase: 'preflight',
                  evaluatorSpecVersion: '0.2.0-draft'
                },
                diagnostics: []
              }
            }
          : { text: JSON.stringify(PAYLOAD) }
    })
    render(
      <QueryClientProvider client={testQueryClient()}>
        <McpContext.Provider value={connected({ client: stub.client, rehearsalSupported: true })}>
          <TryItPane
            buffer={PACK_TEXT}
            packId="vendor-onboarding"
            rehearsalSupported
            connected
            onClose={() => {}}
          />
        </McpContext.Provider>
      </QueryClientProvider>
    )
    fireEvent.click(screen.getByRole('button', { name: 'Run' }))
    await waitFor(() => expect(screen.getAllByText(/approve/).length).toBeGreaterThan(0))

    refuse = true
    fireEvent.click(screen.getByRole('button', { name: 'Run' }))
    await waitFor(() => expect(screen.getByText('class: invalid-input')).toBeTruthy())
    expect(screen.queryByText(/outcomeId/)).toBeNull()
    expect(screen.queryByText('packVersion 1.2.0')).toBeNull()
  })

  it('is stale when what was refused is no longer what would be sent', async () => {
    // A refusal is the runtime's answer to bytes, exactly as a disposition is.
    // It never went stale at all, so an answer about a document the author had
    // since fixed stood there saying it was not conformant.
    const stub = stubClient({
      experimental_evaluate: () => ({
        isError: true,
        text: 'the pack is not conformant',
        structured: {
          evaluationError: {
            class: 'invalid-input',
            phase: 'preflight',
            evaluatorSpecVersion: '0.2.0-draft'
          },
          diagnostics: []
        }
      })
    })
    const Harness = ({ buffer }: { buffer: string }) => (
      <QueryClientProvider client={testQueryClient()}>
        <McpContext.Provider value={connected({ client: stub.client, rehearsalSupported: true })}>
          <TryItPane
            buffer={buffer}
            packId="vendor-onboarding"
            rehearsalSupported
            connected
            onClose={() => {}}
          />
        </McpContext.Provider>
      </QueryClientProvider>
    )
    const view = render(<Harness buffer={PACK_TEXT} />)
    fireEvent.click(screen.getByRole('button', { name: 'Run' }))
    await waitFor(() => expect(screen.getByText('class: invalid-input')).toBeTruthy())
    expect(screen.queryByText(/has changed since this ran/)).toBeNull()
    view.rerender(<Harness buffer={`${PACK_TEXT}\n`} />)
    expect(screen.getByText(/has changed since this ran/)).toBeTruthy()
  })

  it('says which source a run was from, whatever the toggle says now', async () => {
    // The pill read the *current* toggle, so flipping it relabelled an answer
    // that was never about it: a run of the draft became "from the pack on
    // disk" without anything having run.
    draw(ANSWERS)
    fireEvent.click(screen.getByRole('button', { name: 'Run' }))
    await waitFor(() => expect(screen.getByText('from the draft in the editor')).toBeTruthy())
    fireEvent.click(screen.getByRole('radio', { name: 'the saved pack' }))
    expect(screen.getByText('from the draft in the editor')).toBeTruthy()
    expect(screen.queryByText('from the pack on disk')).toBeNull()
    // And it is stale, because the source is part of what would be sent.
    expect(screen.getByText(/has changed since this ran/)).toBeTruthy()
  })
})

describe('the honesty line', () => {
  it('says what a rehearsal does and does not touch', () => {
    draw(ANSWERS, { rehearsalSupported: true })
    expect(
      screen.getByText(/Nothing is saved, nothing is recorded, and no reviewed set is consulted/)
    ).toBeTruthy()
  })

  it('says what changes on a runtime that does not take the declaration', () => {
    draw(ANSWERS, { rehearsalSupported: false })
    const said = screen.getByText(/does not take the rehearsal declaration/)
    expect(said.textContent).toContain('no reviewed set is consulted')
    expect(said.textContent).toContain('appends one record')
  })
})
