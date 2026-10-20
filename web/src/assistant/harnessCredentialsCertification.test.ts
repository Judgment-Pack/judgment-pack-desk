import { afterEach, expect, it, vi } from 'vitest'

// Exercise configureHarness itself with a selected Desk and a real session.
// The certification runner always runs this row before either adapter's suite.
afterEach(() => {
  sessionStorage.clear()
  vi.resetModules()
})

it('certifies the Desk configureHarness credentials', async () => {
  vi.resetModules()
  const desk = '1234567890abcdef1234567890abcdef'
  const bearer = 'desk-certification-session'
  const connection = 'ai-111111111111111111111111'
  const revision = 'a'.repeat(64)
  sessionStorage.setItem('jpack.active-desk', desk)
  const session = await import('../mcp/session')
  session.giveThisPageASessionForTesting(bearer)
  await import('./harnessHost')
  const host = await import('@judgment-pack/harness/host')
  expect(host.deskHeaders()).toEqual({ 'X-Jpack-Desk': desk })
  expect(host.aiHeaders(connection, revision)).toEqual({
    'X-Assistant-Connection': connection,
    'X-Assistant-Revision': revision
  })
  expect(host.aiHeaders()).toEqual({})
  expect(await host.sessionBearer()).toBe(bearer)
  expect(host.chassisUrl('/api/certification', { query: 'a b' })).toContain('query=a+b')
})
