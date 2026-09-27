import type { MappingV2, CopyMapping } from './mappingTypes'

/** Rule-derived targets remain advanced; a form must never silently overwrite them. */
export function getBinding(mapping: MappingV2, kind:'facts'|'evidence', target:string):{owner:string;path:string;complex:boolean}{
 const found:{owner:string;path:string;complex:boolean}[]=[]
 const read=(copy:CopyMapping|undefined,owner:string)=>{if(!copy)return;for(const row of copy[kind]){if(('target' in row?row.target:row.requirement)===target)found.push({owner,path:row.source,complex:false})}}
 read(mapping.case,'case')
 for(const source of mapping.sources??[]){read(source.read.copy,source.name);if(source.read.rule)found.push({owner:source.name,path:'',complex:true})}
 // Any derivation rule may emit this target. Preserve the advanced rule rather
 // than editing a partial copy view of it. Runner owns authoritative coverage.
 if(found.some(b=>b.complex)||found.length>1)return {owner:'advanced',path:'',complex:true}
 return found[0]??{owner:'omitted',path:'',complex:false}
}
export function setBinding(mapping:MappingV2,kind:'facts'|'evidence',target:string,owner:string,path:string):MappingV2{
 if(getBinding(mapping,kind,target).complex)throw Error('Advanced mappings must be edited as a whole.')
 const next=structuredClone(mapping)
 const remove=(copy:CopyMapping|undefined)=>{if(!copy)return;if(kind==='facts')copy.facts=copy.facts.filter(r=>r.target!==target);else copy.evidence=copy.evidence.filter(r=>r.requirement!==target)}
 remove(next.case);for(const s of next.sources??[])remove(s.read.copy)
 const omitted=kind==='facts'?'unmapped':'unmappedEvidence'
 next[omitted]=(next[omitted]??[]).filter(t=>t!==target)
 if(owner==='omitted'){next[omitted]!.push(target);return next}
 let copy:CopyMapping
 if(owner==='case'){next.case??={facts:[],evidence:[]};copy=next.case}else{const source=next.sources?.find(s=>s.name===owner);if(!source)throw Error('Unknown mapping source.');source.read.copy??={facts:[],evidence:[]};copy=source.read.copy}
 if(kind==='facts')copy.facts.push({target,source:path});else copy.evidence.push({requirement:target,source:path})
 return next
}
