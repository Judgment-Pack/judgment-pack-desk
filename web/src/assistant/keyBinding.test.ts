/**
 * The row's five states, mapped from the desk's own answer and nothing else.
 *
 * **There is nothing to test about a comparison, because there is no longer
 * one — and nothing about the page's configuration, because the row no longer
 * reads it.** This module used to compute the binding with the browser's
 * `URL`, which drops an explicit `:443` where Go's `url.Parse` keeps it; then
 * it consulted the page's copy of the configuration first, which is exactly
 * the thing that can be missing — a desk-level read that answered with a
 * refusal resolves to the defaults, and the row said "save an endpoint first"
 * while a good key read beside it named the endpoint and carried this desk's
 * verdict about it.
 *
 * Both verdicts are the chassis' now (`configuredOrigin`, `configuredKind`
 * and `bound` on `GET /api/assistant/key`, exercised over both spellings of a
 * default port in `TestKeyReadCarriesThisDesksOwnBindingVerdict`), and what is
 * left here is the mapping to what a row says.
 */
import { readFileSync } from 'node:fs'
import { join } from 'node:path'
import { describe, expect, it } from 'vitest'
import type { AssistantKeyState } from './client'
import { keyBinding } from './keyBinding'

const BOUND: AssistantKeyState = {
  present: true,
  fingerprint: 'sk-a…wxyz',
  origin: 'https://api.example.invalid',
  kind: 'openai-compatible',
  configuredOrigin: 'https://api.example.invalid',
  configuredKind: 'openai-compatible',
  bound: true
}

const NONE: AssistantKeyState = {
  present: false,
  fingerprint: '',
  origin: '',
  kind: '',
  configuredOrigin: 'https://api.example.invalid',
  configuredKind: 'openai-compatible',
  bound: false
}

/** What the desk answers where it is configured for nowhere. */
const NOWHERE: AssistantKeyState = { ...NONE, configuredOrigin: '', configuredKind: '' }

describe('what the key row is about', () => {
  it('is a page that has not been told, before the read answers', () => {
    expect(keyBinding(undefined)).toBe('unread')
  })

  it('asks for an endpoint first, with or without a key kept here', () => {
    // Storing needs one to bind to, so this is the state with no repair at
    // that row — and a key that is stored is still reported on the line above.
    expect(keyBinding(NOWHERE)).toBe('no-endpoint')
    expect(keyBinding({ ...BOUND, configuredOrigin: '', configuredKind: '', bound: false })).toBe(
      'no-endpoint'
    )
  })

  it('is none where an endpoint is configured and nothing is stored', () => {
    expect(keyBinding(NONE)).toBe('none')
  })

  it('takes the verdict from the desk and computes none of its own', () => {
    // **Both halves, over identical page-side inputs.** The stored origin and
    // the configured one are the same string in these two cases and only the
    // desk's answer differs — which is the whole of what this module may read.
    expect(keyBinding(BOUND)).toBe('bound')
    expect(keyBinding({ ...BOUND, bound: false })).toBe('rebind')
  })

  it('says rebind for a mismatch this page could never have seen', () => {
    // The measured case: the browser folds an explicit default port away and
    // Go does not. The origins here look equal to any comparison this page
    // could write, and the desk says they are not.
    expect(
      keyBinding({
        ...BOUND,
        origin: 'https://api.example.invalid',
        configuredOrigin: 'https://api.example.invalid:443',
        bound: false
      })
    ).toBe('rebind')
  })
})

describe('the module itself', () => {
  it('computes no origin, compares no host, and reads no configuration', () => {
    // A guard over the source, in the enforcement idiom: the defect was twice
    // a second opinion about something the chassis had already decided, and
    // the repair is that there is no opinion here at all.
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
    for (const shape of [
      'new URL(',
      '.host',
      '.protocol',
      'toLowerCase',
      // The configuration, under either name it could be read by. `no-endpoint`
      // is a *state* of this row and stays; what may not come back is reading
      // the page's copy of the file to decide it.
      'AssistantEndpointConfig',
      'deskConfig',
      'config.assistant'
    ]) {
      expect(code, `keyBinding.ts carries ${shape}`).not.toContain(shape)
    }
    // And it takes one argument, which is the answer: a second one is where
    // the page's configuration got back in last time.
    expect(code).toContain('export function keyBinding(key: AssistantKeyState | undefined)')
  })
})
