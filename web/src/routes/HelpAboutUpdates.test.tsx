/**
 * Help & About hands the Updates section the identities Desk recorded at
 * startup, and a Gateway identity only while the local Gateway is running —
 * the same rule as the connection line, so a stopped or external Gateway is
 * never compared with the lock as if it were the one in use.
 */
import { QueryClientProvider } from '@tanstack/react-query'
import { cleanup, render } from '@testing-library/react'
import { MemoryRouter } from 'react-router-dom'
import { afterEach, expect, it, vi } from 'vitest'
import { DeskConfigFixture } from '../config/DeskConfigProvider'
import { effectiveConfig, type ComponentBuilds, type LocalGatewayStatus } from '../config/deskConfig'
import { McpContext } from '../mcp/McpProvider'
import { connected, stubClient, testQueryClient } from '../testing/harness'
import { HelpAbout } from './HelpAbout'

const seen = vi.hoisted(() => ({ props: [] as unknown[] }))
vi.mock('../updates/Updates', () => ({ Updates: (props: unknown) => { seen.props.push(props); return null } }))
afterEach(() => { cleanup(); seen.props.length = 0 })

const builds: ComponentBuilds = { desk: {}, runtime: { revision: 'a'.repeat(40) }, runner: { revision: 'b'.repeat(40) }, sourceWorker: { revision: 'b'.repeat(40) } }
const build = { version: 'v0.8.0', revision: 'c'.repeat(40) }

function show(localGateway: LocalGatewayStatus) {
  const stub = stubClient({ list_packs: () => ({ text: JSON.stringify({ packs: [] }) }) })
  render(
    <QueryClientProvider client={testQueryClient()}>
      <MemoryRouter>
        <McpContext.Provider value={connected({ client: stub.client })}>
          <DeskConfigFixture value={effectiveConfig(undefined, undefined, undefined, {
            path: '/config/desk.json', present: false, localGateway,
            chassis: { projectDir: '/p', projectFile: '/p/jpack-desk.json', runtimeBin: '/bin/jpack', builds }
          })}>
            <HelpAbout />
          </DeskConfigFixture>
        </McpContext.Provider>
      </MemoryRouter>
    </QueryClientProvider>
  )
  return seen.props.at(-1) as { builds?: ComponentBuilds; gateway?: unknown }
}

it('passes the startup builds and the running Gateway identity', () => {
  const props = show({ status: 'ready', build })
  expect(props.builds).toEqual(builds)
  expect(props.gateway).toEqual(build)
})

it.each(['external', 'unavailable'] as const)('passes no Gateway identity when the local Gateway is %s', status => {
  expect(show({ status, build }).gateway).toBeUndefined()
})
