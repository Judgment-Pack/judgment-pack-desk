import { cleanup, fireEvent, render, screen } from '@testing-library/react'
import { afterEach, expect, it } from 'vitest'
import { ReleaseReadiness } from './ReleaseReadiness'
import type { Release } from './client'
afterEach(cleanup)
const report = { status: 'passed', summary: { total: 1, passed: 1, mismatched: 0 }, packs: [{ id: 'target', status: 'passed', summary: { total: 1, passed: 1, mismatched: 0 }, rows: [], coverage: [{ probe: 'reason:unknown', status: 'missing', detail: 'No expected unknown result.' }] }] }
const release: Release = { id: 'release', title: 'Intake', packId: 'intake', packVersion: '1', packDigest: 'pack-hash', runtimeDigest: 'runtime-hash', createdAt: '2026-09-25T12:00:00Z', pack: '{}', sample: { facts: {} }, preview: { disposition: { kind: 'outcome', outcomeId: 'accept', reasons: [], handoff: { state: 'none' } } }, tests: 'passed', testEvidence: { status: 'passed', matrix: '{}', matrixDigest: 'hash', packDigest: 'pack-hash', runtimeDigest: 'runtime-hash', checkedAt: '2026-09-25T12:00:00Z', report } }
it('keeps test outcomes and advisory coverage separate, with details collapsed', () => {
 render(<ReleaseReadiness release={release} />)
 expect(screen.getByText('Passed')).toBeTruthy()
 expect(screen.getByText('Saved cases: 1 · 1 passed · 0 failed')).toBeTruthy()
 const disclosure = screen.getByText('Coverage gaps: 1 · advisory').closest('details')!
 expect(disclosure.open).toBe(false)
 fireEvent.click(screen.getByText('Coverage gaps: 1 · advisory'))
 expect(disclosure.open).toBe(true)
 expect(screen.getByText('No expected unknown result.')).toBeTruthy()
})
it('shows incomplete checks without publishing partial counts as a completed result', () => {
 render(<ReleaseReadiness release={{ ...release, tests: 'error', testEvidence: { ...release.testEvidence!, status: 'error', problem: 'The check did not complete.' } }} />)
 expect(screen.getByText('Check incomplete')).toBeTruthy()
 expect(screen.queryByText('Saved cases: 1 · 1 passed · 0 failed')).toBeNull()
 expect(screen.getByText('Runtime report')).toBeTruthy()
})
it('keeps old releases explicitly untested', () => {
 render(<ReleaseReadiness release={{ ...release, tests: 'not-run', testEvidence: undefined }} />)
 expect(screen.getByText('Not run')).toBeTruthy()
 expect(screen.queryByText('Passed')).toBeNull()
 expect(screen.queryByText('Test results')).toBeNull()
})
