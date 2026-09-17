import { describe, expect, it } from 'vitest'
import { assistantLanguageInstructions } from './assistantLanguage'
import { LANGUAGES, matchLanguage, systemLanguage } from './locales'

describe('language negotiation', () => {
  it.each([
    ['en-CA', 'en'], ['fr-CA', 'fr'], ['es-MX', 'es'], ['de-AT', 'de'],
    ['it-CH', 'it'], ['pt', 'pt-PT'], ['pt-AO', 'pt-PT'], ['pt-BR', 'pt-BR'],
    ['ko-KR', 'ko'], ['ja-JP', 'ja'], ['zh-CN', 'zh-Hans'], ['zh-SG', 'zh-Hans'],
    ['zh-TW', 'zh-Hant'], ['zh-HK', 'zh-Hant'], ['zh-Hans-HK', 'zh-Hans'],
    ['yue-HK', 'yue-Hant'], ['zh-yue-HK', 'yue-Hant'], ['pt_BR', 'pt-BR']
  ])('matches %s to %s', (tag, expected) => expect(matchLanguage(tag)).toBe(expected))
  it('uses the first supported preference and otherwise English', () => {
    expect(systemLanguage(['invalid_tag_123', 'ar', 'fr-CA', 'en'])).toBe('fr')
    expect(systemLanguage(['ar', 'ru'])).toBe('en')
    expect(systemLanguage([])).toBe('en')
    expect(matchLanguage('')).toBeUndefined()
  })
  it('does not infer Cantonese from a region alone', () => {
    expect(matchLanguage('zh-HK')).toBe('zh-Hant')
    expect(matchLanguage('yue')).toBe('yue-Hant')
  })
  it.each(LANGUAGES)('carries $id through assistant replies without changing pack semantics', locale => {
    const instructions = assistantLanguageInstructions(locale.id)
    expect(instructions).toContain(locale.instruction)
    expect(instructions).toContain('unless the user explicitly requests another language')
    expect(instructions).toContain('Never translate tool names, JSON/schema keys')
    expect(instructions).toContain('Do not change decision semantics or test expectations')
  })
})
