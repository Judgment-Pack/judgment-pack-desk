import type {BindingDocument,DialectSourceV3,InputProfile,ProfileBinding} from './types'
/** Local configuration can assign installation pins only. Runner remains the
 * authority for catalog matching, model/capabilities and bound canonical bytes. */
export function importBindings(value:unknown):BindingDocument {
 const v=value as BindingDocument|null
 if(!v||typeof v!=='object'||Array.isArray(v)||Object.keys(v).some(k=>!['dialect','sources'].includes(k))||typeof v.dialect!=='string'||!/^sha256:[a-f0-9]{64}$/.test(v.dialect)||!v.sources||typeof v.sources!=='object'||Array.isArray(v.sources)||Object.keys(v.sources).length>16)throw Error('Invalid local dialect bindings')
 const pin=(p:ProfileBinding)=>p&&typeof p.profile==='string'&&!!p.profile&&p.profile.length<=256&&typeof p.profileDigest==='string'&&/^sha256:[a-f0-9]{64}$/.test(p.profileDigest)
 for(const p of Object.values(v.sources)){
  if(!pin(p)||Object.keys(p).some(k=>!['profile','profileDigest','webProfiles'].includes(k)))throw Error('Invalid local dialect binding')
  if(p.webProfiles!==undefined&&(typeof p.webProfiles!=='object'||!p.webProfiles||Array.isArray(p.webProfiles)||Object.entries(p.webProfiles).some(([k,v])=>!['search','page'].includes(k)||!pin(v)||Object.keys(v).some(k=>!['profile','profileDigest'].includes(k)))))throw Error('Invalid local web binding')
 }
 return v
}
export function sourcePin(source:DialectSourceV3,binding?:BindingDocument):ProfileBinding|undefined {
 return binding?.sources[source.name]??(source.profile&&source.profileDigest?{profile:source.profile,profileDigest:source.profileDigest}:undefined)
}
export function profileMatches(source:DialectSourceV3,profile:InputProfile,capability?:'answer'|'search'|'page'):boolean {
 if(source.kind==='research')return profile.class==='generated'&&!!profile.research?.capabilities.includes(capability??'answer')&&((capability??'answer')!=='answer'||profile.research.model===source.model)
 if(source.kind==='operation'&&source.platform)return source.shape===profile.shape&&source.record===profile.adapter.name
 return source.profile===profile.id
}
