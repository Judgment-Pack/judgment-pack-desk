import { cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react'
import { afterEach, beforeEach, expect, it, vi } from 'vitest'
import { deskFetch } from '../files/client'
import { VerificationDownload } from './JobsView'
vi.mock('../files/client', async original => ({ ...await original<typeof import('../files/client')>(), deskFetch: vi.fn() }))

const runId = 'run_' + '0'.repeat(32)
// As Runner encodes an export: & escaped, and a newline at the end.
const v2 = '{"version":2,"releaseDigest":"sha256:00","run":{"audit":{"pack":"a\\u0026b"}}}\n'
const v3 = '{"version":3,"releaseDigest":"sha256:00","run":{"audit":{"pack":"a\\u0026b"},"auditBytes":"eyJwYWNrIjoiYSZiIn0="}}\n'
const comparison = /can be compared with a gateway receipt’s decision\.recordDigest; verify-run does not make that comparison/
let saved: { file: string, bytes: Promise<string> }[]
let objects: Blob[]
const createObjectURL = URL.createObjectURL, revokeObjectURL = URL.revokeObjectURL
beforeEach(() => {
 saved = []; objects = []
 URL.createObjectURL = (blob: Blob) => { objects.push(blob); return `blob:${objects.length - 1}` }
 URL.revokeObjectURL = () => {}
 vi.spyOn(HTMLAnchorElement.prototype, 'click').mockImplementation(function (this: HTMLAnchorElement) {
  saved.push({ file: this.download, bytes: objects[Number(this.href.slice('blob:'.length))].text() })
 })
})
afterEach(() => { cleanup(); vi.restoreAllMocks(); vi.mocked(deskFetch).mockReset(); URL.createObjectURL = createObjectURL; URL.revokeObjectURL = revokeObjectURL })

async function download(answer: string) {
 vi.mocked(deskFetch).mockResolvedValue(new Response(answer, { headers: { 'Content-Type': 'application/json' } }))
 render(<VerificationDownload runId={runId} />)
 fireEvent.click(screen.getByRole('button', { name: 'Download verification record' }))
 await waitFor(() => expect(screen.queryByRole('status') ?? screen.queryByRole('alert')).toBeTruthy())
 expect(deskFetch).toHaveBeenCalledWith(`/api/operations/runs/${runId}/verification?version=3`)
}

it('names a version-2 answer to a version-3 request as version 2, and offers no comparison', async () => {
 await download(v2)
 expect(saved.map(s => s.file)).toEqual([`${runId}-verification-v2.json`])
 expect(await saved[0].bytes).toBe(v2)
 expect(screen.getByRole('status').textContent).toContain(`Saved ${runId}-verification-v2.json: export version 2`)
 expect(screen.queryByText(comparison)).toBeNull()
})

it('names a version-3 answer as version 3, saves its bytes as sent, and says verify-run does not compare its digest', async () => {
 await download(v3)
 expect(saved.map(s => s.file)).toEqual([`${runId}-verification-v3.json`])
 expect(await saved[0].bytes).toBe(v3)
 expect(screen.getByRole('status').textContent).toMatch(comparison)
})

it('saves nothing when the answer names no export version Desk knows', async () => {
 for (const answer of ['{"version":4}', '{"version":"3"}', '{}', 'null', 'not JSON']) {
  await download(answer)
  expect(saved).toEqual([])
  expect(screen.getByRole('alert').textContent).toBe('The local runner could not complete this request.')
  cleanup()
 }
})
