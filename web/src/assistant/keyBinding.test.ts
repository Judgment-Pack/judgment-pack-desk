/**
 * The row's five states, mapped from the desk's own answer.
 *
 * **There is nothing to test about a comparison, because there is no longer
 * one.** This module used to compute the binding with the browser's `URL`,
 * which drops an explicit `:443` where Go's `url.Parse` keeps it — so a key
 * stored for a host and a configuration naming the same host with its default
 * port written out read as bound here while the relay sent nothing. The
 * verdict is the chassis' now (`bound` on `GET /api/assistant/key`, exercised
 * over both spellings in `TestKeyReadCarriesThisDesksOwnBindingVerdict`), and
 * what is left here is the mapping to what a row says.
 */
import { readFileSync } from 'node:fs'
import { join } from 'node:path'
import { describe, expect, it } from 'vitest'
import type { AssistantEndpointConfig } from '../config/deskConfig'
import type { AssistantKeyState } from './client'
import { keyBinding } from './keyBinding'

const ENDPOINT: AssistantEndpointConfig = {
  url: 'https://api.example.invalid/v1',
  kind: 'openai-compatible',
  model: 'a-model',
  tools: []
}

const BOUND: AssistantKeyState = {
  present: true,
  fingerprint: 'sk-a…wxyz',
  origin: 'https://api.example.invalid',
  kind: 'openai-compatible',
  configuredOrigin: 'https://api.example.invalid',
  bound: true
}

const NONE: AssistantKeyState = {
  present: false,
  fingerprint: '',
  origin: '',
  kind: '',
  configuredOrigin: 'https://api.example.invalid',
  bound: false
}

describe('what the key row is about', () => {
  it('is a page that has not been told, before the read answers', () => {
    expect(keyBinding(undefined, ENDPOINT)).toBe('unread')
  })

  it('asks for an endpoint first, with or without a key kept here', () => {
    // Storing needs one to bind to, so this is the state with no repair at
    // that row — and a key that is stored is still reported on the line above.
    expect(keyBinding({ ...NONE, configuredOrigin: '' }, null)).toBe('no-endpoint')
    expect(keyBinding({ ...BOUND, configuredOrigin: '', bound: false }, null)).toBe('no-endpoint')
  })

  it('is none where an endpoint is configured and nothing is stored', () => {
    expect(keyBinding(NONE, ENDPOINT)).toBe('none')
  })

  it('takes the verdict from the desk and computes none of its own', () => {
    // **Both halves, over the same page-side inputs.** The endpoint and the
    // stored origin are identical in these two cases and only the desk's
    // answer differs — which is the whole of what this module may read.
    expect(keyBinding(BOUND, ENDPOINT)).toBe('bound')
    expect(keyBinding({ ...BOUND, bound: false }, ENDPOINT)).toBe('rebind')
  })

  it('says rebind for a mismatch this page could never have seen', () => {
    // The measured case: the browser folds an explicit default port away and
    // Go does not. The origins here look equal to any comparison this page
    // could write, and the desk says they are not.
    expect(
      keyBinding(
        {
          ...BOUND,
          origin: 'https://api.example.invalid',
          configuredOrigin: 'https://api.example.invalid:443',
          bound: false
        },
        { ...ENDPOINT, url: 'https://api.example.invalid:443/v1' }
      )
    ).toBe('rebind')
  })
})

describe('the module itself', () => {
  it('computes no origin and compares no host', () => {
    // A guard over the source, in the enforcement idiom: the defect was a
    // second implementation of the desk's rule, and the repair is that there
    // is no implementation here at all.
    const text = readFileSync(join(import.meta.dirname, 'keyBinding.ts'), 'utf8')
    // Read off the code rather than the prose: the module comment says `URL`
    // and `url.Parse` on purpose, because the reason there is nothing here is
    // worth writing down.
    const code = text
      .split('\n')
      .filter((line) => {
        const trimmed = line.trim()
        return !trimmed.startsWith('*') && !trimmed.startsWith('//') && !trimmed.startsWith('/*')
      })
      .join('\n')
    for (const shape of ['new URL(', '.host', '.protocol', 'toLowerCase']) {
      expect(code, `keyBinding.ts carries ${shape}`).not.toContain(shape)
    }
  })
})
