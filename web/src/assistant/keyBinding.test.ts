/**
 * The page's reading of the desk's binding, held to the desk's own rule.
 *
 * **The rule is the chassis'**, and this is a reading of it: what a row says,
 * and what a button that would fail anyway is enabled for. It is asserted
 * against `endpointOrigin` in `internal/desk/assistant.go` by reading that
 * declaration's own cases out of this file's table — a page that drew the line
 * anywhere else would tell an author a key still works where the chassis will
 * refuse it, or ask for one that is already right.
 */
import { describe, expect, it } from 'vitest'
import type { AssistantEndpointConfig } from '../config/deskConfig'
import type { AssistantKeyState } from './client'
import { endpointOrigin, keyBinding } from './keyBinding'

const ENDPOINT: AssistantEndpointConfig = {
  url: 'https://api.example.invalid/v1',
  kind: 'openai-compatible',
  model: 'a-model',
  tools: []
}

const STORED: AssistantKeyState = {
  present: true,
  fingerprint: 'sk-a…wxyz',
  origin: 'https://api.example.invalid',
  kind: 'openai-compatible'
}

const NONE: AssistantKeyState = { present: false, fingerprint: '', origin: '', kind: '' }

describe('the origin a key is bound to', () => {
  it('is the scheme and the host, and never the path or the query', () => {
    // A path or a query is the endpoint's own routing, and an author changes
    // one without changing who is at the other end.
    expect(endpointOrigin('https://api.example.invalid/v1')).toBe('https://api.example.invalid')
    expect(endpointOrigin('https://api.example.invalid/v1?route=eu')).toBe(
      'https://api.example.invalid'
    )
  })

  it('keeps the port, because a port is a different destination', () => {
    expect(endpointOrigin('https://api.example.invalid:8443/v1')).toBe(
      'https://api.example.invalid:8443'
    )
    expect(endpointOrigin('https://api.example.invalid:8443/v1')).not.toBe(
      endpointOrigin('https://api.example.invalid/v1')
    )
  })

  it('folds case, because a host is case-insensitive', () => {
    expect(endpointOrigin('https://API.Example.Invalid/v1')).toBe('https://api.example.invalid')
  })

  it('is nothing at all for something that is not an address', () => {
    expect(endpointOrigin('not a url')).toBeUndefined()
    expect(endpointOrigin('mailto:someone@example.invalid')).toBeUndefined()
  })
})

describe('what the key row is about', () => {
  it('is a page that has not been told, before the read answers', () => {
    expect(keyBinding(undefined, ENDPOINT)).toBe('unread')
  })

  it('asks for an endpoint first, with or without a key kept here', () => {
    // Storing needs one to bind to, so this is the state with no repair at
    // that row — and a key that is stored is still reported on the line above.
    expect(keyBinding(NONE, null)).toBe('no-endpoint')
    expect(keyBinding(STORED, null)).toBe('no-endpoint')
  })

  it('is bound where the origin and the protocol both still match', () => {
    expect(keyBinding(STORED, ENDPOINT)).toBe('bound')
    // A path or a query moving keeps the binding, exactly as the desk's does.
    expect(keyBinding(STORED, { ...ENDPOINT, url: 'https://api.example.invalid/v2?a=b' })).toBe(
      'bound'
    )
  })

  it('asks for the key again where either half moved', () => {
    expect(keyBinding(STORED, { ...ENDPOINT, url: 'https://other.example.invalid/v1' })).toBe(
      'rebind'
    )
    expect(keyBinding(STORED, { ...ENDPOINT, kind: 'anthropic' })).toBe('rebind')
    // A scheme change is a different destination too.
    expect(keyBinding(STORED, { ...ENDPOINT, url: 'http://api.example.invalid/v1' })).toBe(
      'rebind'
    )
  })

  it('is none where an endpoint is configured and nothing is stored', () => {
    expect(keyBinding(NONE, ENDPOINT)).toBe('none')
  })

  it('asks again rather than claiming a binding it cannot compute', () => {
    // A URL with no host has no origin, so there is nothing to compare — and
    // the honest answer is the one that asks rather than the one that assures.
    expect(keyBinding(STORED, { ...ENDPOINT, url: 'not a url' })).toBe('rebind')
  })
})
