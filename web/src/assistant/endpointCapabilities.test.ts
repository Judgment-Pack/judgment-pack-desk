import { afterEach, describe, expect, it } from 'vitest'
import * as host from '@judgment-pack/harness/host'
import { capabilitiesFor, FAMILY_CAPABILITIES } from '@judgment-pack/harness/capabilities'
import type { AssistantConfig, EndpointCapabilityRecord } from '../config/deskConfig'
import { forgetConnections, rememberConnection } from './connectionRecords'
import { capabilitiesOf, effectiveFacts } from './endpointCapabilities'

afterEach(forgetConnections)

const api = (capabilities?: EndpointCapabilityRecord, models = ['a-model']): AssistantConfig => ({
  engine: 'vercel', thinking: 'off', endpoint: { url: 'https://api.example.invalid/v1', kind: 'openai-compatible', model: models[0] ?? null, models, tools: [], ...(capabilities ? { capabilities } : {}) }
})
const PROBE = { at: '2026-10-19T12:00:00Z', nativeToolCalls: false, parallelToolCalls: false, contextWindow: 16384, reasoningField: 'reasoning_content' as const }

describe('what a connection states about its endpoint', () => {
  it('is the probe\'s finding with each override laid over it, member by member', () => {
    expect(effectiveFacts({ probe: PROBE, override: { contextWindow: 65536, vision: true } })).toEqual({
      nativeToolCalls: false, parallelToolCalls: false, contextWindow: 65536, reasoningField: 'reasoning_content', vision: true
    })
    expect(effectiveFacts({ override: { vision: true } })).toEqual({ vision: true })
    expect(effectiveFacts(undefined)).toEqual({})
  })

  it('gives the harness its own members, and nothing where nothing is stated', () => {
    expect(capabilitiesOf(api().endpoint)).toEqual({})
    expect(capabilitiesOf(api({ probe: PROBE, override: { outputReserve: 4096 } }).endpoint)).toEqual({
      contextWindowTokens: 16384, outputTokens: 4096, nativeToolCalls: false, parallelToolCalls: false,
      streamingQuirks: { reasoningField: 'reasoning_content', wholeJsonAnswer: false }
    })
  })

  it('gives a window stated alone a reserve of an eighth of it, held between the harness\'s floor and 8,192, and drops a reserve stated alone', () => {
    expect(capabilitiesOf(api({ override: { contextWindow: 32768 } }).endpoint)).toMatchObject({ contextWindowTokens: 32768, outputTokens: 4096 })
    expect(capabilitiesOf(api({ override: { contextWindow: 4096 } }).endpoint)).toMatchObject({ outputTokens: 1024 })
    expect(capabilitiesOf(api({ override: { contextWindow: 1_000_000 } }).endpoint)).toMatchObject({ outputTokens: 8192 })
    expect(capabilitiesOf(api({ override: { outputReserve: 4096 } }).endpoint)).toEqual({})
  })

  it('maps the reasoning dialect, the tool schemas and the turn order', () => {
    expect(capabilitiesOf(api({ override: { reasoningKind: 'none', reasoningIgnored: true, toolSchemas: 'gemini-subset', statePlacement: 'system-tail', strictAlternation: true } }).endpoint)).toEqual({
      reasoning: { kind: 'none', ignored: true }, toolSchemas: 'gemini-subset', statePlacement: 'system-tail', strictAlternation: true
    })
  })

  it('is a record the harness accepts, completed from the family\'s default, whatever the form can state', () => {
    const states: EndpointCapabilityRecord[] = [
      { probe: PROBE },
      { override: { contextWindow: 8192, outputReserve: 4096, vision: true, strictAlternation: true, reasoningKind: 'none' } },
      { probe: PROBE, override: { nativeToolCalls: true, wholeJsonAnswer: true } }
    ]
    for (const record of states) {
      const stated = capabilitiesOf(api(record).endpoint)
      const full = capabilitiesFor({ family: 'openai-compatible', model: 'a-model', capabilities: stated })
      // What nobody stated is the family's.
      expect(full.toolSchemas).toBe(FAMILY_CAPABILITIES['openai-compatible'].toolSchemas)
      expect(full.nativeToolCalls).toBe(stated.nativeToolCalls ?? FAMILY_CAPABILITIES['openai-compatible'].nativeToolCalls)
    }
  })
})

describe('the host\'s answer for an endpoint and model', () => {
  const target = (id: string) => ({ connectionId: id, connectionRevision: 'a'.repeat(64) })

  it('is what the one connection serving that model states', () => {
    rememberConnection(target('ai-111111111111111111111111'), api({ probe: PROBE }))
    expect(host.endpointCapabilities({ family: 'openai-compatible', model: 'a-model' })).toMatchObject({ nativeToolCalls: false, contextWindowTokens: 16384 })
  })

  it('is nothing for a model no connection serves, or a wire none speaks', () => {
    rememberConnection(target('ai-111111111111111111111111'), api({ probe: PROBE }))
    expect(host.endpointCapabilities({ family: 'openai-compatible', model: 'another-model' })).toEqual({})
    expect(host.endpointCapabilities({ family: 'anthropic', model: 'a-model' })).toEqual({})
  })

  it('is nothing where two connections serve the model and state different things, because it cannot tell them apart', () => {
    rememberConnection(target('ai-111111111111111111111111'), api({ probe: PROBE }))
    rememberConnection(target('ai-222222222222222222222222'), api({ override: { nativeToolCalls: true } }))
    expect(host.endpointCapabilities({ family: 'openai-compatible', model: 'a-model' })).toEqual({})
    // …and where they state the same, it is that.
    forgetConnections()
    rememberConnection(target('ai-111111111111111111111111'), api({ override: { vision: true } }))
    rememberConnection(target('ai-222222222222222222222222'), api({ override: { vision: true } }))
    expect(host.endpointCapabilities({ family: 'openai-compatible', model: 'a-model' })).toEqual({ vision: true })
  })
})
