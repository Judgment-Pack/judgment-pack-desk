/** BCP 47 language identifiers are preferences, never protocol/schema values. */
export const LANGUAGES = [
  { id: 'en', label: 'English', instruction: 'English' },
  { id: 'fr', label: 'Français', instruction: 'French' },
  { id: 'es', label: 'Español', instruction: 'Spanish' },
  { id: 'de', label: 'Deutsch', instruction: 'German' },
  { id: 'it', label: 'Italiano', instruction: 'Italian' },
  { id: 'pt-PT', label: 'Português (Portugal)', instruction: 'European Portuguese' },
  { id: 'pt-BR', label: 'Português (Brasil)', instruction: 'Brazilian Portuguese' },
  { id: 'ko', label: '한국어', instruction: 'Korean' },
  { id: 'zh-Hans', label: '普通话（简体中文）', instruction: 'Mandarin Chinese using Simplified Chinese characters' },
  { id: 'zh-Hant', label: '國語（繁體中文）', instruction: 'Mandarin Chinese using Traditional Chinese characters' },
  { id: 'yue-Hant', label: '廣東話（繁體中文）', instruction: 'Cantonese using Traditional Chinese characters and natural Cantonese wording' },
  { id: 'ja', label: '日本語', instruction: 'Japanese' }
] as const
export type Language = typeof LANGUAGES[number]['id']
export type LanguagePreference = Language | 'system'
export const isLanguage = (value: unknown): value is Language => LANGUAGES.some(language => language.id === value)

export function matchLanguage(tag: string): Language | undefined {
  try {
    // Older platforms also report the zh-yue and underscore spellings.
    const normalized = tag.replaceAll('_', '-').replace(/^zh-yue/i, 'yue')
    const locale = new Intl.Locale(normalized)
    const { language, script, region } = locale
    if (language === 'yue') return 'yue-Hant'
    if (language === 'zh' || language === 'cmn') return script === 'Hant' || (!script && ['TW', 'HK', 'MO'].includes(region ?? '')) ? 'zh-Hant' : 'zh-Hans'
    if (language === 'pt') return region === 'BR' ? 'pt-BR' : 'pt-PT'
    return isLanguage(language) ? language : undefined
  } catch { return undefined }
}
export function systemLanguage(languages: readonly string[]): Language {
  for (const tag of languages) { const match = matchLanguage(tag); if (match) return match }
  return 'en'
}

let activeLanguage: Language = 'en'
export const currentLanguage = (): Language => activeLanguage
export function rememberLanguage(value: Language) { activeLanguage = value }
