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

it('validates original runtime controls before applying canonical precedence', () => {
  for (const value of [null, false, true, 0, 1, '', 'invalid', [], {}]) {
    expect(() => savedCriticChoice({critic: value}), `critic: ${JSON.stringify(value)}`).toThrow('Invalid saved chat')
    expect(() => savedCriticChoice({critic: value, adversarialReview: true})).toThrow('Invalid saved chat')
  }
  for (const value of [null, 0, 1, '', 'false', 'invalid', [], {}]) {
    expect(() => savedCriticChoice({adversarialReview: value}), `adversarialReview: ${JSON.stringify(value)}`).toThrow('Invalid saved chat')
    expect(() => savedCriticChoice({critic: 'on', adversarialReview: value})).toThrow('Invalid saved chat')
  }
})

it('preserves undefined absence, valid booleans and canonical precedence at the runtime boundary', () => {
  expect(savedCriticChoice({critic: undefined, adversarialReview: undefined})).toBeUndefined()
  expect(savedCriticChoice({critic: undefined, adversarialReview: false})).toBe('off')
  expect(savedCriticChoice({adversarialReview: true})).toBe('on')
  for (const critic of ['off', 'auto', 'on'] as const) {
    for (const adversarialReview of [undefined, false, true]) {
      expect(savedCriticChoice({critic, adversarialReview})).toBe(critic)
    }
  }
})
