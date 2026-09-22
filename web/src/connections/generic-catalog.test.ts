import { expect, it } from 'vitest'
import { parseConnectionCatalog } from './catalog'
import { genericCatalog } from '../testing/genericConnection'

it('admits a new provider by its protocols, without a provider-specific handler', () => {
 const raw=genericCatalog()
 expect(parseConnectionCatalog(raw).providers[0]?.id).toBe('fixture-files')
 raw.providers[0]!.id='another-store';raw.providers[0]!.source!.id='another-store'
 expect(parseConnectionCatalog(raw).providers[0]?.id).toBe('another-store')
})
it.each(['protocol','auth','selection','registration','queryMode'] as const)('keeps an unsupported %s visible without enabling it', key => {
 const raw=genericCatalog();Object.assign(raw.providers[0]!,{[key]:'future-protocol'})
 const parsed=parseConnectionCatalog(raw)
 expect(parsed.providers).toHaveLength(0);expect(parsed.unsupported?.[0]?.presentation?.name).toBe('Fixture files')
})
it.each(['javascript:alert(1)','http://example.com/login','https://user:secret@example.com/login','https://example.com/login?token=secret'])('rejects an unsafe authorization endpoint %s', endpoint=>{
 const raw=genericCatalog();raw.providers[0]!.authorizationEndpoints=[endpoint]
 expect(()=>parseConnectionCatalog(raw)).toThrow()
})
it('rejects malformed display data, remote icons and duplicate setup fields',()=>{
 const raw=genericCatalog();raw.providers[0]!.presentation!.icon='https://example.com/track.png'
 expect(()=>parseConnectionCatalog(raw)).toThrow()
 raw.providers[0]!.presentation!.icon='';raw.providers[0]!.setup!.push(raw.providers[0]!.setup![0]!)
 expect(()=>parseConnectionCatalog(raw)).toThrow()
})
it('does not confuse a new provider with a legacy source or a different acquisition source',()=>{
 const raw=genericCatalog();raw.providers[0]!.source!.record='note-v1'
 expect(parseConnectionCatalog(raw).providers).toEqual([])
 raw.providers[0]!.source!.record='resource-v1';raw.providers[0]!.source!.id='another-store'
 expect(parseConnectionCatalog(raw).providers).toEqual([])
})

it('keeps legacy providers on their explicit record and authentication contracts',()=>{
 const raw=genericCatalog();raw.providers[0]!.id='gmail';raw.providers[0]!.source!.id='gmail'
 expect(parseConnectionCatalog(raw).providers).toEqual([])
})
