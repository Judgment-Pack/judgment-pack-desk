import {expect,it} from 'vitest'
import {readFileSync} from 'node:fs'
import {parseConnectionCatalog} from './catalog'

// Handwritten vectors shared in meaning with Go; no captured output is a fixture.
const read='{"operation":"read","effect":"read-external"}'
const act='{"operation":"act","effect":"act"}'
const run='{"operation":"run","effect":"act"}'
const source=`"taxonomy":${read},"profile":{"effect":"read-external","operations":["read"]},"capabilityTaxonomies":{},"dispatchTaxonomies":{"read":${read}}`
const fixture=`{"version":5,"discoveryTaxonomy":{"operation":"discover","effect":"pure"},"sources":[{"id":"web","input":"url","mediaTypes":["text/html"],"maxBytes":4194304,"adapter":"adapter-web","capabilities":["page"],"taxonomy":${read},"profile":{"effect":"read-external","operations":["read"]},"capabilityTaxonomies":{"page":${read}},"dispatchTaxonomies":{"page":${read}}}],"providers":[{"id":"obsidian","auth":"local-folder","registration":"none","selection":"source-search","queryRequired":false,"operations":["status"],"protocol":"connection-v1","queryMode":"text","presentation":{"icon":"","name":"Notes","description":{"en":"Notes"},"instructions":{"en":"Choose notes"}},"setup":[],"authorizationEndpoints":[],"source":{"id":"obsidian","shape":"command","record":"note-v1","capabilities":[],${source}},"taxonomy":${run},"profile":{"effect":"act","operations":["read","act"]},"operationTaxonomies":{"status":${run}},"operationDeclarations":{"status":{"taxonomy":${run},"profile":{"effect":"act","operations":["read","act"]},"components":{"operation":{"operation":"read","effect":"read"},"operator-policy":${act}}}}}]}`
it('preserves complete owner facts without turning unsupported methods into actions',()=>{
 const result=parseConnectionCatalog(fixture)
 expect(result.providers).toEqual([])
 expect(result.unsupported?.[0]?.operationDeclarations?.status?.components).toEqual({operation:{operation:'read',effect:'read'},'operator-policy':{operation:'act',effect:'act'}})
 expect(result.researchSources?.[0]?.dispatchTaxonomies).toEqual({read:{operation:'read',effect:'read-external'}})
 expect(result.discoveryTaxonomy).toEqual({operation:'discover',effect:'pure'})
})
const mutations:[string,string,string][]=[
 ['missing discovery','"discoveryTaxonomy":{"operation":"discover","effect":"pure"},',''],
 ['wrong discovery','"operation":"discover","effect":"pure"','"operation":"discover","effect":"read"'],
 ['missing profile','"profile":{"effect":"read-external","operations":["read"]},',''],
 ['null profile','"profile":{"effect":"read-external","operations":["read"]}','"profile":null'],
 ['missing declarations','"operationDeclarations":','"otherDeclarations":'],
 ['extra declaration','"operationDeclarations":{"status":','"operationDeclarations":{"configure":{},"status":'],
 ['empty dispatch',`"dispatchTaxonomies":{"page":${read}}`,'"dispatchTaxonomies":{}'],
 ['null dispatch',`"dispatchTaxonomies":{"page":${read}}`,'"dispatchTaxonomies":null'],
 ['missing capability',`"capabilityTaxonomies":{"page":${read}}`,'"capabilityTaxonomies":{}'],
 ['extra capability','"capabilityTaxonomies":{}',`"capabilityTaxonomies":{"page":${read}}`],
 ['unknown selector','"dispatchTaxonomies":{"read":','"dispatchTaxonomies":{"other":'],
 ['duplicate selector','"dispatchTaxonomies":{"page":',`"dispatchTaxonomies":{"page":${read},"page":`],
 ['duplicate taxonomy',`"taxonomy":${read}`,`"taxonomy":${read},"taxonomy":${read}`],
 ['duplicate taxonomy field','"operation":"read","effect":"read-external"','"operation":"read","operation":"read","effect":"read-external"'],
 ['case folded field','"operation":"read","effect":"read-external"','"Operation":"read","effect":"read-external"'],
 ['taxonomy extension','"operation":"read","effect":"read-external"','"operation":"read","effect":"read-external","approval":true'],
 ['wrong effect','"operation":"read","effect":"read-external"','"operation":"read","effect":"act"'],
 ['check kind noncheck','"operation":"read","effect":"read-external"','"operation":"read","effect":"read-external","checkKind":"test"'],
 ['explicit null operation','"operation":"read","effect":"read-external"','"operation":null,"effect":"read-external"'],
 ['invalid experimental','"operation":"read","effect":"read-external"','"operation":"read","effect":"read-external","experimental":null'],
 ['experimental contradiction','"operation":"read","effect":"read-external"','"operation":"read","effect":"read-external","experimental":true'],
 ['profile order','"operations":["read","act"]','"operations":["act","read"]'],
 ['profile duplicate','"operations":["read","act"]','"operations":["read","read","act"]'],
 ['missing operator policy',`"operator-policy":${act}`,`"other":${act}`],
 ['weaker policy',`"operator-policy":${act}`,'"operator-policy":{"operation":"read","effect":"read"}'],
 ['intrinsic contradiction','"operation":{"operation":"read","effect":"read"}','"operation":{"operation":"discover","effect":"read"}'],
 ['empty operations','"operations":["status"]','"operations":[]'],
]
it.each(mutations)('refuses %s',(_name,before,after)=>{expect(fixture).toContain(before);expect(()=>parseConnectionCatalog(fixture.replace(before,after))).toThrow()})
it('bounds catalog byte and row counts',()=>{
 expect(()=>parseConnectionCatalog(' '.repeat(128<<10)+fixture)).toThrow()
 const raw=JSON.parse(fixture);raw.providers=Array.from({length:33},()=>raw.providers[0]);expect(()=>parseConnectionCatalog(raw)).toThrow()
})
it('retains historical absence of owner metadata',()=>{
 const result=parseConnectionCatalog({version:2,sources:[],providers:[]})
 expect(result).not.toHaveProperty('discoveryTaxonomy')
 expect(result).not.toHaveProperty('researchSources')
})
it.runIf(Boolean(process.env.DESK_GATEWAY_CATALOG5_OBSERVATION))('reads the explicit verified Gateway observation through the browser reader',()=>{
 const result=parseConnectionCatalog(readFileSync(process.env.DESK_GATEWAY_CATALOG5_OBSERVATION!,'utf8'))
 expect(result.providers.map(p=>p.id)).toContain('google-drive')
 expect(result.researchSources?.find(s=>s.id==='azure-devops')?.dispatchTaxonomies?.['pipelines.run']).toEqual({operation:'read',effect:'read-external'})
 expect(result.researchSources?.find(s=>s.id==='databricks')?.profile).toEqual({effect:'act',operations:['discover','act','run']})
})
