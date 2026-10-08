import { FileRequestError } from '../files/client'
import { GatewayError, OverBudget } from '../research/gatewayClient'

// Only these authored descriptions reach chat and saved Work details. Never
// echo provider bodies, queries, credentials, or arbitrary adapter stderr.
const FAILURES = {
 'search-timeout': 'The search provider took too long to respond. Try a shorter query within the remaining search budget.',
 'search-not-grounded': 'The provider returned no usable grounded sources. Try a shorter, more specific query within the remaining search budget.',
 'provider-unavailable': 'The search provider could not complete this request. A retry is allowed within the remaining search budget.',
 'credentials-required': 'The search provider rejected authentication or access. Check this connection in Admin > Connections > Web search.',
 'search-budget-exhausted': 'This connection has reached its daily search limit. Wait for the limit to reset or update it in Web search settings.',
 'rate-limited': 'The search provider is limiting requests. Try again later.',
 'source-changed': 'The search connection changed. Start a new message to use its current settings.',
 'connect-required': 'The selected search connection is no longer available. Select a connection in Web search settings.',
 'setup-required': 'The selected search connection needs configuration. Check Web search settings.',
 'blocked-by-policy': 'Web search is disabled by the connection policy.',
 'invalid-request': 'Gateway rejected the search request as invalid.',
 'gateway-unavailable': 'Desk could not reach the search Gateway. Check local processing in Admin > Document processing.',
 'gateway-refused': 'Gateway refused this search request. The refusal did not identify a supported cause; check the connection test and Gateway diagnostics.',
 'search-over-limit': 'The search response exceeded Desk’s size limit. Try a narrower query.',
 'search-storage-failed': 'The search response could not be saved in this Desk. Check local storage availability and retry.',
 'search-proof-failed': 'The search response arrived, but its acquisition record could not be completed. No links were authorized.',
 'search-verification-failed': 'Search results could not be verified. No links were authorized.',
 'search-canceled': 'The search was stopped.',
 'search-failed': 'Desk could not complete the search. No verified sources were retained; the cause is unknown.'
} as const
export type SearchFailureCode = keyof typeof FAILURES
export function searchFailureMessage(code: unknown): string | undefined {
 return typeof code === 'string' && Object.hasOwn(FAILURES, code) ? FAILURES[code as SearchFailureCode] : undefined
}
export class SearchFailure extends Error {
 constructor(readonly code: SearchFailureCode) { super(FAILURES[code]); this.name = 'SearchFailure' }
}
export function searchFailure(cause: unknown): SearchFailure {
 if (cause instanceof SearchFailure) return cause
 if ((cause as {name?:unknown} | null)?.name === 'AbortError') return new SearchFailure('search-canceled')
 if (cause instanceof OverBudget) return new SearchFailure('search-over-limit')
 if (cause instanceof FileRequestError) return new SearchFailure('search-storage-failed')
 if (cause instanceof GatewayError) {
  // The adapter's protocol token is carried by Gateway as bounded stderr.
  const token = /^(?:source failed: )?([a-z-]+)$/.exec(cause.message)?.[1]
  if (token && ['search-timeout','search-not-grounded','provider-unavailable','credentials-required','search-budget-exhausted','rate-limited','source-changed','connect-required','setup-required','blocked-by-policy','invalid-request'].includes(token)) return new SearchFailure(token as SearchFailureCode)
  if (cause.status >= 502 && cause.status <= 504) return new SearchFailure('gateway-unavailable')
  return new SearchFailure('gateway-refused')
 }
 return new SearchFailure('search-failed')
}
