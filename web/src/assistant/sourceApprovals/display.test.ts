import {expect,it} from 'vitest'
import {parseJsonText} from '../../research/verify/canon'
import {sourceReviewJSON} from './display'
import type {SourceReview} from './catalog'

it('uses shared wire formatting without rounding schema numbers or losing empty/missing values',()=>{
 const inputSchema=parseJsonText('{"type":"object","properties":{"n":{"minimum":1.0,"maximum":1e0,"default":9007199254740993,"multipleOf":-0}}}') as SourceReview['snapshot']['completeDeclarations'][number]['inputSchema']
 const row:SourceReview={status:'review-required',reviewId:'1'.repeat(32),reviewRevision:1,snapshot:{adapterId:'desk-search',adapterVersion:'1',kind:'custom',trust:'first-party',configurationDigest:'a'.repeat(64),toolListDigest:'b'.repeat(64),status:'review-required',identity:{kind:'connection',label:'<b>literal</b>'},identityDigest:'c'.repeat(64),
  configuration:{target:'https://example.invalid/',targetDigest:'',authorityDigest:'',publicKeyDigest:'',deskDigest:'a'.repeat(64),preferenceDigest:'',connection:'',revision:'',provider:'',bundleDigest:'',verifierDigest:'',gatewayBuildDigest:'',ownerDigest:'b'.repeat(64),catalogDigest:'',catalogOwnerDigest:''},
  completeDeclarations:[{name:'search_sources',description:'"exact"',inputSchema,taxonomy:{operation:'discover',effect:'read-external'},effectClass:'read',delegation:'research',approval:'none'}]}}
 const text=sourceReviewJSON(row)
 expect(text).toContain('"inputSchema":{"type":"object","properties":{"n":{"minimum":1.0,"maximum":1e0,"default":9007199254740993,"multipleOf":-0}}}')
 expect(text).toContain('"targetDigest":""');expect(text).not.toContain('"approval":{');expect(text).toContain('"description":"\\"exact\\""')
 const decoded=JSON.parse(text);expect(decoded.snapshot.configuration).toEqual(row.snapshot.configuration);expect(decoded.reviewId).toBe(row.reviewId)
})
