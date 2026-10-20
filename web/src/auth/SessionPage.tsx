import {LoadingState} from '../ui/LoadingState'
import { SignInEntry } from './SignInEntry'
import { useEffect, useRef, useState } from 'react'
import { languageLoadFailed, languagePreference, languageReady, msg, setLanguage, useLocale } from '../i18n'
import { LANGUAGES, type LanguagePreference } from '../i18n/locales'
import { Button } from '../ui/Button'
import { Select } from '../ui/Select'
import { TooltipProvider } from '../ui/Tooltip'
import { useAppliedTheme } from '../config/theme'
import { chooseEntryTheme, pendingEntryTheme, readEntryTheme } from '../config/entryTheme'
import { DeskIdentity } from '../desks/DeskIdentity'
import { BrandMark } from '../ui/BrandMark'
import { useEntryDesks } from './entryDesks'
import type { ThemeChoice } from '../config/deskConfig'
import styles from './SessionPage.module.css'

export type AccessState = 'checking' | 'ready' | 'recovery' | 'unavailable'
/** Local-only entry; provider sign-in is shown only once a real backend exists. */
export function SessionPage(props: { state: AccessState; detail?: string }) {
  // Unknown access is loading, not sign-in. Keep public entry content and its
  // saved preference out of the workspace bootstrap path.
  return props.state === 'checking' ? <SessionLoading /> : <SessionPageContent state={props.state} detail={props.detail} />
}

function SessionLoading() {
  useLocale()
  // Only a fresh explicit choice may cross sign-in's document reload. Reading
  // it here does not consume it or apply an old public-page preference.
  useAppliedTheme(pendingEntryTheme()?.theme)
  return <main className={styles.loading}><LoadingState>{msg('Opening Desk…')}</LoadingState></main>
}

function SessionPageContent({ state }: { state: Exclude<AccessState, 'checking'>; detail?: string }) {
  useLocale()
  const [theme, setTheme] = useState<ThemeChoice>(readEntryTheme)
  const desks = useEntryDesks(state === 'recovery')
  const [signingIn, setSigningIn] = useState(false)
  const [languageSaved, setLanguageSaved] = useState(true)
  const [loadingLanguage, setLoadingLanguage] = useState(false)
  const heading = useRef<HTMLHeadingElement>(null)
  useAppliedTheme(theme)
  useEffect(() => { heading.current?.focus() }, [state])
  return <TooltipProvider><div className={styles.page}>
    <header className={styles.header}>
      <DeskIdentity name={desks.selected?.name ?? (desks.directory ? msg('Desk unavailable') : msg('Desk'))} mark={desks.selected?.mark}
        value={desks.selection ?? ''} desks={desks.directory?.desks ?? []} startup={desks.directory?.startup} disabled={signingIn || !desks.directory} onSelect={desks.choose} />
      <div className={styles.preferences}>
        <Select id="entry-language" aria-label={msg('Language')} value={languagePreference()} quiet options={[{ value: 'system', label: msg('Use system language') }, ...LANGUAGES.map(item => ({ value: item.id, label: item.label }))]} onValueChange={async value => {
          setLoadingLanguage(true); setLanguageSaved(setLanguage(value as LanguagePreference)); await languageReady(); setLoadingLanguage(false)
        }} disabled={loadingLanguage} />
        <Select id="entry-appearance" aria-label={msg('Appearance')} value={theme} quiet options={[{ value: 'system', label: msg('System') }, { value: 'light', label: msg('Light') }, { value: 'dark', label: msg('Dark') }]} onValueChange={value => {
          setTheme(value as ThemeChoice)
        }} onValueCommit={value => chooseEntryTheme(value as ThemeChoice)} />
      </div>
    </header>
    <main className={styles.main}>
      <div className={styles.content}>
        {desks.selected && <BrandMark mark={desks.selected.mark} className={styles.largeMark} />}
        {state === 'recovery' ? <>
          {desks.selected && !desks.problem ? <SignInEntry key={desks.selected.id} name={desks.selected.name} onBusyChange={setSigningIn} /> : <>
            {desks.problem || desks.directory ? <>
              <h1 ref={heading} tabIndex={-1}>{msg('Choose a desk')}</h1>
              <p role="alert" className={styles.notice}>{desks.problem === 'storage' ? msg('This browser could not save the selected desk. Allow site storage and try again.') : desks.problem === 'unavailable' ? msg('Desks could not be loaded or saved. Please try again.') : msg('The selected desk is unavailable. Choose another desk.')}</p>
              <Button onClick={desks.retry}>{msg('Try again')}</Button>
            </> : <LoadingState>{msg('Loading…')}</LoadingState>}
          </>}
        </> : <>
          <h1 ref={heading} tabIndex={-1}>{msg('Desk is not responding')}</h1>
          <p className={styles.lead}>{msg('Make sure Desk is running on this computer, then try again.')}</p>
          {state === 'unavailable' && <Button variant="primary" className={styles.action} onClick={() => window.location.reload()}>{msg('Try again')}</Button>}
        </>}
        {!languageSaved && <p role="status" className={styles.notice}>{msg('Language changed for this visit. This browser could not save your preference.')}</p>}
        {languageLoadFailed() && <p role="status" className={styles.notice}>{msg('The language could not be loaded. Using English for now. Select the language again to retry.')}</p>}
      </div>
    </main>
    <footer className={styles.footer}>{msg('Personal · This computer')}</footer>
  </div></TooltipProvider>
}
