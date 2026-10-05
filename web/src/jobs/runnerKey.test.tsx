/**
 * Runner's key as the page holds it: read only in its four shapes, and asked
 * again while Help & About shows it, so that what the page says follows
 * Runner (review round 1, MEDIUM 2: it rode the configuration query, which is
 * read once and kept).
 */
import { QueryClientProvider, useQueryClient } from '@tanstack/react-query'
import { act, cleanup, render, screen } from '@testing-library/react'
import { afterEach, describe, expect, it, vi } from 'vitest'
import { testQueryClient } from '../testing/harness'
import { RunnerSignatures } from '../routes/GatesHelp'
import { RUNNER_KEY_REFRESH_MS, refreshRunnerKey, runnerKeyOf, useRunnerKey } from './runnerKey'

afterEach(() => { cleanup(); vi.useRealTimers(); vi.unstubAllGlobals() })

const KEY = { state: 'signed', publicKey: 'a'.repeat(64), keyId: 'b'.repeat(32) }

/**
 * Desk's answers to `GET /api/runner-key`, in turn, the last kept; answers
 * how many times it was asked.
 */
function answering(...answers: unknown[]): () => number {
  let asked = 0
  vi.stubGlobal('fetch', async (url: string) => {
    if (!String(url).includes('/api/runner-key')) return { ok: false, status: 404, statusText: '', text: async () => '{}' }
    const answer = answers[Math.min(asked++, answers.length - 1)]
    return { ok: true, status: 200, statusText: '', text: async () => JSON.stringify({ runnerKey: answer }) }
  })
  return () => asked
}

let refresh: (() => Promise<void>) | undefined

/** What Gates shows of Runner's key, as the polled query holds it. */
function Shown() {
  const { data } = useRunnerKey()
  const client = useQueryClient()
  refresh = () => refreshRunnerKey(client)
  return <div data-testid="shown">{data === undefined ? 'unasked' : data === null ? 'no Runner' : data.state}<RunnerSignatures runnerKey={data ?? undefined} /></div>
}

function show() {
  render(<QueryClientProvider client={testQueryClient()}><Shown /></QueryClientProvider>)
}

/** Lets the interval pass, and the answer it asks for land. */
async function oneInterval() {
  await act(async () => { await vi.advanceTimersByTimeAsync(RUNNER_KEY_REFRESH_MS) })
}

describe('Runner’s key, asked again while it is shown', () => {
  it('goes from starting to signed without a reload', async () => {
    vi.useFakeTimers({ shouldAdvanceTime: true })
    answering({ state: 'starting' }, KEY)
    show()
    expect(await screen.findByText(/Runner has not started on this desk yet/)).toBeTruthy()
    await oneInterval()
    expect(await screen.findByText(/Runner signs the record of each Jobs run/)).toBeTruthy()
    expect(screen.getByTestId('shown').textContent).toContain('a'.repeat(64))
  })

  it('stops saying runs are signed once Runner has refused its key', async () => {
    vi.useFakeTimers({ shouldAdvanceTime: true })
    answering(KEY, { state: 'unsigned', reason: 'runner-refused', detail: 'it must be a regular file' })
    show()
    expect(await screen.findByText(/Runner signs the record of each Jobs run/)).toBeTruthy()
    await oneInterval()
    expect(await screen.findByText(/Runner refused its signing key when it started: it must be a regular file\./)).toBeTruthy()
    expect(screen.getByTestId('shown').textContent).not.toContain('Runner signs the record')
    expect(screen.getByTestId('shown').textContent).not.toContain('a'.repeat(64))
  })

  it('says Runner is not running once it stops answering, and never that it signs', async () => {
    vi.useFakeTimers({ shouldAdvanceTime: true })
    answering(KEY, { state: 'not-running', detail: 'runner unavailable' })
    show()
    expect(await screen.findByText(/Runner signs the record of each Jobs run/)).toBeTruthy()
    await oneInterval()
    expect(await screen.findByText(/Runner is not running on this desk now, so no Jobs run is run or signed\. Runner did not start: runner unavailable\./)).toBeTruthy()
    expect(screen.getByTestId('shown').textContent).not.toContain('Runner signs the record')
  })

  it('asks again at once when a Jobs action completes, without waiting for the interval', async () => {
    vi.useFakeTimers({ shouldAdvanceTime: false })
    const asked = answering({ state: 'starting' }, KEY)
    show()
    await act(async () => { await vi.advanceTimersByTimeAsync(0) })
    expect(asked()).toBe(1)
    await act(async () => { await refresh!() })
    await act(async () => { await vi.advanceTimersByTimeAsync(0) })
    expect(asked()).toBe(2)
    expect(screen.getByTestId('shown').textContent).toContain('Runner signs the record')
  })

  it('shows nothing where the desk has no Runner', async () => {
    answering(null)
    show()
    expect(await screen.findByText('no Runner')).toBeTruthy()
    expect(screen.getByTestId('shown').textContent).toBe('no Runner')
  })
})

describe('runnerKeyOf', () => {
  it.each([
    [KEY, KEY],
    [{ state: 'unsigned', reason: 'in-use' }, { state: 'unsigned', reason: 'in-use' }],
    [{ state: 'unsigned', reason: 'runner-refused', detail: 'it must be a regular file' }, { state: 'unsigned', reason: 'runner-refused', detail: 'it must be a regular file' }],
    [{ state: 'starting' }, { state: 'starting' }],
    [{ state: 'not-running', detail: 'runner unavailable' }, { state: 'not-running', detail: 'runner unavailable' }],
    [{ state: 'not-running' }, { state: 'not-running' }]
  ])('reads %j', (value, want) => {
    expect(runnerKeyOf(value)).toEqual(want)
  })

  it.each([
    undefined, null, 'signed', [], {},
    { state: 'signed', publicKey: 'A'.repeat(64), keyId: 'b'.repeat(32) },
    { state: 'signed', publicKey: 'a'.repeat(63), keyId: 'b'.repeat(32) },
    { state: 'signed', publicKey: 'a'.repeat(64) },
    { state: 'unsigned', reason: 'another reason' },
    { state: 'unsigned', reason: 'custody', detail: 7 },
    { state: 'unsigned' },
    { state: 'not-running', detail: 7 },
    { state: 'running' }
  ])('reads nothing from %j', value => {
    expect(runnerKeyOf(value)).toBeUndefined()
  })
})
