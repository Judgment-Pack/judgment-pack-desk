import { readFileSync } from 'node:fs'
import { join } from 'node:path'
import { describe, expect, it, vi } from 'vitest'
import { STATE_HEADER } from '@judgment-pack/harness/engines/vercel/loop'
import * as host from '@judgment-pack/harness/host'
import { capabilitiesFor, FAMILY_CAPABILITIES } from '@judgment-pack/harness/capabilities'
import { ASSISTANT_KINDS } from '../config/deskConfig'
import * as capabilities from './endpointCapabilities'
import { IMAGES_NOT_RENDERED } from './endpointCapabilities'

/**
 * The Desk's `configureHarness` call (`assistant/harnessHost.ts`), which the test
 * ground imports before every test as `main.tsx` imports it before the page
 * starts: every injection point of the package is the Desk's, none is the
 * package's own stand-in, which throws naming itself.
 */
describe('the Desk as the harness host', () => {
  it('supplies every injection point the package declares', () => {
    const notInjected = new Set(['ASSISTANT_TOOLS', 'ASSISTANT_ENGINES', 'NO_SESSION_MESSAGE', 'configureHarness', 'sourceMessage'])
    const points = Object.entries(host).filter(([name]) => !notInjected.has(name))
    // Written out, so a release that adds an injection point fails here until the Desk wires it.
    expect(points.map(([name]) => name).sort()).toEqual([
      'assistantLanguageInstructions', 'currentLanguage', 'sessionBearer', 'sessionEnded', 'whenSessionEnds',
      'forgetSession', 'refusalCode', 'discardBody', 'NoSession', 'chassisUrl', 'deskHeaders', 'deskSocketURL',
      'aiHeaders', 'socketURL', 'socketProtocols', 'DeskWebSocketTransport', 'deskFetch', 'answer', 'readFile',
      'dialectAPI', 'endpointCapabilities', 'renderImageToText'
    ].sort())
    for (const [name, value] of points) {
      expect(typeof value, name).toBe('function')
      expect(String(value), `${name} is the package's stand-in`).not.toMatch(/unsupplied/i)
    }
    // endpointCapabilities has an answer of its own before a host supplies it; the Desk's is read through its module.
    const answered = vi.spyOn(capabilities, 'endpointCapabilities')
    host.endpointCapabilities({ family: 'gemini', model: 'any-model' })
    expect(answered).toHaveBeenCalledWith({ family: 'gemini', model: 'any-model' })
    answered.mockRestore()
    expect(host.NO_SESSION_MESSAGE).not.toBe('no session')
  })

  it('answers nothing beyond the family, so every endpoint runs on its family default record', () => {
    for (const family of ASSISTANT_KINDS) {
      expect(host.endpointCapabilities({ family, model: 'any-model' })).toEqual({})
      expect(capabilitiesFor({ family, model: 'any-model' })).toEqual(FAMILY_CAPABILITIES[family])
    }
    // The state's placement and the turn order are the family's: neither member is set.
    expect(capabilitiesFor({ family: 'openai-compatible', model: 'any-model' })).not.toHaveProperty('strictAlternation')
    expect(capabilitiesFor({ family: 'anthropic', model: 'any-model' })).not.toHaveProperty('statePlacement')
  })

  it('refuses to render an image as text, with a sentence the model reads in its place', async () => {
    const image = { kind: 'image' as const, mediaType: 'image/png' as const, bytes: 'iVBORw0KGgo=' }
    await expect(host.renderImageToText(image, new AbortController().signal)).rejects.toThrow(IMAGES_NOT_RENDERED)
  })
})

describe('what the chassis reads of the harness\'s requests', () => {
  it('drops the execution state from the memory recall query by the header the harness writes', () => {
    // internal/desk/ai_context.go reads the recall query off the newest user
    // text, and drops a text part that starts with this header: the harness's
    // STATE_HEADER (the contract vector budget/state-placement.json). Read out
    // of the Go source, so the two cannot drift.
    const source = readFileSync(join(import.meta.dirname, '..', '..', '..', 'internal', 'desk', 'ai_context.go'), 'utf8')
    const declared = /const executionStateHeader = ("[^"\n]*")/.exec(source)
    expect(declared, 'executionStateHeader is declared in internal/desk/ai_context.go').not.toBeNull()
    expect(JSON.parse(declared![1]!)).toBe(STATE_HEADER)
  })
})
