import {LoadingState} from '../ui/LoadingState'
import { useEffect, useRef, useState } from 'react'
import { msg, useLocale } from '../i18n'
import { keepSetupSession } from '../mcp/session'
import { Button } from '../ui/Button'
import { Input } from '../ui/Input'
import { PasswordInput } from '../ui/PasswordInput'
import { Field } from '../ui/Field'
import { Disclosure } from '../ui/Disclosure'
import { authCall, beginSignIn, signInProblem, type SignInStatus } from './flow'
import { signInMessage } from './messages'
import styles from './SessionPage.module.css'

export function SignInEntry({ name, onBusyChange }: { name?: string; onBusyChange?: (busy: boolean) => void }) {
  useLocale()
  const heading = useRef<HTMLHeadingElement>(null)
  useEffect(() => { heading.current?.focus() }, [])
  const [status, setStatus] = useState<SignInStatus | null>(null)
  const [setup, setSetup] = useState(false)
  const [code, setCode] = useState('')
  const [password, setPassword] = useState('')
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState(signInProblem())
  useEffect(() => {
    const abort = new AbortController()
    void authCall<SignInStatus>('status', undefined, undefined, abort.signal).then(value => {
      if (typeof value?.enabled !== 'boolean' || typeof value?.unavailable !== 'boolean') throw new Error()
      setStatus(value)
    }).catch(() => { if (!abort.signal.aborted) setError('unavailable') })
    return () => abort.abort()
  }, [])
  async function submit() {
    setBusy(true); onBusyChange?.(true); setError('')
    try {
      if (status?.mode === 'password') {
        const reply = await authCall<{ id: string }>('password/login', { password })
        setPassword('')
        keepSetupSession(reply.id)
        window.location.reload()
      } else if (setup) {
        const reply = await authCall<{ id: string }>('setup', { code: code.trim() })
        keepSetupSession(reply.id)
        window.location.assign('/admin#identity-provider')
      } else {
        await beginSignIn('login', { returnPath: window.location.pathname + window.location.search + window.location.hash })
      }
    } catch (cause) { setError(cause instanceof Error ? ('code' in cause ? String(cause.code) : 'unavailable') : 'unavailable'); setBusy(false); onBusyChange?.(false) }
  }
  const passwordMode = status?.mode === 'password'
  const local = status?.localAccess === true && !status.enabled && !status.unavailable
  return <>
    <h1 ref={heading} tabIndex={-1}>{local ? msg('Open {{desk}}', {desk: name ?? msg('Desk')}) : setup || status?.enabled === false ? msg('Set up sign-in') : msg('Sign in to {{desk}}', {desk: name ?? msg('Desk')})}</h1>
    <p className={styles.lead}>{local ? msg('Personal · This computer') : status?.enabled ? msg('Sign in to open your local workspace. Signing in does not upload your packs or chats.') : msg('The installation owner needs to configure sign-in once on this computer.')}</p>
    {error && <p role="alert" className={styles.notice}>{signInMessage(error)}</p>}
    {status?.unavailable && <p role="alert">{msg('Sign-in is locked because its configuration cannot be read. Contact the installation owner.')}</p>}
    {!status && !error && <LoadingState>{msg('Loading…')}</LoadingState>}
    {status && !status.unavailable && (passwordMode ? <form onSubmit={event => { event.preventDefault(); void submit() }}>
      <div className={styles.setupCode}><Field label={msg('Desk password')}>{wiring => <PasswordInput {...wiring} autoComplete="current-password" value={password} onChange={event => setPassword(event.target.value)} disabled={busy} required maxLength={256} />}</Field></div>
      <Button type="submit" variant="primary" className={styles.action} disabled={busy || !password}>{busy ? msg('Signing in…') : msg('Sign in')}</Button>
      <div className={styles.detail}><Disclosure title={msg('Forgot password?')}><p>{msg('Stop Desk and run jpack-desk --reset-sign-in on this computer, then restart and set up sign-in again. Packs, chats, and source connections are preserved.')}</p></Disclosure></div>
    </form> : local ? <Button variant="primary" className={styles.action} onClick={() => window.location.reload()}>{msg('Continue')}</Button> : status.enabled || setup ? <form onSubmit={event => { event.preventDefault(); void submit() }}>
      {setup && <div className={styles.setupCode}>
        <label htmlFor="sign-in-setup-code">{msg('Owner setup code')}</label>
        <Input id="sign-in-setup-code" type="password" autoComplete="off" value={code} onChange={event => setCode(event.target.value)} autoFocus />
        <p>{msg('Use the setup code shown in the terminal where Desk is running. Use it to set a Desk password or configure provider sign-in.')}</p>
      </div>}
      <Button type="submit" variant="primary" className={styles.action} disabled={busy || (setup && !code.trim())}>{busy ? msg('Working…') : setup ? msg('Continue') : msg('Continue with {{provider}}', { provider: status.label })}</Button>
      {setup && <Button type="button" variant="quiet" className={styles.action} disabled={busy} onClick={() => setSetup(false)}>{msg('Back')}</Button>}
    </form> : <Button variant="primary" className={styles.action} onClick={() => setSetup(true)}>{msg('Set up sign-in')}</Button>)}
    {!status && error && <Button className={styles.action} onClick={() => window.location.reload()}>{msg('Try again')}</Button>}
  </>
}
