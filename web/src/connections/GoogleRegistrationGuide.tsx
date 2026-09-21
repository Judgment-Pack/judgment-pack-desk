import { useEffect, type RefObject } from 'react'
import { msg, useLocale } from '../i18n'
import { Button } from '../ui/Button'
import type { ConnectionProvider } from './client'
import styles from './GoogleRegistrationGuide.module.css'

/** Local setup help shares the shell's dock, drawer, scrolling and resizing. */
export function GoogleRegistrationGuide({ provider, onContinue, headingRef }: { provider: ConnectionProvider; onContinue: () => void; headingRef: RefObject<HTMLHeadingElement | null> }) {
  useLocale()
  useEffect(() => { headingRef.current?.focus() }, [provider, headingRef])
  const name = provider === 'gmail' ? msg('Gmail') : msg('Google Drive')
  const scope = `https://www.googleapis.com/auth/${provider === 'gmail' ? 'gmail.readonly' : 'drive.file'}`
  return <section className={styles.guide}>
    <h2 ref={headingRef} tabIndex={-1}>{msg('{{provider}} registration', { provider: name })}</h2>
    <p>{msg('Set up a Google app once on this computer, then connect your account.')}</p>
    <ol className={styles.steps}>
      <li><h3>{msg('Create a Google Cloud project')}</h3>
        <p>{msg('Open Google Cloud Console and select or create a project.')}</p>
        <a href="https://console.cloud.google.com/" target="_blank" rel="noreferrer">{msg('Open Google Cloud Console')}</a>
      </li>
      <li><h3>{msg('Enable the APIs')}</h3>
        <p>{provider === 'gmail' ? msg('In APIs & Services → Library, enable Gmail API.') : msg('In APIs & Services → Library, enable Google Drive API and Google Picker API.')}</p>
      </li>
      <li><h3>{msg('Configure the consent screen')}</h3>
        <p>{msg('In Google Auth Platform → Branding, enter your app name and contact email.')}</p>
        <p>{msg('Open Audience in the left sidebar. Choose External for personal Google accounts. Internal is limited to your Google Workspace organization.')}</p>
        <p>{msg('For your own local Desk, you can keep In production. If you use Testing, add your Google email under Test users and expect to reconnect Drive or Gmail after seven days.')}</p>
        <p>{msg('Personal-use apps can qualify for an exemption from verification. Google may still show an unverified-app warning.')}</p>
      </li>
      <li><h3>{msg('Choose access permissions')}</h3>
        <p>{msg('In Data Access, add this scope:')}</p><code>{scope}</code>
        <p>{provider === 'gmail' ? msg('Gmail read-only access lets Desk read email; only messages you select are attached to chat.') : msg('Desk uses this permission for files you select in Google Picker.')}</p>
      </li>
      <li><h3>{msg('Create desktop credentials')}</h3>
        <p>{msg('In Google Auth Platform → Clients, choose Create client, select Desktop app, then download the credentials JSON.')}</p>
      </li>
      <li><h3>{msg('Finish in Desk')}</h3>
        <p>{msg('Choose Continue setup, upload the downloaded JSON, then open the chat + menu and select {{provider}} to sign in with Google.', { provider: name })}</p>
        <Button onClick={onContinue}>{msg('Continue setup')}</Button>
      </li>
    </ol>
    <footer className={styles.footer}><a href="https://developers.google.com/workspace/guides/create-credentials#desktop-app" target="_blank" rel="noreferrer">{msg('Official Google documentation')}</a></footer>
  </section>
}
