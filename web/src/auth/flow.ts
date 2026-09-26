/** OIDC redirects keep their completion proof in this tab. Provider tokens and
 * registration secrets never enter this storage. This module has no session
 * dependency so the one bootstrap can finish a redirect before mounting Desk. */
export type SignInStatus = { enabled: boolean; label: string; unavailable: boolean; localAccess?: boolean }
export type SignInProvider = { label: string; issuer: string; clientId: string; clientSecret?: string }
export type SignInOwner = { subject: string; issuer: string; name: string; email?: string }
export type SignInSettings = {
  enabled: boolean; callbackUrl: string; storageAvailable: boolean;
  provider?: SignInProvider; owner?: SignInOwner; hasSecret?: boolean;
  testedOwner?: SignInOwner; testId?: string
}
const ATTEMPT_KEY = 'jpack-desk.sign-in-attempt.v1'
let problem = ''
export const signInProblem = () => problem

export class SignInError extends Error {
  constructor(readonly code: string) { super(code) }
}
export async function authCall<T>(path: string, body?: unknown, bearer?: string, signal?: AbortSignal): Promise<T> {
  const headers: Record<string, string> = {}
  if (body !== undefined) headers['Content-Type'] = 'application/json'
  if (bearer) headers.Authorization = `Bearer ${bearer}`
  const response = await fetch(`/api/auth/${path}`, {
    method: body === undefined ? 'GET' : 'POST', headers,
    ...(body === undefined ? {} : { body: JSON.stringify(body) }),
    credentials: 'omit', redirect: 'error', signal: signal ? AbortSignal.any([signal, AbortSignal.timeout(20_000)]) : AbortSignal.timeout(20_000)
  })
  const value = await response.json()
  if (!response.ok) throw new SignInError(typeof value?.error === 'string' ? value.error : 'unavailable')
  return value as T
}
export async function beginSignIn(kind: 'login' | 'test', body: unknown, bearer?: string) {
  const reply = await authCall<{ attemptId: string; proof: string; authorizationUrl: string }>(kind === 'test' ? 'test' : 'start', body, bearer)
  const target = new URL(reply.authorizationUrl)
  if (typeof reply.attemptId !== 'string' || !/^[a-f0-9]{48}$/.test(reply.attemptId) ||
      typeof reply.proof !== 'string' || !/^[a-f0-9]{48}$/.test(reply.proof) ||
      (target.protocol !== 'https:' && !(target.protocol === 'http:' && ['127.0.0.1', '[::1]'].includes(target.hostname)))) {
    throw new SignInError('unavailable')
  }
  try { sessionStorage.setItem(ATTEMPT_KEY, JSON.stringify({ id: reply.attemptId, proof: reply.proof, kind, at: Date.now() })) }
  catch { throw new SignInError('browser-storage') }
  window.location.assign(target.href)
}
function replacePath(path: string) {
  const target = new URL(path, window.location.origin)
  if (!path.startsWith('/') || path.startsWith('//') || path.includes('\\') || target.origin !== window.location.origin) throw new SignInError('invalid-callback')
  window.history.replaceState(null, '', path)
  window.dispatchEvent(new PopStateEvent('popstate'))
}
export async function finishSignIn(stored: string | null): Promise<{ handled: boolean; id: string | null }> {
  if (window.location.pathname !== '/auth/return') return { handled: false, id: stored }
  const id = new URLSearchParams(window.location.hash.slice(1)).get('attempt')
  let kind = 'login'
  try {
    const attempt = JSON.parse(sessionStorage.getItem(ATTEMPT_KEY) ?? 'null')
    sessionStorage.removeItem(ATTEMPT_KEY)
    replacePath('/')
    if (!attempt || attempt.id !== id || !['test', 'login'].includes(attempt.kind) || !Number.isFinite(attempt.at) || attempt.at > Date.now() || Date.now() - attempt.at > 5 * 60_000 || typeof attempt.proof !== 'string' || !/^[a-f0-9]{48}$/.test(attempt.proof)) throw new SignInError('invalid-callback')
    kind = attempt.kind
    const result = await authCall<{ kind: string; id?: string; returnPath: string }>('complete', { attemptId: id, proof: attempt.proof }, kind === 'test' ? stored ?? undefined : undefined)
    if (result.kind !== kind || (kind === 'login' && (typeof result.id !== 'string' || !/^[a-f0-9]{48}$/.test(result.id)))) throw new SignInError('invalid-callback')
    problem = ''
    replacePath(result.returnPath)
    return { handled: true, id: kind === 'test' ? stored : result.id! }
  } catch (error) {
    problem = error instanceof SignInError ? error.code : 'unavailable'
    replacePath(kind === 'test' ? '/admin#identity-provider' : '/')
    return { handled: true, id: kind === 'test' ? stored : null }
  }
}
