import { cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react'
import { afterEach, beforeEach, expect, it, vi } from 'vitest'
import { deskFetch } from '../files/client'
import { VerificationDownload } from './JobsView'
vi.mock('../files/client', async original => ({ ...await original<typeof import('../files/client')>(), deskFetch: vi.fn() }))

const runId = 'run_' + '0'.repeat(32)
const bytes = (...parts: (string | number[])[]) => new Uint8Array(parts.flatMap(part => typeof part === 'string' ? [...new TextEncoder().encode(part)] : part))
// As Runner encodes an export: & escaped, and a newline at the end.
const v2 = bytes('{"version":2,"releaseDigest":"sha256:00","run":{"audit":{"pack":"a\\u0026b"}}}\n')
const v3 = bytes('{"version":3,"releaseDigest":"sha256:00","run":{"audit":{"pack":"a\\u0026b"},"auditBytes":"eyJwYWNrIjoiYSZiIn0="}}\n')
// Bytes that are not UTF-8, inside a string of an export that still parses: a
// reader that decodes the answer to text and saves that text would save
// U+FFFD in their place.
const notUTF8 = bytes('{"version":3,"releaseDigest":"sha256:00","run":{"note":"', [0xff, 0xfe, 0xc3], '"}}\n')
const comparison = /can be compared with a gateway receipt’s decision\.recordDigest; verify-run does not make that comparison/
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

const answer = (body: Uint8Array<ArrayBuffer> | string, status = 200) => new Response(body, { status, headers: { 'Content-Type': 'application/json' } })
async function attempt() {
 const calls = vi.mocked(deskFetch).mock.calls.length
 fireEvent.click(screen.getByRole('button', { name: 'Download verification record' }))
 await waitFor(() => expect(vi.mocked(deskFetch).mock.calls.length).toBe(calls + 1))
 await waitFor(() => expect(screen.queryByRole('status') ?? screen.queryByRole('alert')).toBeTruthy())
 expect(deskFetch).toHaveBeenLastCalledWith(`/api/operations/runs/${runId}/verification?version=3`)
}
async function download(body: Uint8Array<ArrayBuffer> | string) {
 vi.mocked(deskFetch).mockResolvedValue(answer(body))
 render(<VerificationDownload runId={runId} />)
 await attempt()
}

it('names a version-2 answer to a version-3 request as version 2, and offers no comparison', async () => {
 await download(v2)
 expect(saved.map(s => s.file)).toEqual([`${runId}-verification-v2.json`])
 expect(await saved[0].bytes).toEqual(v2)
 expect(screen.getByRole('status').textContent).toContain(`Saved ${runId}-verification-v2.json: export version 2`)
 expect(screen.queryByText(comparison)).toBeNull()
})

it('names a version-3 answer as version 3, saves its bytes as sent, and says verify-run does not compare its digest', async () => {
 await download(v3)
 expect(saved.map(s => s.file)).toEqual([`${runId}-verification-v3.json`])
 expect(await saved[0].bytes).toEqual(v3)
 expect(screen.getByRole('status').textContent).toMatch(comparison)
})

it('saves the bytes it was sent, not text decoded from them', async () => {
 await download(notUTF8)
 expect(saved.map(s => s.file)).toEqual([`${runId}-verification-v3.json`])
 expect(await saved[0].bytes).toEqual(notUTF8)
})

it('saves nothing when the answer names no export version Desk knows', async () => {
 for (const body of ['{"version":4}', '{"version":"3"}', '{}', 'null', 'not JSON']) {
  await download(body)
  expect(saved).toEqual([])
  expect(screen.getByRole('alert').textContent).toBe('The local runner could not complete this request.')
  cleanup()
 }
})

// What a failed attempt leaves is the failure alone: the line saying what an
// earlier attempt saved goes, so it is never read as this attempt's result.
it('clears what an earlier download saved when a later attempt fails', async () => {
 await download(v3)
 expect(saved).toHaveLength(1)
 expect(screen.getByRole('status')).toBeTruthy()
 const failures: [string, () => void][] = [
  ['an answer of no known version', () => vi.mocked(deskFetch).mockResolvedValue(answer('not JSON'))],
  ['an HTTP failure', () => vi.mocked(deskFetch).mockResolvedValue(answer('{"error":{"code":"store_error"}}', 500))],
  ['a request that did not complete', () => vi.mocked(deskFetch).mockRejectedValue(new TypeError('Failed to fetch'))]
 ]
 for (const [name, arrange] of failures) {
  arrange()
  await attempt()
  expect(saved, name).toHaveLength(1)
  expect(screen.queryByRole('status'), name).toBeNull()
  expect(screen.getByRole('alert'), name).toBeTruthy()
  // A success between failures, so each failure follows a saved line.
  vi.mocked(deskFetch).mockResolvedValue(answer(v2))
  await attempt()
  expect(screen.getByRole('status').textContent, name).toContain('export version 2')
  saved.pop()
 }
})
