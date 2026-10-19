import { useEffect, useRef, useState, type ReactNode } from 'react'
import { msg, useLocale } from '../i18n'
import { answer, deskFetch } from '../files/client'
import { Button } from '../ui/Button'
import { Notice } from '../ui/Notice'
import type { Run } from './client'
import styles from './JobsView.module.css'

export function signingFailureMessage(reason:string) {
 switch(reason){
  case "The record's signature sidecar could not be read.": return msg('The decision was saved, but its signature could not be read.')
  case "The record's signature sidecar names a key that is not the expected one.": return msg('The decision was saved, but its signature uses an unexpected key.')
  case "The record's signature sidecar does not verify under the expected key.": return msg('The decision was saved, but its signature could not be verified.')
  case "The Runtime signed nothing: the key at the signing key's path is not the expected key.": return msg('The decision was saved without a signature because the signing key changed.')
  case "The Runtime signed nothing, and the key at the signing key's path could not be read as a key.": return msg('The decision was saved without a signature because the signing key could not be read.')
  case "The record carries no signature.": return msg('The decision was saved without a signature.')
  default:return msg('The decision was saved, but signing failed.')
 }
}
export function RunSigningNotice({run}:{run:Run}){
 useLocale()
 if(run.signing?.status!=='failed')return null
 return <Notice tone="warning" detailLabel={msg('View details')} detail={signingFailureMessage(run.signing.reason)}>{msg('Signing failed')}</Notice>
}
interface SignatureCheck {
  status: 'verified' | 'unsigned' | 'invalid' | 'unavailable'
  references?: 'verified' | 'unavailable' | 'invalid' | 'check-unavailable'
  checkedAt: string
  recordDigest?: string
  publicKey?: string
}

function referenceMessage(status: SignatureCheck['references']) {
  switch (status) {
    case 'verified': return msg('Verified against saved references')
    case 'invalid': return msg('Release or inputs did not match saved references')
    case 'check-unavailable': return msg('The release and input checks could not be completed')
    default: return msg('Not checked — saved references unavailable')
  }
}

export function VerifySavedRecord({ runId, actions }: { runId: string; actions?: ReactNode }) {
  useLocale()
  const [busy, setBusy] = useState(false)
  const [result, setResult] = useState<SignatureCheck>()
  const [error, setError] = useState(false)
  const request = useRef<AbortController | null>(null)
  useEffect(() => {
    setResult(undefined); setError(false); setBusy(false)
    return () => { request.current?.abort(); request.current = null }
  }, [runId])
  const verify = async () => {
    if (busy) return
    const controller = new AbortController()
    request.current = controller
    setBusy(true); setError(false); setResult(undefined)
    try {
      const checked = await answer<SignatureCheck>(await deskFetch(`/api/audit/runs/${runId}/signature`, { method: 'POST', signal: controller.signal }))
      if (!controller.signal.aborted) setResult(checked)
    } catch {
      if (!controller.signal.aborted) setError(true)
    } finally {
      if (!controller.signal.aborted) setBusy(false)
    }
  }
  const badReferences = result?.references === 'invalid' || result?.references === 'check-unavailable'
  return <div className={styles.field}>
    <div className={styles.actions}>
      <Button disabled={busy} onClick={() => void verify()}>{busy ? msg('Verifying…') : msg('Verify saved record')}</Button>
      {actions}
    </div>
    {(result || error) && <Notice tone={result?.status === 'verified' && !badReferences ? 'success' : 'warning'} detailLabel={msg('View details')} detail={<>
      <dl className={styles.properties}>
        <div><dt>{msg('Record signature')}</dt><dd>{result?.status === 'verified' ? msg('Verified') : result?.status === 'unsigned' ? msg('The saved record has no signature.') : result?.status === 'invalid' ? msg('Signature not verified') : msg('The signature could not be checked.')}</dd></div>
        <div><dt>{msg('Release and inputs')}</dt><dd>{referenceMessage(error ? 'check-unavailable' : result?.references)}</dd></div>
        <div><dt>{msg('Decision outcome')}</dt><dd>{msg('Not re-run')}</dd></div>
      </dl>
      <p>{msg('These checks use references saved by Desk. They do not establish source accuracy or policy correctness, and do not re-run the decision.')}</p>
      {result?.recordDigest && <code>{result.recordDigest}</code>}
    </>}>
      {badReferences ? msg('Record checks did not pass') : result?.status === 'verified' ? msg('Signature verified · Checked just now') : result?.status === 'unsigned' ? msg('The saved record has no signature.') : result?.status === 'unavailable' || error ? msg('The signature could not be checked.') : msg('Signature not verified')}
    </Notice>}
  </div>
}
