import i18next from 'i18next'
import { initReactI18next, useTranslation } from 'react-i18next'
import { LANGUAGES, isLanguage, matchLanguage, systemLanguage, rememberLanguage, type Language, type LanguagePreference } from './locales'
import en from './locales/en.json'

const catalogues = import.meta.glob<{ default: Record<string, string> }>(['./locales/*.json', '!./locales/en.json'])
export const LANGUAGE_KEY = 'jpack-desk.language.v1'
export const i18n = i18next.createInstance()
let preference: LanguagePreference = 'system'
let changeSequence = 0
let ready: Promise<void> = Promise.resolve()
let loadFailed = false
const browserLanguages = () => typeof navigator === 'undefined' ? [] : navigator.languages?.length ? navigator.languages : [navigator.language]
const readPreference = (): LanguagePreference => {
  try { const value = localStorage.getItem(LANGUAGE_KEY); return isLanguage(value) ? value : 'system' } catch { return 'system' }
}
// Tests and non-browser consumers start in English. The application explicitly
// initializes personal settings before mounting; merely importing never writes.
void i18n.use(initReactI18next).init({
  resources: { en: { translation: en } }, lng: 'en', fallbackLng: 'en',
  supportedLngs: LANGUAGES.map(language => language.id), load: 'currentOnly',
  keySeparator: false, nsSeparator: false, returnNull: false,
  interpolation: { escapeValue: false }, react: { useSuspense: false },
  initAsync: false
})
export function language(): Language { return isLanguage(i18n.language) ? i18n.language : 'en' }
export function languagePreference(): LanguagePreference { return preference }
function applyPreference() {
  const next = preference === 'system' ? systemLanguage(browserLanguages()) : preference
  const sequence = ++changeSequence
  const apply = (selected: Language) => {
    if (sequence !== changeSequence) return
    rememberLanguage(selected)
    void i18n.changeLanguage(selected)
    if (typeof document !== 'undefined') { document.documentElement.lang = selected; document.documentElement.dir = 'ltr' }
  }
  loadFailed = false
  if (i18n.hasResourceBundle(next, 'translation')) {
    apply(next); ready = Promise.resolve(); return
  }
  // Keep the current interface intact while a bundled catalogue loads. A stale
  // request cannot override a newer choice or remount the user's workspace.
  ready = catalogues[`./locales/${next}.json`]!().then(catalogue => {
    i18n.addResourceBundle(next, 'translation', catalogue.default)
    apply(next)
  }).catch(() => {
    if (sequence !== changeSequence) return
    loadFailed = true
    apply('en')
  })
}
export function languageReady(): Promise<void> { return ready }
export function languageLoadFailed(): boolean { return loadFailed }
/** Returns whether the personal choice was persisted; blocked storage still applies this visit. */
export function setLanguage(next: LanguagePreference): boolean {
  if (next !== 'system' && !isLanguage(next)) return false
  preference = next
  let stored = true
  try { if (next === 'system') localStorage.removeItem(LANGUAGE_KEY); else localStorage.setItem(LANGUAGE_KEY, next) } catch { stored = false }
  applyPreference()
  return stored
}
export function initializeLanguage(): () => void {
  preference = readPreference(); applyPreference()
  const onSystemChange = () => { if (preference === 'system') applyPreference() }
  const onStorage = (event: StorageEvent) => { if (event.key === LANGUAGE_KEY || event.key === null) { preference = readPreference(); applyPreference() } }
  window.addEventListener('languagechange', onSystemChange)
  window.addEventListener('storage', onStorage)
  return () => { window.removeEventListener('languagechange', onSystemChange); window.removeEventListener('storage', onStorage) }
}
/** Subscribe without remounting editors, unsent messages, or active agent runs. */
export function useLocale(): Language { useTranslation(undefined, { i18n }); return language() }
/** For authored UI messages only. Never pass user text, JSON values, paths, or source excerpts. */
export function msg(source: string, values?: Record<string, unknown>): string {
  return i18n.t(source, { defaultValue: source, ...values }) as string
}
/** Keep regional conventions (for example en-GB dates) when following the system. */
export function formattingLocale(): string {
  if (preference === 'system') {
    for (const tag of browserLanguages()) {
      if (matchLanguage(tag) !== language()) continue
      try { return new Intl.Locale(tag.replaceAll('_', '-').replace(/^zh-yue/i, 'yue')).toString() } catch { /* Try the next preference. */ }
    }
  }
  return language()
}
export function formatNumber(value: number, options?: Intl.NumberFormatOptions): string { return new Intl.NumberFormat(formattingLocale(), options).format(value) }
export function formatDate(value: Date | number, options?: Intl.DateTimeFormatOptions): string { return new Intl.DateTimeFormat(formattingLocale(), options).format(value) }

// The run state and durable history retain the original Desk status message.
// Translate it at presentation time so old progress updates follow the current
// language. This is only for Desk-authored status/errors, never model prose,
// source evidence, document values, or raw runtime diagnostics.
const statusPatterns = Object.entries(en).filter(([, source]) => source.includes('{{') && !source.includes('<') && (source.includes('{{count}}') || source.replace(/\{\{\w+\}\}/g, '').trim().length >= 6))
  // Match the most specific caption first, so a short log prefix cannot
  // consume a longer notice and leave its remaining explanation in English.
  .sort((a, b) => b[1].replace(/\{\{\w+\}\}/g, '').length - a[1].replace(/\{\{\w+\}\}/g, '').length)
  .map(([key, source]) => {
  const names: string[] = []
  const pattern = source.split(/(\{\{\w+\}\})/).map(part => {
    if (/^\{\{\w+\}\}$/.test(part)) { names.push(part.slice(2, -2)); return '([\\s\\S]*?)' }
    return part.replace(/[.*+?^${}()|[\]\\]/g, '\\$&')
  }).join('')
  return { key: key.replace(/_(one|other)$/, ''), names, pattern: new RegExp(`^${pattern}$`) }
})
export function systemMessage(source: string, depth = 0): string {
  if (!source || language() === 'en' || depth > 8) return source
  if (Object.hasOwn(en, source)) return msg(source)
  for (const entry of statusPatterns) {
    const found = entry.pattern.exec(source)
    if (found) return msg(entry.key, Object.fromEntries(entry.names.map((name, index) => {
      const value = found[index + 1]!
      return [name, /^message\d+$/.test(name) ? systemMessage(value, depth + 1) : name === 'count' && /^\d+$/.test(value) ? Number(value) : value]
    })))
  }
  // Composed Desk notices keep each paragraph canonical. Only explicitly
  // named message slots recurse; identifiers and user/model prose never do.
  if (source.includes('\n\n')) return source.split('\n\n').map(part => systemMessage(part, depth + 1)).join('\n\n')
  return source
}
