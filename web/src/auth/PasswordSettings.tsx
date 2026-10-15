import { useId, useRef, useState } from 'react'
import { msg, useLocale } from '../i18n'
import { forgetSession, sessionBearer } from '../mcp/session'
import { Button } from '../ui/Button'
import { Dialog } from '../ui/Dialog'
import { Disclosure } from '../ui/Disclosure'
import { Field, FieldGroup } from '../ui/Field'
import { PasswordInput } from '../ui/PasswordInput'
import { SettingRow } from '../ui/SettingRow'
import { SettingsActions, SettingsFeedback } from '../ui/SettingsFeedback'
import { authCall, SignInError, type SignInMode } from './flow'
import { signInMessage } from './messages'

export function PasswordSettings({ mode, storageAvailable }: { mode: SignInMode; storageAvailable: boolean }) {
  useLocale()
  const [open, setOpen] = useState(false)
  const [current, setCurrent] = useState('')
  const [password, setPassword] = useState('')
  const [confirm, setConfirm] = useState('')
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState<unknown>(null)
  const [validation, setValidation] = useState(false)
  const [uncertain, setUncertain] = useState(false)
  const pending = useRef(false)
  const opener = useRef<HTMLButtonElement>(null)
  const form = useId()
  const changing = mode === 'password'
  const title = changing ? msg('Change password') : msg('Set password')
  const lengthOK = Array.from(password).length >= 4 && Array.from(password).length <= 8
  const lengthError = validation && !uncertain && !lengthOK ? signInMessage('password-length') : undefined
  const mismatch = confirm !== password
  const currentError = error instanceof SignInError && error.code === 'password-current-incorrect'
  const needsSignIn = error instanceof SignInError && ['session-ended', 'password-mode-changed'].includes(error.code)
  function close(next: boolean) {
    if (pending.current) return
    setOpen(next); setCurrent(''); setPassword(''); setConfirm(''); setError(null); setValidation(false); setUncertain(false)
  }
  function returnToSignIn() { forgetSession(); window.location.assign('/') }
  async function save() {
    if (pending.current || uncertain || needsSignIn) return
    setValidation(true)
    if (!lengthOK || mismatch || (changing && !current)) return
    pending.current = true; setBusy(true); setError(null)
    try {
      const result = await authCall<{ saved: boolean }>(`password/${changing ? 'change' : 'set'}`, { currentPassword: current, newPassword: password }, await sessionBearer())
      if (result.saved !== true) throw new Error('unconfirmed')
      setCurrent(''); setPassword(''); setConfirm('')
      returnToSignIn()
    } catch (cause) {
      setError(cause)
      if (!(cause instanceof SignInError)) { setUncertain(true); setCurrent(''); setPassword(''); setConfirm('') }
    } finally { pending.current = false; setBusy(false) }
  }
  if (mode === 'provider') return null
  return <>
    <SettingRow title={msg('Desk password')} description={changing ? msg('A password is required to open any Desk on this computer.') : msg('Require a password to open any Desk on this computer.')} separator={false} action={<Button ref={opener} disabled={!storageAvailable} onClick={() => close(true)}>{title}</Button>} />
    {changing && <Disclosure title={msg('Recovery')}><p>{msg('Stop Desk and run jpack-desk --reset-sign-in on this computer, then restart and set up sign-in again. Packs, chats, and source connections are preserved.')}</p></Disclosure>}
    <Dialog open={open} onOpenChange={close} openerRef={opener} title={title} footer={<SettingsActions feedback={<SettingsFeedback tone={error && !currentError ? 'error' : 'neutral'}>{uncertain ? msg('Could not confirm whether the password was saved. Return to sign-in and try the new password first.') : error && !currentError ? signInMessage(error) : busy ? msg('Saving…') : null}</SettingsFeedback>}>
      <Button disabled={busy} onClick={() => close(false)}>{msg('Cancel')}</Button>
      {uncertain || needsSignIn ? <Button variant="primary" onClick={returnToSignIn}>{msg('Return to sign-in')}</Button> : <Button type="submit" form={form} variant="primary" disabled={busy || !password || !confirm || (changing && !current)}>{busy ? msg('Saving…') : title}</Button>}
    </SettingsActions>}>
      <form id={form} onSubmit={event => { event.preventDefault(); void save() }}>
        <FieldGroup>
          {changing && <Field label={msg('Current password')} error={currentError ? signInMessage(error) : undefined}>{wiring => <PasswordInput {...wiring} showLabel={msg('Show current password')} hideLabel={msg('Hide current password')} autoComplete="current-password" value={current} maxLength={256} required disabled={busy || uncertain} onChange={event => { setCurrent(event.target.value); if (currentError) setError(null) }} />}</Field>}
          <Field label={msg('New password')} hint={lengthError ? undefined : msg('Use 4 to 8 characters.')} error={lengthError}>{wiring => <PasswordInput {...wiring} showLabel={msg('Show new password')} hideLabel={msg('Hide new password')} autoComplete="new-password" value={password} maxLength={256} required disabled={busy || uncertain} onChange={event => setPassword(event.target.value)} />}</Field>
          <Field label={msg('Confirm new password')} error={validation && !uncertain && mismatch ? msg('Passwords do not match.') : undefined}>{wiring => <PasswordInput {...wiring} showLabel={msg('Show confirmation password')} hideLabel={msg('Hide confirmation password')} autoComplete="new-password" value={confirm} maxLength={256} required disabled={busy || uncertain} onChange={event => setConfirm(event.target.value)} />}</Field>
          <SettingsFeedback>{changing ? msg('Changing your password signs out all Desk sessions.') : msg('Setting a password signs out all Desk sessions. Sign in again with your new password.')}</SettingsFeedback>
        </FieldGroup>
      </form>
    </Dialog>
  </>
}
