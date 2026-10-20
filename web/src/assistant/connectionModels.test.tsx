import { QueryClientProvider } from '@tanstack/react-query'
import { cleanup, renderHook, waitFor } from '@testing-library/react'
import type { ReactNode } from 'react'
import { afterEach, describe, expect, it, vi } from 'vitest'
import * as host from '@judgment-pack/harness/host'
import { DeskConfigFixture } from '../config/DeskConfigProvider'
import { effectiveConfig, type AssistantConfig } from '../config/deskConfig'
import { testQueryClient } from '../testing/harness'
import type { AIRegistry } from './aiConnections'
import { forgetConnections, rememberConnection } from './connectionRecords'
import { modelsFor } from './connectionModels'
import { useAssistantSlot } from './useAssistantSlot'

afterEach(() => {
  cleanup()
  forgetConnections()
  vi.unstubAllGlobals()
})

const API = (more: Record<string, unknown> = {}, models = ['a-model', 'a-cheaper-model']): AssistantConfig => ({
  engine: 'vercel', thinking: 'off', endpoint: { url: 'https://api.example.invalid/v1', kind: 'openai-compatible', model: 'a-model', models, tools: [], ...more }
})
const CODEX: AssistantConfig = {
  engine: 'codex', thinking: 'off', endpoint: null,
  agent: { provider: 'openai', authMethod: 'subscription', model: 'model', models: ['model', 'small'], tools: [], childModel: 'small' }
}
const target = (id: string, revision = 'a'.repeat(64)) => ({ connectionId: id, connectionRevision: revision, connectionName: id })
const API_ID = 'ai-111111111111111111111111', OTHER_ID = 'ai-222222222222222222222222', CODEX_ID = 'ai-333333333333333333333333'
const ask = (connection: ReturnType<typeof target> | undefined, engine: 'vercel' | 'codex' = 'vercel') =>
  modelsFor({ ...(connection ? { connection } : {}), engine, ...(engine === 'vercel' ? { family: 'openai-compatible' as const } : {}), model: 'a-model' })

describe('what a connection names for its review and its helper tasks', () => {
  it('answers each connection from its own settings, not from the model name', () => {
    rememberConnection(target(API_ID), API({ criticModel: 'a-cheaper-model', childModel: 'a-cheaper-model' }))
    rememberConnection(target(OTHER_ID), API({ childModel: 'a-model' }))
    // The same model name, asked through two connections.
    expect(ask(target(API_ID))).toEqual({ critic: 'a-cheaper-model', child: 'a-cheaper-model' })
    expect(ask(target(OTHER_ID))).toEqual({ child: 'a-model' })
  })

  it('answers the capacity a connection states as the requests it carries at once, and none where it states none', () => {
    rememberConnection(target(API_ID), API({ capacity: 6 }))
    rememberConnection(target(OTHER_ID), API())
    expect(ask(target(API_ID))).toEqual({ concurrentRequests: 6 })
    expect(ask(target(OTHER_ID))).toEqual({})
    // The harness holds a connection's capacity to 1 to 16, and refuses another answer.
    expect(host.runModels({ connection: target(API_ID), engine: 'vercel', family: 'openai-compatible', model: 'a-model' })).toMatchObject({ concurrentRequests: 6 })
  })

  it('states no capacity for a subscription, which runs one child at a time', () => {
    rememberConnection(target(CODEX_ID), { ...CODEX, endpoint: { url: 'https://api.example.invalid/v1', kind: 'openai-compatible', model: null, models: [], tools: [], capacity: 8 } })
    expect(ask(target(CODEX_ID), 'codex')).toEqual({ child: 'small' })
  })

  it('answers nothing where the connection names nothing, or the Desk has no record of it', () => {
    rememberConnection(target(API_ID), API())
    expect(ask(target(API_ID))).toEqual({})
    expect(ask(target(OTHER_ID))).toEqual({})
    expect(ask(undefined)).toEqual({})
  })

  it('answers nothing for a connection edited since the run\'s slot resolved it', () => {
    rememberConnection(target(API_ID, 'a'.repeat(64)), API({ criticModel: 'a-cheaper-model' }))
    expect(ask(target(API_ID, 'b'.repeat(64)))).toEqual({})
  })

  it('names no model the desk\'s own choice of models has left out, because the relay would refuse it', () => {
    rememberConnection(target(API_ID), API({ criticModel: 'a-cheaper-model', childModel: 'a-cheaper-model' }, ['a-model']))
    expect(ask(target(API_ID))).toEqual({})
  })

  it('reads a subscription connection\'s agent settings for a Codex run, and the endpoint\'s for an API run', () => {
    rememberConnection(target(CODEX_ID), CODEX)
    expect(ask(target(CODEX_ID), 'codex')).toEqual({ child: 'small' })
    rememberConnection(target(API_ID), API({ criticModel: 'a-cheaper-model' }))
    expect(ask(target(API_ID), 'codex')).toEqual({})
  })

  it('is the Desk\'s answer to the harness, once the host is wired', () => {
    rememberConnection(target(API_ID), API({ criticModel: 'a-cheaper-model' }))
    expect(host.runModels({ connection: target(API_ID), engine: 'vercel', family: 'openai-compatible', model: 'a-model' })).toEqual({ critic: 'a-cheaper-model', child: 'a-model' })
    expect(host.runModels({ engine: 'vercel', family: 'openai-compatible', model: 'a-model' })).toEqual({ critic: 'a-model', child: 'a-model' })
  })
})

describe('the slot that resolves a connection records it for the runs bound to it', () => {
  it('records the connection\'s settings under its id and revision as the slot reads them', async () => {
    const connection = { id: API_ID, name: 'Open weights', revision: 'c'.repeat(64), enabled: true, assistant: API({ criticModel: 'a-cheaper-model' }) }
    const registry: AIRegistry = { version: 1, defaultConnection: API_ID, connections: [connection], path: 'registry', sha256: 'd'.repeat(64) }
    const effective = { ...effectiveConfig(undefined), aiConnections: { loading: false, data: registry } }
    vi.stubGlobal('fetch', async () => Response.json({ present: false }))
    const wrapper = ({ children }: { children: ReactNode }) => (
      <QueryClientProvider client={testQueryClient()}><DeskConfigFixture value={effective}>{children}</DeskConfigFixture></QueryClientProvider>
    )
    renderHook(() => useAssistantSlot(API_ID), { wrapper })
    await waitFor(() => expect(ask(target(API_ID, 'c'.repeat(64)))).toEqual({ critic: 'a-cheaper-model' }))
    expect(ask(target(API_ID, 'e'.repeat(64)))).toEqual({})
  })
})
