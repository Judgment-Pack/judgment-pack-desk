import {expect,it} from 'vitest'
import {importBindings,profileMatches} from './bindings'
import type {ResearchSource,InputProfile} from './types'
const digest='sha256:'+'a'.repeat(64)
const source:ResearchSource={name:'dataset',kind:'research',maxAge:300,model:'model-a',question:'Classify this dataset',constraints:{},evidence:[],fields:[{name:'classification',type:'text',required:true}],capabilities:['answer','search'],read:{copy:{facts:[],evidence:[]}}}
const profile:InputProfile={id:'ai',publicKey:'key',class:'generated',source:'ai',authority:'local',shape:'http',adapter:{name:'search',version:'1',digest},endpoint:'https://provider.test',research:{capabilities:['answer','search'],model:'model-a'}}
it('keeps definition/model choices separate from local profile assignments',()=>{
 expect(profileMatches(source,profile)).toBe(true)
 expect(profileMatches(source,{...profile,research:{...profile.research!,model:'model-b'}})).toBe(false)
 expect(profileMatches(source,{...profile,class:'record'})).toBe(false)
 expect(profileMatches(source,profile,'page')).toBe(false)
})
it('permits only exact profile assignments and refuses recipe overrides',()=>{
 const value={dialect:digest,sources:{dataset:{profile:'ai',profileDigest:digest,webProfiles:{search:{profile:'search',profileDigest:digest}}}}}
 expect(importBindings(value)).toEqual(value)
 expect(()=>importBindings({...value,model:'replacement'})).toThrow()
 expect(()=>importBindings({...value,sources:{dataset:{...value.sources.dataset,question:'different'}}})).toThrow()
 expect(()=>importBindings({...value,sources:{dataset:{...value.sources.dataset,webProfiles:{page:{profile:'page',profileDigest:'stale'}}}}})).toThrow()
})
