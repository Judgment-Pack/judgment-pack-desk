import { expect, it } from 'vitest'
import { criticMode, readCriticSettings } from '@judgment-pack/harness/criticSwitch'
import vectors from '../chat/fixtures/critic-controls.json'
import { savedCriticChoice } from './criticSettings'
import type { CriticMode, CriticTier } from '@judgment-pack/harness/contract'

it('projects every saved critic control vector into the released canonical reader', () => {
  for (const row of vectors) {
    const chat = row.chat as {critic?: CriticMode; criticReasoning?: CriticTier; adversarialReview?: boolean}
    const settings = {critic: savedCriticChoice(chat), criticReasoning: chat.criticReasoning, criticProject: row.project || undefined} as Parameters<typeof readCriticSettings>[0]
    expect(() => readCriticSettings(settings, row.engine === 'codex'), row.name).not.toThrow()
    expect(criticMode(settings, {}).mode, row.name).toBe(row.mode)
    expect(Object.hasOwn(settings, 'adversarialReview')).toBe(false)
  }
})

it('keeps inheritance absent and refuses unconverted retired presence', () => {
  expect(savedCriticChoice({})).toBeUndefined()
  for (const adversarialReview of [undefined, false, true]) {
    expect(() => readCriticSettings({adversarialReview}, false)).toThrow(/adversarialReview/)
  }
})
