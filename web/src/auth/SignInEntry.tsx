import { useEffect, useRef, useState } from 'react'
import { msg, useLocale } from '../i18n'
import { keepSetupSession } from '../mcp/session'
import { Button } from '../ui/Button'
import { Input } from '../ui/Input'
import { authCall, beginSignIn, signInProblem, type SignInStatus } from './flow'
import { signInMessage } from './messages'
import styles from './SessionPage.module.css'

export function SignInEntry() {
  useLocale()
  const heading = useRef<HTMLHeadingElement>(null)
  useEffect(() => { heading.current?.focus() }, [])
  const [status, setStatus] = useState<SignInStatus | null>(null)
  const [setup, setSetup] = useState(false)
  const [code, setCode] = useState('')
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
    setBusy(true); setError('')
    try {
      if (setup) {
        const reply = await authCall<{ id: string }>('setup', { code: code.trim() })
        keepSetupSession(reply.id)
        window.location.assign('/admin#identity-provider')
      } else {
        await beginSignIn('login', { returnPath: window.location.pathname + window.location.search + window.location.hash })
      }
    } catch (cause) { setError(cause instanceof Error ? ('code' in cause ? String(cause.code) : 'unavailable') : 'unavailable'); setBusy(false) }
  }
  const local = status?.localAccess === true && !status.enabled && !status.unavailable
  return <>
    <h1 ref={heading} tabIndex={-1}>{local ? msg('Open your local Desk') : setup || status?.enabled === false ? msg('Set up sign-in') : msg('Sign in to Unveil')}</h1>
    <p className={styles.lead}>{local ? msg('Personal · This computer') : status?.enabled ? msg('Sign in to open your local workspace. Signing in does not upload your packs or chats.') : msg('The installation owner needs to configure sign-in once on this computer.')}</p>
    {error && <p role="alert" className={styles.notice}>{signInMessage(error)}</p>}
    {status?.unavailable && <p role="alert">{msg('Sign-in is locked because its configuration cannot be read. Contact the installation owner.')}</p>}
    {!status && !error && <p role="status">{msg('Loading…')}</p>}
    {status && !status.unavailable && (local ? <Button variant="primary" className={styles.action} onClick={() => window.location.reload()}>{msg('Continue')}</Button> : status.enabled || setup ? <form onSubmit={event => { event.preventDefault(); void submit() }}>
      {setup && <div className={styles.setupCode}>
        <label htmlFor="sign-in-setup-code">{msg('Owner setup code')}</label>
        <Input id="sign-in-setup-code" type="password" autoComplete="off" value={code} onChange={event => setCode(event.target.value)} autoFocus />
        <p>{msg('Use the setup code shown in the terminal where Desk is running. This is only needed for initial setup.')}</p>
      </div>}
      <Button type="submit" variant="primary" className={styles.action} disabled={busy || (setup && !code.trim())}>{busy ? msg('Working…') : setup ? msg('Continue') : msg('Continue with {{provider}}', { provider: status.label })}</Button>
      {setup && <Button type="button" variant="quiet" className={styles.action} disabled={busy} onClick={() => setSetup(false)}>{msg('Back')}</Button>}
    </form> : <Button variant="primary" className={styles.action} onClick={() => setSetup(true)}>{msg('Set up sign-in')}</Button>)}
    {!status && error && <Button className={styles.action} onClick={() => window.location.reload()}>{msg('Try again')}</Button>}
  </>
}
