import { SignInEntry } from './SignInEntry'
import { useEffect, useRef, useState } from 'react'
import { languageLoadFailed, languagePreference, languageReady, msg, setLanguage, useLocale } from '../i18n'
import { LANGUAGES, type LanguagePreference } from '../i18n/locales'
import { Button } from '../ui/Button'
import { Select } from '../ui/Select'
import { TooltipProvider } from '../ui/Tooltip'
import { applyTheme } from '../config/theme'
import type { ThemeChoice } from '../config/deskConfig'
import styles from './SessionPage.module.css'

// Public product branding is available before project configuration is authorized.
const PRODUCT_NAME = 'Unveil'
const PRODUCT_MARK = 'U'
const ENTRY_THEME_KEY = 'jpack-desk:entry-theme'

function entryTheme(): ThemeChoice {
  try {
    const stored = localStorage.getItem(ENTRY_THEME_KEY)
    if (stored === 'system' || stored === 'dark' || stored === 'light') return stored
  } catch { /* Private browsing may deny preference storage. */ }
  const current = document.documentElement.dataset.theme
  return current === 'dark' || current === 'light' ? current : 'system'
}

export type AccessState = 'checking' | 'ready' | 'recovery' | 'unavailable'
/** Local-only entry; provider sign-in is shown only once a real backend exists. */
export function SessionPage({ state }: { state: AccessState; detail?: string }) {
  useLocale()
  const [theme, setTheme] = useState<ThemeChoice>(entryTheme)
  const [languageSaved, setLanguageSaved] = useState(true)
  const [loadingLanguage, setLoadingLanguage] = useState(false)
  const heading = useRef<HTMLHeadingElement>(null)
  useEffect(() => { applyTheme(theme) }, [theme])
  useEffect(() => { if (state !== 'checking') heading.current?.focus() }, [state])
  return <TooltipProvider><div className={styles.page}>
    <header className={styles.header}>
      <div className={styles.brand}><span className={styles.mark} aria-hidden="true">{PRODUCT_MARK}</span><span>{PRODUCT_NAME}</span></div>
      <div className={styles.preferences}>
        <Select id="entry-language" aria-label={msg('Language')} value={languagePreference()} quiet options={[{ value: 'system', label: msg('Use system language') }, ...LANGUAGES.map(item => ({ value: item.id, label: item.label }))]} onValueChange={async value => {
          setLoadingLanguage(true); setLanguageSaved(setLanguage(value as LanguagePreference)); await languageReady(); setLoadingLanguage(false)
        }} disabled={loadingLanguage} />
        <Select id="entry-appearance" aria-label={msg('Appearance')} value={theme} quiet options={[{ value: 'system', label: msg('System') }, { value: 'light', label: msg('Light') }, { value: 'dark', label: msg('Dark') }]} onValueChange={value => {
          setTheme(value as ThemeChoice)
          try { localStorage.setItem(ENTRY_THEME_KEY, value) } catch { /* Choice still applies for this visit. */ }
        }} />
      </div>
    </header>
    <main className={styles.main}>
      <div className={styles.content}>
        <span className={styles.largeMark} aria-hidden="true">{PRODUCT_MARK}</span>
        {state === 'recovery' ? <SignInEntry /> : <>
          <h1 ref={heading} tabIndex={-1}>{state === 'checking' ? msg('Opening Desk…') : msg('Desk is not responding')}</h1>
          <p className={styles.lead}>{state === 'checking' ? msg('Checking your local session…') : msg('Make sure Desk is running on this computer, then try again.')}</p>
          {state === 'unavailable' && <Button variant="primary" className={styles.action} onClick={() => window.location.reload()}>{msg('Try again')}</Button>}
        </>}
        {!languageSaved && <p role="status" className={styles.notice}>{msg('Language changed for this visit. This browser could not save your preference.')}</p>}
        {languageLoadFailed() && <p role="status" className={styles.notice}>{msg('The language could not be loaded. Using English for now. Select the language again to retry.')}</p>}
      </div>
    </main>
    <footer className={styles.footer}>{msg('Personal · This computer')}</footer>
  </div></TooltipProvider>
}
