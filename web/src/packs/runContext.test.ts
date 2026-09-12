import { describe, expect, it } from 'vitest'
import { QueryClient } from '@tanstack/react-query'
import { publishPackRun, traceMatches, type PackRunSnapshot } from './runContext'
import type { EvaluationRun } from '../mcp/types'

const snapshot: PackRunSnapshot = { id: 'run-1', packId: 'expense', packBytes: '{"version":"1"}\n', run: { facts: '{"amount":"20"}' } as EvaluationRun }
describe('trace revision binding', () => {
  it('requires the same pack and exact submitted bytes, including whitespace', () => {
    expect(traceMatches(snapshot, 'expense', snapshot.packBytes)).toBe(true)
    expect(traceMatches(snapshot, 'other', snapshot.packBytes)).toBe(false)
    expect(traceMatches(snapshot, 'expense', '{"version":"1"}')).toBe(false)
    expect(traceMatches(snapshot, 'expense', '{"version":"2"}\n')).toBe(false)
    expect(traceMatches({ ...snapshot, packBytes: undefined }, 'expense', undefined)).toBe(false)
    expect(traceMatches(undefined, 'expense', snapshot.packBytes)).toBe(false)
  })
  it('replaces the explicitly selected explanation within its query-client session', () => {
    const client = new QueryClient()
    publishPackRun(client, snapshot)
    publishPackRun(client, { ...snapshot, id: 'run-2' })
    expect(client.getQueryData<PackRunSnapshot>(['pack-explanation', 'expense'])?.id).toBe('run-2')
    expect(new QueryClient().getQueryData(['pack-explanation', 'expense'])).toBeUndefined()
    client.clear()
  })
})
