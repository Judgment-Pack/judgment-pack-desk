import { useQueryClient } from '@tanstack/react-query'
import { useEffect, useState, type ReactNode } from 'react'
import { NoSession, bootstrapUnavailable, discardBody, forgetSession, sessionBearer, sessionEnded, whenSessionEnds } from '../mcp/session'
import { VerifiedSessionProvider, type VerifiedSession } from './VerifiedSession'
import { SessionPage, type AccessState } from './SessionPage'

/** UI boundary only. Every backend operation still enforces its own guard.
 * Mount no workspace/config/chat providers before the server confirms access. */
export function SessionGate({ children }: { children: ReactNode }) {
  const client = useQueryClient()
  const [state, setState] = useState<AccessState>('checking')
  const [detail, setDetail] = useState('')
  const [verified, setVerified] = useState<VerifiedSession | null>(null)
  useEffect(() => {
    let active = true
    const abort = new AbortController()
    const timer = setTimeout(() => abort.abort(), 12_000)
    const unsubscribe = whenSessionEnds(() => {
      if (!active) return
      setState('recovery')
      setVerified(null)
      setDetail(sessionEnded() ?? '')
      // Cancel before clearing so late query results cannot repopulate the
      // previous session's cache. Chat/runtime providers unmount below.
      void client.cancelQueries()
      client.clear()
    })
    void (async () => {
      try {
        const bearer = await sessionBearer()
        if (!active) return
        const response = await fetch('/api/session', {
          headers: { Authorization: `Bearer ${bearer}` }, credentials: 'omit',
          redirect: 'error', signal: abort.signal
        })
        if (!response.ok) {
          await discardBody(response)
          if (!active) return
          if (response.status === 401) { forgetSession(); return }
          setState('unavailable'); return
        }
        const record: unknown = await response.json()
        if (!active) return
        if (!record || typeof record !== 'object' || !('subject' in record) || typeof record.subject !== 'string' || !record.subject || !('issuer' in record) || (record.issuer !== null && typeof record.issuer !== 'string') || ('name' in record && typeof record.name !== 'string') || ('email' in record && typeof record.email !== 'string')) {
          setState('unavailable'); return
        }
        setVerified(record as VerifiedSession)
        // A concurrent refusal/sign-out wins over an older successful read.
        setState(sessionEnded() === null ? 'ready' : 'recovery')
      } catch (error) {
        if (!active) return
        setState(error instanceof NoSession && !bootstrapUnavailable() ? 'recovery' : 'unavailable')
        setDetail(error instanceof NoSession ? sessionEnded() ?? '' : '')
      } finally { clearTimeout(timer) }
    })()
    return () => { active = false; clearTimeout(timer); abort.abort(); unsubscribe() }
  }, [client])
  return state === 'ready' && verified ? <VerifiedSessionProvider value={verified}>{children}</VerifiedSessionProvider> : <SessionPage state={state} detail={detail} />
}
