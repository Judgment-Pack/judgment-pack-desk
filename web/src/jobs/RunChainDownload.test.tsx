import { cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react'
import { afterEach, beforeEach, expect, it, vi } from 'vitest'
import { deskFetch } from '../files/client'
import { RunChainDownload } from './JobsView'
vi.mock('../files/client', async original => ({ ...await original<typeof import('../files/client')>(), deskFetch: vi.fn() }))

const bytes = (...parts: (string | number[])[]) => new Uint8Array(parts.flatMap(part => typeof part === 'string' ? [...new TextEncoder().encode(part)] : part))
// Two lines as Runner ends them, and bytes that are neither UTF-8 nor JSON: a
// page that read the answer as text, or as JSON, would not save these.
const chain = bytes(`{"entryVersion":"1","trail":"${'a'.repeat(32)}","sequence":1}\n`, '{"sequence":2,"note":"', [0xff, 0xfe, 0xc3], '"}  \n')
let saved: { file: string, bytes: Promise<Uint8Array> }[]
let objects: Blob[]
const createObjectURL = URL.createObjectURL, revokeObjectURL = URL.revokeObjectURL
beforeEach(() => {
 saved = []; objects = []
 URL.createObjectURL = (blob: Blob) => { objects.push(blob); return `blob:${objects.length - 1}` }
 URL.revokeObjectURL = () => {}
 vi.spyOn(HTMLAnchorElement.prototype, 'click').mockImplementation(function (this: HTMLAnchorElement) {
  saved.push({ file: this.download, bytes: objects[Number(this.href.slice('blob:'.length))].arrayBuffer().then(buffer => new Uint8Array(buffer)) })
 })
})
afterEach(() => { cleanup(); vi.restoreAllMocks(); vi.mocked(deskFetch).mockReset(); URL.createObjectURL = createObjectURL; URL.revokeObjectURL = revokeObjectURL })

const jsonl = (body: BodyInit | null, status = 200, type = 'application/jsonl') => new Response(body, { status, headers: { 'Content-Type': type } })
// An answer whose body fails after its first bytes, as a transfer does when
// Desk, or the runner behind it, ends it early.
const aborted = () => jsonl(new ReadableStream({ start(controller) { controller.enqueue(chain.slice(0, 20)); controller.error(new TypeError('network error')) } }))
async function attempt() {
 const calls = vi.mocked(deskFetch).mock.calls.length
 fireEvent.click(screen.getByRole('button', { name: 'Download the runner’s chain of runs' }))
 await waitFor(() => expect(vi.mocked(deskFetch).mock.calls.length).toBe(calls + 1))
 await waitFor(() => expect(screen.queryByRole('status') ?? screen.queryByRole('alert')).toBeTruthy())
 expect(deskFetch).toHaveBeenLastCalledWith('/api/operations/run-chain')
}

it('saves the runner’s chain of runs as run-chain.jsonl, byte for byte, and says it is unverified here', async () => {
 vi.mocked(deskFetch).mockResolvedValue(jsonl(chain))
 render(<RunChainDownload />)
 expect(screen.getByText('The file is the runner’s whole chain of runs, byte for byte as the runner sent it, unverified here.')).toBeTruthy()
 await attempt()
 expect(saved.map(s => s.file)).toEqual(['run-chain.jsonl'])
 expect(await saved[0].bytes).toEqual(chain)
 expect(screen.getByRole('status').textContent).toBe('Saved run-chain.jsonl.')
})

it('saves an empty chain as an empty file', async () => {
 vi.mocked(deskFetch).mockResolvedValue(jsonl(null))
 render(<RunChainDownload />)
 await attempt()
 expect(saved.map(s => s.file)).toEqual(['run-chain.jsonl'])
 expect(await saved[0].bytes).toEqual(new Uint8Array())
})

// Desk refuses a chain past its bound, and a transfer the runner aborted, with
// an error status; a transfer that ends early on the way to the page fails
// while it is read. Each saves nothing, and none is read as a shorter chain.
it('saves nothing when the chain is refused or its transfer ends early, and says why', async () => {
 const failures: [string, () => Response | Promise<Response>, string][] = [
  ['past Desk’s bound', () => jsonl('{"error":"The runner response exceeded its limit.","code":"bad_request"}', 502, 'application/json'), 'The runner response exceeded its limit.'],
  ['aborted by the runner', () => jsonl('{"error":"The runner\'s answer did not complete.","code":"bad_request"}', 502, 'application/json'), 'The runner\'s answer did not complete.'],
  ['refused by the runner', () => jsonl('{"error":{"code":"store_error","message":"The run store could not be read."}}', 500, 'application/json'), 'The run store could not be read.'],
  ['refused in no words', () => jsonl('not JSON', 503, 'text/plain'), 'The local runner could not complete this request.'],
  ['ended early on the way to the page', aborted, 'network error'],
  ['a request that did not complete', () => Promise.reject(new TypeError('Failed to fetch')), 'Failed to fetch']
 ]
 for (const [name, answer, said] of failures) {
  vi.mocked(deskFetch).mockImplementation(async () => answer())
  render(<RunChainDownload />)
  await attempt()
  expect(saved, name).toEqual([])
  expect(screen.queryByRole('status'), name).toBeNull()
  expect(screen.getByRole('alert').textContent, name).toBe(said)
  cleanup()
 }
})

// What a failed attempt leaves is the failure alone: the line saying what an
// earlier attempt saved goes, so it is never read as this attempt's result.
it('clears what an earlier download saved when a later attempt fails', async () => {
 vi.mocked(deskFetch).mockResolvedValue(jsonl(chain))
 render(<RunChainDownload />)
 await attempt()
 expect(screen.getByRole('status')).toBeTruthy()
 vi.mocked(deskFetch).mockImplementation(async () => aborted())
 await attempt()
 expect(saved).toHaveLength(1)
 expect(screen.queryByRole('status')).toBeNull()
 expect(screen.getByRole('alert')).toBeTruthy()
})
