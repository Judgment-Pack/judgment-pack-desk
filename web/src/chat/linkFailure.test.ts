import { expect, it } from 'vitest'
import { linkFailure, linkFailureMessage } from './linkFailure'
it('classifies only known host codes and never exposes arbitrary errors',()=>{
 expect(linkFailure(new Error('source failed: web-redirect-origin'))).toBe('web-redirect-origin')
 expect(linkFailure(new Error('private-secret-key'))).toBe('unknown')
 expect(linkFailureMessage(linkFailure(new Error('web-unknown-provider-message')))).not.toContain('web-unknown')
})

import { GatewayError } from '../research/gatewayClient'
import { configurableRedirect } from './linkFailure'
const refusal=(policy:unknown='google-grounding',to='https://publisher.com')=>'source failed: web-redirect-origin '+JSON.stringify({redirect:{from:'https://vertexaisearch.cloud.google.com',to,policy,permitted:false}})
it('offers only complete known redirect policy metadata from a Gateway error',()=>{
 expect(configurableRedirect(new GatewayError(502,refusal()))).toBe('google-grounding')
 for(const error of [new Error(refusal()),new GatewayError(502,'source failed: web-redirect-origin'),new GatewayError(502,refusal(null)),new GatewayError(502,refusal('anything')),new GatewayError(502,refusal('google-grounding','https://publisher.com/?secret=x')),new GatewayError(502,refusal()+'...'),new GatewayError(502,refusal(),'blocked-by-policy')])expect(configurableRedirect(error)).toBeUndefined()
})
