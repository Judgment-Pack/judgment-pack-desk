/**
 * The run page's fields (#213): when the run started, who initiated it, the
 * release's digests, and whether the record's exact bytes and a signature
 * sidecar are held, from records shaped as Runner v0.5.0 serves them.
 */
import { createHash } from 'node:crypto'
import { cleanup, configure, render, screen, waitFor } from '@testing-library/react'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { Tooltip } from 'radix-ui'
import { MemoryRouter, Route, Routes } from 'react-router-dom'
import { afterEach, beforeEach, expect, it, vi } from 'vitest'
import { JobsContent } from './JobsView'
import { jobsAPI, type Run } from './client'
import { when } from './RunRecord'
import { JOB, RECORD_LINE, release, runner, runs } from './__fixtures__/activity'

vi.mock('./client', async original => ({ ...await original<typeof import('./client')>(), jobsAPI: vi.fn() }))
// Each test stands the whole job page up and reads several of Runner's answers;
// on a loaded host that takes longer than testing-library's one second.
configure({ asyncUtilTimeout: 5000 })
beforeEach(() => { vi.mocked(jobsAPI).mockImplementation(runner() as never) })
afterEach(() => { cleanup(); vi.resetAllMocks() })

async function open(run: Run) {
  render(<QueryClientProvider client={new QueryClient({ defaultOptions: { queries: { retry: false } } })}><Tooltip.Provider><MemoryRouter initialEntries={[`/jobs/${JOB}/runs/${run.id}`]}><Routes><Route path="/jobs/:jobId/runs/:runId" element={<JobsContent />} /></Routes></MemoryRouter></Tooltip.Provider></QueryClientProvider>)
  await screen.findByText('Execution')
}
const fact = (term: string) => screen.getByText(term, { selector: 'dt' }).nextElementSibling!.textContent

it('shows when the run started, that this installation requested it, the release’s digests, and that its record bytes and sidecar are held', async () => {
  await open(runs.signed)
  expect(fact('Submitted')).toBe(when(runs.signed.createdAt))
  expect(fact('Started')).toBe(when(runs.signed.startedAt!))
  expect(fact('Finished')).toBe(when(runs.signed.finishedAt!))
  expect(fact('Requested by')).toBe('This installation')
  await waitFor(() => expect(fact('Pack digest')).toBe(release.packDigest))
  expect(fact('Runtime digest')).toBe(release.runtimeDigest)
  expect(fact('Release')).toBe('Fixed version 1.2.0')
  expect(fact('Exact record bytes')).toBe('Present in the record')
  expect(fact('Signature sidecar')).toBe('Present in the record')
  await waitFor(() => expect(fact('Exact record bytes: SHA-256')).toBe('sha256:' + createHash('sha256').update(RECORD_LINE).digest('hex')))
  expect(fact('Requester as recorded')).toBe(runs.signed.requestedBy)
})

it('says a record holds no sidecar where Runner returned none', async () => {
  await open(runs.unsigned)
  expect(fact('Exact record bytes')).toBe('Present in the record')
  expect(fact('Signature sidecar')).toBe('Absent from the record')
  expect(fact('Signature sidecar: SHA-256')).toBe('Absent from the record')
})

it('words a trigger’s run by the trigger’s name and the revision that fired', async () => {
  await open(runs.running)
  await waitFor(() => expect(fact('Requested by')).toBe('Nightly intake, trigger revision 2'))
  expect(fact('Started')).toBe(when(runs.running.startedAt!))
  expect(fact('Finished')).toBe('—')
  expect(fact('Exact record bytes')).toBe('Absent from the record')
})

it('labels an interrupted run’s finish as the time the runner recorded the interruption', async () => {
  await open(runs.interrupted)
  expect(fact('Interruption recorded by the runner')).toBe(when(runs.interrupted.finishedAt!))
  expect(screen.queryByText('Finished', { selector: 'dt' })).toBeNull()
})

it('says when Runner kept no record of who asked', async () => {
  await open(runs.legacy)
  expect(fact('Requested by')).toBe('Not recorded')
  expect(fact('Requester as recorded')).toBe('Not recorded')
})
