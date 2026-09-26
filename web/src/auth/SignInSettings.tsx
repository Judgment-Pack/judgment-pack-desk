import { useCallback, useEffect, useMemo, useRef, useState } from 'react'
import { msg, useLocale } from '../i18n'
import { forgetSession, sessionBearer } from '../mcp/session'
import { useInspectorPortal } from '../shell/InspectorSlot'
import { useInspectorPresentation } from '../shell/InspectorPresentation'
import { Button } from '../ui/Button'
import { Disclosure } from '../ui/Disclosure'
import { Input } from '../ui/Input'
import { SettingsSection } from '../ui/SettingsSection'
import { authCall, beginSignIn, signInProblem, type SignInProvider, type SignInSettings as Settings } from './flow'
import { signInMessage } from './messages'
import styles from './SignInSettings.module.css'

const empty: SignInProvider = { label: '', issuer: '', clientId: '' }

export function SignInSettings() {
  const locale = useLocale()
  const [settings, setSettings] = useState<Settings | null>(null)
  const [provider, setProvider] = useState<SignInProvider>(empty)
  const [open, setOpen] = useState(false)
  const [width, setWidth] = useState(480)
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState<unknown>(signInProblem())
  const opener = useRef<HTMLButtonElement>(null)
  const close = useCallback(() => { setOpen(false); opener.current?.focus() }, [])
  const onOpenChange = useCallback((value: boolean) => { if (!value) close() }, [close])
  const reset = useCallback(() => setWidth(480), [])
  const presentation = useMemo(() => ({ title: msg('Sign-in & access'), available: open, open,
    onOpenChange, width, onResize: setWidth, onReset: reset, minimumMainWidth: 560,
    maximumWidth: 560, closeOnEscape: true, restoreFocusRef: opener
  }), [open, onOpenChange, width, reset, locale])
  useInspectorPresentation(presentation)
  useEffect(() => {
    let active = true
    void sessionBearer().then(bearer => authCall<Settings>('settings', undefined, bearer)).then(value => {
      if (!active) return
      setSettings(value); setProvider(value.provider ?? empty)
      if (value.testId || signInProblem()) setOpen(true)
    }).catch(cause => { if (active) setError(cause) })
    return () => { active = false }
  }, [])
  const unchanged = !!settings?.testId && provider.label === settings.provider?.label && provider.issuer === settings.provider?.issuer && provider.clientId === settings.provider?.clientId && !provider.clientSecret
  async function test() {
    setBusy(true); setError('')
    try {
      await beginSignIn('test', { provider, keepSecret: !!settings?.hasSecret && !provider.clientSecret }, await sessionBearer())
    } catch (cause) { setError(cause); setBusy(false) }
  }
  async function enable() {
    setBusy(true); setError('')
    try {
      await authCall('enable', { testId: settings?.testId }, await sessionBearer())
      forgetSession()
      window.location.assign('/')
    } catch (cause) { setError(cause); setBusy(false) }
  }
  const field = (key: keyof SignInProvider, value: string) => setProvider(current => ({ ...current, [key]: value }))
  const pane = useInspectorPortal(open ? <div className={styles.form}>
    <p>{msg('Connect an identity provider, test your account, then enable sign-in for this computer.')}</p>
    {!!error && <p role="alert">{signInMessage(error)}</p>}
    <form onSubmit={event => { event.preventDefault(); void test() }}>
      <label htmlFor="auth-label">{msg('Provider name')}</label>
      <Input id="auth-label" value={provider.label} maxLength={80} required disabled={busy} onChange={event => field('label', event.target.value)} />
      <label htmlFor="auth-issuer">{msg('Issuer URL')}</label>
      <Input id="auth-issuer" type="url" value={provider.issuer} maxLength={2048} required disabled={busy} onChange={event => field('issuer', event.target.value.trim())} />
      <label htmlFor="auth-client">{msg('Client ID')}</label>
      <Input id="auth-client" value={provider.clientId} maxLength={512} required disabled={busy} autoComplete="off" onChange={event => field('clientId', event.target.value.trim())} />
      <Disclosure title={msg('Registration secret (optional)')}>
        <p>{msg('Only enter a secret if your desktop registration supplies one. It is stored on this computer and never included in releases.')}</p>
        <label htmlFor="auth-secret">{msg('Client secret')}</label>
        <Input id="auth-secret" type="password" value={provider.clientSecret ?? ''} maxLength={4096} disabled={busy} autoComplete="new-password" onChange={event => field('clientSecret', event.target.value)} />
        {settings?.hasSecret && <p>{msg('A secret is already saved. Leave this blank to keep it for the same registration.')}</p>}
      </Disclosure>
      <label htmlFor="auth-callback">{msg('Redirect URI')}</label>
      <Input id="auth-callback" value={settings?.callbackUrl ?? ''} readOnly onFocus={event => event.target.select()} />
      <p className={styles.hint}>{msg('Register this exact address. Use the same Desk port after restarting.')}</p>
      <Disclosure title={msg('Setup instructions')}>
        <ol>
          <li>{msg('Create a desktop or public client registration with your identity provider. Enable OpenID Connect authorization code sign-in with PKCE.')}</li>
          <li>{msg('For Google, choose Desktop app and use https://accounts.google.com as the issuer. Complete the basic consent screen and add your account as a test user if the app is in Testing.')}</li>
          <li>{msg('For Microsoft Entra ID, add the Mobile and desktop applications platform and the redirect URI above. Use https://login.microsoftonline.com/YOUR-TENANT-ID/v2.0 as the issuer.')}</li>
          <li>{msg('For another provider, copy its OpenID Connect issuer and public client ID. Allow the redirect URI above and the openid, profile, and email scopes.')}</li>
          <li>{msg('Test sign-in with the account that should own this Desk. After verification, enable sign-in below.')}</li>
        </ol>
        <p><a href="https://developers.google.com/identity/protocols/oauth2/native-app" target="_blank" rel="noreferrer">{msg('Google documentation')}</a>{' · '}<a href="https://learn.microsoft.com/en-us/entra/identity-platform/scenario-desktop-app-registration" target="_blank" rel="noreferrer">{msg('Microsoft documentation')}</a></p>
      </Disclosure>
      <Button type="submit" disabled={busy || !settings?.storageAvailable} variant={unchanged ? 'secondary' : 'primary'}>{busy ? msg('Working…') : msg('Test sign-in')}</Button>
    </form>
    {unchanged && settings?.testedOwner && <section className={styles.verified}>
      <h3>{msg('Account verified')}</h3>
      <p>{settings.testedOwner.name || settings.testedOwner.email || settings.testedOwner.subject}</p>
      {settings.testedOwner.email && settings.testedOwner.name && <p>{settings.testedOwner.email}</p>}
      <p>{msg('This account will have access to all existing packs and chats in this Desk. Other accounts will be refused. Enabling sign-in ends current Desk sessions.')}</p>
      <Disclosure title={msg('Technical details')}><p>{settings.testedOwner.issuer}</p><code>{settings.testedOwner.subject}</code></Disclosure>
      <Button variant="primary" disabled={busy} onClick={() => void enable()}>{msg('Enable sign-in for this account')}</Button>
    </section>}
    <Disclosure title={msg('Recovery')}><p>{msg('If you lose access to the provider, stop Desk and run jpack-desk --reset-sign-in on this computer, then restart and set up sign-in again. Packs, chats, and source connections are preserved.')}</p></Disclosure>
  </div> : null)
  return <SettingsSection title={msg('Sign-in & access')} level={2} variant="standalone" description={msg('Personal · This computer')}>
    {pane}
    {!settings && !error && <p role="status">{msg('Loading…')}</p>}
    {!open && !!error && <p role="alert">{signInMessage(error)}</p>}
    {settings && <>
      <p>{settings.enabled ? settings.owner?.name || settings.owner?.email || settings.owner?.subject : msg('Sign-in is not configured.')}</p>
      <p>{msg('One owner per local Desk. Source connections are managed separately.')}</p>
      {!settings.storageAvailable && <p role="alert">{signInMessage('storage')}</p>}
      <Button ref={opener} onClick={() => setOpen(true)}>{settings.enabled ? msg('Manage sign-in') : msg('Set up sign-in')}</Button>
    </>}
  </SettingsSection>
}
