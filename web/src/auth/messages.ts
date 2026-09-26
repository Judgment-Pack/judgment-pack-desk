import { msg } from '../i18n'
import { SignInError } from './flow'

export function signInMessage(error: unknown): string {
  const code = error instanceof SignInError ? error.code : typeof error === 'string' ? error : ''
  switch (code) {
    case 'cancelled': return msg('Sign-in was cancelled. You can try again.')
    case 'access-denied': return msg('This account is not the owner of this Desk. Sign in with the configured owner account.')
    case 'setup-code': return msg('The setup code is incorrect. Use the code shown by the running Desk process.')
    case 'browser-storage': return msg('Allow session storage in this browser to complete sign-in.')
    case 'invalid-settings': return msg('Check the issuer URL and client registration settings.')
    case 'provider-unavailable': return msg('The identity provider could not be reached or does not support this sign-in flow.')
    case 'test-required':
    case 'settings-changed': return msg('The sign-in settings changed or the test expired. Test sign-in again.')
    case 'session-ended': return msg('Your session ended. Sign in again.')
    case 'storage': return msg('The sign-in configuration could not be saved securely. Check this computer’s configuration directory.')
    case 'busy': return msg('Too many sign-in attempts. Wait a minute and try again.')
    case 'invalid-callback':
    case 'verification-failed': return msg('Sign-in could not be verified. Start again from this Desk tab.')
    default: return msg('Sign-in is unavailable. Check that Desk and your identity provider are running, then try again.')
  }
}
