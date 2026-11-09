import { afterEach, expect, it, vi } from 'vitest'
import { cleanup, render, screen } from '@testing-library/react'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { ProjectMoveSettingsChoice, PROJECT_LOCATION_KEY, type ProjectLocationStatus } from './ProjectLocation'
import { decodeStorageBudgetStatus, storageLineOff } from './storageBudgetModel'
import { storageStatusFixture } from './storageBudgetModel.fixture'
import { storageBudgetStatusKey } from './useStorageBudgetStatus'
import { DialectReadRecovery } from '../chat/ResponseRecovery'
import type { DialectReadRecoveryView } from '../assistant/serverRuns/types'

vi.mock('../files/client', async original => ({ ...await original<typeof import('../files/client')>(), deskFetch: vi.fn(async () => { throw new Error('no read in this test') }) }))
afterEach(cleanup)

const off = { version: 1, line: 'off', servedAt: '2026-11-08T12:00:00Z' }

it('decodes the storage line off as a status with nothing measured, and refuses any other shape', () => {
  const status = decodeStorageBudgetStatus(off)
  expect(storageLineOff(status)).toBe(true)
  expect(status.snapshot).toBeNull()
  expect(status.pressure.active).toBe(false)
  expect(status.registration).toBeNull()
  expect(storageLineOff(decodeStorageBudgetStatus(storageStatusFixture()))).toBe(false)
  expect(storageLineOff(decodeStorageBudgetStatus({ ...storageStatusFixture(), line: 'on' }))).toBe(false)
  expect(() => decodeStorageBudgetStatus({ ...off, line: 'maybe' })).toThrow()
  expect(() => decodeStorageBudgetStatus({ ...off, availability: 'available' })).toThrow()
  expect(() => decodeStorageBudgetStatus({ ...off, version: 2 })).toThrow()
})

function withStatus(line: 'on' | 'off') {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } })
  client.setQueryData(storageBudgetStatusKey, line === 'off' ? decodeStorageBudgetStatus(off) : decodeStorageBudgetStatus(storageStatusFixture()))
  const location: ProjectLocationStatus = { path: '/work/new', restartRequired: false, settingsConflict: { file: '/work/new/jpack.json', digest: 'a'.repeat(64), later: [], canFinish: true, finished: false } }
  client.setQueryData(PROJECT_LOCATION_KEY, location)
  return client
}

it('hides the finish-move choice while the storage line is off', () => {
  const offClient = withStatus('off')
  const view = render(<QueryClientProvider client={offClient}><ProjectMoveSettingsChoice/></QueryClientProvider>)
  expect(screen.queryByRole('button', { name: 'Finish the move with the moved settings' })).toBeNull()
  view.unmount()
  const onClient = withStatus('on')
  render(<QueryClientProvider client={onClient}><ProjectMoveSettingsChoice/></QueryClientProvider>)
  expect(screen.getByRole('button', { name: 'Finish the move with the moved settings' })).toBeTruthy()
  offClient.clear(); onClient.clear()
})

it('offers no retained-read inspection and no storage admission caption while the storage line is off', () => {
  const view: DialectReadRecoveryView = { runId: 'arun_ffffffffffffffffffffffffffffffff', runRevision: '3', recoveryRevision: '1', reads: [
    { operationId: 'found', originalOutcome: 'interrupted', backingState: 'present-valid', ordinal: '1', storageState: 'unresolved', canInspect: true },
    { operationId: 'unseen', originalOutcome: 'unknown', backingState: 'not-observed', reason: 'budget-unresolved', ordinal: '0', storageState: 'unresolved', canInspect: false },
  ] }
  const inspect = vi.fn()
  const shown = render(<DialectReadRecovery view={view} failed={false} busy={false} disabled={false} onInspect={inspect} storageLineOff/>)
  expect(screen.queryByRole('button', { name: 'Inspect retained read' })).toBeNull()
  expect(screen.queryByText(/Storage recovery admission is required/)).toBeNull()
  expect(screen.getByText('The retained snapshot was found. The earlier read outcome is unchanged.')).toBeTruthy()
  shown.unmount()
  render(<DialectReadRecovery view={view} failed={false} busy={false} disabled={false} onInspect={inspect}/>)
  expect(screen.getByRole('button', { name: 'Inspect retained read' })).toBeTruthy()
  expect(screen.getByText(/Storage recovery admission is required/)).toBeTruthy()
})
