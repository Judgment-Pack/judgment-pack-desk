import { msg } from '../i18n'
import { GatewayError } from '../research/gatewayClient'

// Only host-defined codes are persisted. Never save arbitrary provider errors,
// URL query strings, page content, or credentials as a failure explanation.
const codes = ['web-redirect-origin', 'web-outside-site', 'web-public-only', 'web-unavailable', 'web-over-limit', 'web-unsupported-type', 'web-redirect-limit', 'unknown'] as const
export type LinkFailureCode = typeof codes[number]
export function validLinkFailure(value: unknown): value is LinkFailureCode {
 return typeof value === 'string' && (codes as readonly string[]).includes(value)
}
export function linkFailure(cause: unknown): LinkFailureCode {
 const message = cause instanceof Error ? cause.message : String(cause)
 const code = /\bweb-[a-z-]+\b/.exec(message)?.[0]
 return validLinkFailure(code) ? code : 'unknown'
}
export function linkFailureMessage(code: LinkFailureCode): string {
 switch(code) {
  case 'web-redirect-origin': return msg('The link redirects to another website, which this reader does not allow.')
  case 'web-outside-site': return msg('The page is outside the website selected for exploration.')
  case 'web-public-only': return msg('The link points to a private or local network address. Only public websites can be read.')
  case 'web-unavailable': return msg('The website could not be reached or did not return a readable page.')
  case 'web-over-limit': return msg('The page exceeds the 4 MiB reading limit.')
  case 'web-unsupported-type': return msg('The website returned a format or encoding this reader does not support.')
  case 'web-redirect-limit': return msg('The link redirects too many times.')
  default: return msg('Could not read this link. Use a public HTTPS page, PDF, or text file under 4 MiB.')
 }
}

/** Only the gateway's complete typed refusal can suggest this opt-in. Store
 * the known policy identifier, never origins, paths, tokens, or arbitrary text. */
export function configurableRedirect(cause:unknown):'google-grounding'|undefined {
 if(!(cause instanceof GatewayError)||cause.code!==undefined)return
 const match=/^(?:source failed: )?web-redirect-origin (\{[^\n]+\})$/.exec(cause.message.trim())
 if(!match||new TextEncoder().encode(match[0].replace(/^source failed: /,'')).length>200)return
 try {
  const value=JSON.parse(match[1]!)?.redirect
  if(value?.policy!=='google-grounding'||value.permitted!==false||value.from!=='https://vertexaisearch.cloud.google.com'||typeof value.to!=='string')return
  const target=new URL(value.to)
  if(target.protocol!=='https:'||target.origin!==value.to||target.username||target.password||target.port)return
  return 'google-grounding'
 }catch{return}
}
