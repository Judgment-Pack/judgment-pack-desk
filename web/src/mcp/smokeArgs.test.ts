import { describe, expect, it, vi } from 'vitest'
import { parseSmokeArgs } from '../../scripts/smokeArgs.ts'

const origin = 'http://127.0.0.1:8799'

describe('smoke CLI arguments', () => {
  it('imports without exiting or opening a connection', async () => {
    const exit = vi.spyOn(process, 'exit').mockImplementation(() => {
      throw new Error('parser import attempted to exit')
    })
    const connect = vi.fn(() => {
      throw new Error('parser import attempted to open a WebSocket')
    })
    const fetch = vi.fn(() => {
      throw new Error('parser import attempted to fetch')
    })
    vi.stubGlobal('WebSocket', class {
      constructor() {
        connect()
      }
    })
    vi.stubGlobal('fetch', fetch)
    try {
      vi.resetModules()
      const module = await import('../../scripts/smokeArgs.ts')
      expect(module.parseSmokeArgs([origin]).target).toBe(origin)
      expect(exit).not.toHaveBeenCalled()
      expect(connect).not.toHaveBeenCalled()
      expect(fetch).not.toHaveBeenCalled()
    } finally {
      vi.unstubAllGlobals()
      exit.mockRestore()
    }
  })

  it('keeps the default target, trace count, and optional modes', () => {
    expect(parseSmokeArgs([origin])).toEqual({
      target: origin,
      traceHead: 5,
      matrix: false,
      graphs: false
    })
  })

  it('accepts both standalone flags', () => {
    expect(parseSmokeArgs([origin, '--matrix', '--graphs'])).toMatchObject({
      matrix: true,
      graphs: true
    })
  })

  it('maps every value flag and accepts a trace count of zero', () => {
    expect(parseSmokeArgs([
      origin,
      '--secret', 'secret',
      '--facts', 'facts.json',
      '--evidence', 'evidence.json',
      '--pack', 'decision',
      '--trace', '0',
      '--expect-kind', 'unresolved',
      '--expect-handoff', 'ready',
      '--expect-matrix-status', 'passed',
      '--expect-graph-status', 'passed',
      '--graph-document', 'onboarding',
      '--graph-file', 'onboarding.graph.json'
    ])).toEqual({
      target: origin,
      secret: 'secret',
      facts: 'facts.json',
      evidence: 'evidence.json',
      packId: 'decision',
      traceHead: 0,
      expectKind: 'unresolved',
      expectHandoff: 'ready',
      expectMatrixStatus: 'passed',
      matrix: true,
      expectGraphStatus: 'passed',
      graphs: true,
      graphDocument: 'onboarding',
      graphFile: 'onboarding.graph.json'
    })
  })

  it.each([
    ['--expect-matrix-status', 'matrix'],
    ['--expect-graph-status', 'graphs']
  ] as const)('%s enables %s without a standalone flag', (flag, mode) => {
    expect(parseSmokeArgs([origin, flag, 'passed'])[mode]).toBe(true)
  })

  it('refuses a missing target', () => {
    expect(() => parseSmokeArgs([])).toThrow('the desk URL is required')
  })

  it.each([
    '--secret', '--facts', '--evidence', '--pack', '--trace',
    '--expect-kind', '--expect-handoff', '--expect-matrix-status',
    '--expect-graph-status', '--graph-document', '--graph-file'
  ])('refuses a missing value for %s', (flag) => {
    expect(() => parseSmokeArgs([origin, flag])).toThrow(`${flag} needs a value`)
  })

  it('refuses an unknown flag', () => {
    expect(() => parseSmokeArgs([origin, '--unknown', 'value'])).toThrow('unknown flag --unknown')
  })

  it.each(['', ' ', 'abc', '-1', '1.5', 'Infinity', '-Infinity', 'NaN', '1e999'])
    ('refuses invalid trace count %s', (value) => {
      expect(() => parseSmokeArgs([origin, '--trace', value]))
        .toThrow('--trace requires a non-negative integer')
    })
})
