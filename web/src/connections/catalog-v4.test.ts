import {expect,it} from 'vitest'
import {parseConnectionCatalog} from './catalog'
import {genericCatalog} from '../testing/genericConnection'
function fixture(){
 const raw=genericCatalog()
 return {...raw,version:4,providers:raw.providers.map(p=>({...p,source:{...p.source!,shape:'http',adapter:'adapter-sources',capabilities:['answer','search']}})),sources:[{id:'web',input:'url',maxBytes:4194304,mediaTypes:['text/html','text/plain','application/pdf'],adapter:'adapter-web',capabilities:['page']}]}
}
it('retains program identity separately from the result contract and exposes declared capabilities',()=>{
 const result=parseConnectionCatalog(fixture())
 expect(result.providers[0]?.source).toMatchObject({record:'resource-v1',adapter:'adapter-sources'})
 expect(result.researchSources).toHaveLength(2)
 expect(result.researchSources?.[1]).toMatchObject({id:'web',adapter:'adapter-web',capabilities:['page']})
 expect(result.web).toBe(true)
})
it.each([undefined,null,['crawl'],['search','answer'],['answer','answer']])('refuses malformed capability declaration %j',capabilities=>{
 const raw=fixture();Object.assign(raw.providers[0]!.source,{capabilities})
 expect(()=>parseConnectionCatalog(raw)).toThrow()
})
it('does not let commands declare a portable adapter or let HTTP omit it',()=>{
 const raw=fixture();raw.providers[0]!.source.shape='command'
 expect(()=>parseConnectionCatalog(raw)).toThrow()
 const source=raw.providers[0]!.source as {adapter?:string;shape:string};delete source.adapter
 expect(parseConnectionCatalog(raw).researchSources).toHaveLength(2)
 source.shape='http';expect(()=>parseConnectionCatalog(raw)).toThrow()
})
it('rejects malformed explicit source capabilities as well',()=>{
 const raw=fixture();raw.sources[0]!.capabilities=['page','page'];expect(()=>parseConnectionCatalog(raw)).toThrow()
})

// Exact output of Gateway 0ff377ab, shared with the server admission test.
import gatewayCatalog from '../testing/gatewayCatalogV4.json'
it('reads the actual pinned Gateway catalog without conflating receipt program and result contract',()=>{
 const result=parseConnectionCatalog(gatewayCatalog)
 expect(result.providers.map(p=>p.id)).toContain('google-drive')
 expect(result.researchSources?.find(s=>s.id==='web')).toMatchObject({adapter:'adapter-web',capabilities:['page']})
 expect(result.researchSources?.find(s=>s.id==='web-search')).toMatchObject({adapter:'adapter-sources',capabilities:['answer','search']})
})
