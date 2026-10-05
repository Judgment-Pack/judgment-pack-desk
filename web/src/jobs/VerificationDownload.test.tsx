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
// A run's chain entry as Runner writes it, its exact line in base64, and the
// entry's checkpoint, as versions 4 and 5 carry them.
const entryLine = (sequence: unknown) => `{"entryVersion":"1","trail":"${'a'.repeat(32)}","sequence":${JSON.stringify(sequence)},"previous":"sha256:${'0'.repeat(64)}","kind":"run","run":"${runId}","auditDigest":"sha256:${'1'.repeat(64)}"}`
const chain = (entry: string, sequence = 42) => `"chain":{"entry":${JSON.stringify(entry)},"checkpoint":{"checkpointVersion":"1","recordDigest":"sha256:${'2'.repeat(64)}","sequence":${sequence},"trail":"${'a'.repeat(32)}"}}`
const v4 = bytes(`{"version":4,"releaseDigest":"sha256:00","run":{"audit":{"pack":"a\\u0026b"},"auditBytes":"eyJwYWNrIjoiYSZiIn0="},${chain(btoa(entryLine(42)))}}\n`)
const v5 = bytes(`{"version":5,"releaseDigest":"sha256:00","run":{"audit":{"pack":"a\\u0026b"},"auditBytes":"eyJwYWNrIjoiYSZiIn0=","auditSignatures":"eyJzaWduYXR1cmUiOiIwMCJ9Cg=="},${chain(btoa(entryLine(7)), 7)}}\n`)
const text = (body: Uint8Array) => new TextDecoder().decode(body)
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
 expect(deskFetch).toHaveBeenLastCalledWith(`/api/operations/runs/${runId}/verification?version=5`)
}
async function download(body: Uint8Array<ArrayBuffer> | string) {
 vi.mocked(deskFetch).mockResolvedValue(answer(body))
 render(<VerificationDownload runId={runId} />)
 await attempt()
}

it('names a version-2 answer to a version-5 request as version 2, offers no comparison, and finds no chain entry', async () => {
 await download(v2)
 expect(saved.map(s => s.file)).toEqual([`${runId}-verification-v2.json`])
 expect(await saved[0].bytes).toEqual(v2)
 expect(screen.getByRole('status').textContent).toContain(`Saved ${runId}-verification-v2.json: export version 2, which carries no exact bytes of the audit record`)
 expect(screen.getByRole('status').textContent).toContain('Runner’s chain of runs: no chain entry in this export.')
 expect(screen.getByRole('status').textContent).not.toMatch(comparison)
})

it('names a version-3 answer as version 3, saves its bytes as sent, says verify-run does not compare its digest, and finds no chain entry', async () => {
 await download(v3)
 expect(saved.map(s => s.file)).toEqual([`${runId}-verification-v3.json`])
 expect(await saved[0].bytes).toEqual(v3)
 expect(screen.getByRole('status').textContent).toContain(`Saved ${runId}-verification-v3.json: export version 3, with the audit record’s exact bytes.`)
 expect(screen.getByRole('status').textContent).toMatch(comparison)
 expect(screen.getByRole('status').textContent).toContain('Runner’s chain of runs: no chain entry in this export.')
})

// Runner answers version 4 to a request for 5 when the run's record is unsigned.
it('names a version-4 answer as version 4: the chain entry and checkpoint, unsigned, and the entry by its sequence, not checked', async () => {
 await download(v4)
 expect(saved.map(s => s.file)).toEqual([`${runId}-verification-v4.json`])
 expect(await saved[0].bytes).toEqual(v4)
 const status = screen.getByRole('status').textContent
 expect(status).toContain(`Saved ${runId}-verification-v4.json: export version 4, with the audit record’s exact bytes and the run’s chain entry and checkpoint; unsigned.`)
 expect(status).toMatch(comparison)
 expect(status).not.toContain('signatures')
 expect(status).toContain('Runner’s chain of runs: chain entry 42, not checked.')
})

it('names a version-5 answer as version 5: the chain entry and checkpoint and the record’s signatures, and the entry by its sequence, not checked', async () => {
 await download(v5)
 expect(saved.map(s => s.file)).toEqual([`${runId}-verification-v5.json`])
 expect(await saved[0].bytes).toEqual(v5)
 const status = screen.getByRole('status').textContent
 expect(status).toContain(`Saved ${runId}-verification-v5.json: export version 5, with the audit record’s exact bytes, the run’s chain entry and checkpoint, and the record’s signatures, not checked here.`)
 expect(status).toMatch(comparison)
 expect(status).not.toContain('unsigned')
 expect(status).toContain('Runner’s chain of runs: chain entry 7, not checked.')
})

// The sequence shown is the one the entry's own line names. Nothing here holds
// the entry to its checkpoint, so one that names another sequence changes nothing.
it('reads the sequence from the chain entry itself, and checks it against nothing', async () => {
 const disagreeing = bytes(text(v4).replace('"sequence":42,"trail"', '"sequence":41,"trail"'))
 expect(text(disagreeing)).toContain('"sequence":41,"trail"')
 await download(disagreeing)
 expect(saved.map(s => s.file)).toEqual([`${runId}-verification-v4.json`])
 expect(screen.getByRole('status').textContent).toContain('chain entry 42, not checked.')
})

it('saves the bytes it was sent, not text decoded from them', async () => {
 await download(notUTF8)
 expect(saved.map(s => s.file)).toEqual([`${runId}-verification-v3.json`])
 expect(await saved[0].bytes).toEqual(notUTF8)
})

it('saves nothing when the answer names no export version Desk knows', async () => {
 for (const body of ['{"version":6}', text(v5).replace('"version":5', '"version":6'), '{"version":1}', '{"version":"5"}', '{}', 'null', 'not JSON']) {
  await download(body)
  expect(saved).toEqual([])
  expect(screen.getByRole('alert').textContent).toBe('The local runner could not complete this request.')
  cleanup()
 }
})

// A version-4 or version-5 answer is named for the chain entry it carries, so
// one whose entry cannot be read is not saved under that name.
it('saves nothing when a version-4 or version-5 answer carries no chain entry it can read', async () => {
 for (const body of [
  '{"version":4}',
  '{"version":5,"run":{}}',
  text(v4).replace(/"entry":"[^"]*"/, '"entry":null'),
  text(v4).replace(/"entry":"[^"]*"/, '"entry":"not base64!"'),
  text(v5).replace(/"entry":"[^"]*"/, `"entry":"${btoa('not JSON')}"`),
  text(v4).replace(/"entry":"[^"]*"/, `"entry":"${btoa(entryLine(0))}"`),
  text(v4).replace(/"entry":"[^"]*"/, `"entry":"${btoa(entryLine('42'))}"`),
  text(v4).replace(/"entry":"[^"]*"/, `"entry":"${btoa(entryLine(1.5))}"`),
  text(v5).replace(/"entry":"[^"]*"/, `"entry":"${btoa(entryLine(2 ** 53))}"`),
  text(v5).replace(/"entry":"[^"]*"/, `"entry":"${btoa(String.fromCharCode(0xff, 0xfe))}"`)
 ]) {
  await download(body)
  expect(saved, body).toEqual([])
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
