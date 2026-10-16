import type { ExecutableDialect, DialectCopies } from './dialectTypes'

/** Rule-derived targets remain advanced; a form must never silently overwrite them. */
export function getBinding(mapping: ExecutableDialect, kind:'facts'|'evidence', target:string):{owner:string;path:string;complex:boolean}{
 const found:{owner:string;path:string;complex:boolean}[]=[]
 const read=(copy:DialectCopies|undefined,owner:string)=>{if(!copy)return;for(const row of copy[kind]??[]){if(('target' in row?row.target:row.requirement)===target)found.push({owner,path:row.source,complex:false})}}
 read(mapping.case,'case')
 for(const source of mapping.sources??[]){read(source.read.copy,source.name);if(source.read.rule)found.push({owner:source.name,path:'',complex:true})}
 // Any derivation rule may emit this target. Preserve the advanced rule rather
 // than editing a partial copy view of it. Runner owns authoritative coverage.
 if(found.some(b=>b.complex)||found.length>1)return {owner:'advanced',path:'',complex:true}
 return found[0]??{owner:'omitted',path:'',complex:false}
}
export function setBinding<T extends ExecutableDialect>(mapping:T,kind:'facts'|'evidence',target:string,owner:string,path:string):T{
 if(getBinding(mapping,kind,target).complex)throw Error('Advanced dialects must be edited as a whole.')
 const next=structuredClone(mapping)
 const remove=(copy:DialectCopies|undefined)=>{if(!copy)return;if(kind==='facts'&&copy.facts)copy.facts=copy.facts.filter(r=>r.target!==target);else if(kind==='evidence'&&copy.evidence)copy.evidence=copy.evidence.filter(r=>r.requirement!==target)}
 remove(next.case);for(const s of next.sources??[])remove(s.read.copy)
 const omitted=kind==='facts'?'unmapped':'unmappedEvidence'
 next[omitted]=(next[omitted]??[]).filter(t=>t!==target)
 if(owner==='omitted'){next[omitted]!.push(target);return finishDialectEdit(next)}
 let copy:DialectCopies
 if(owner==='case'){next.case??={facts:[],evidence:[]};copy=next.case}else{const source=next.sources?.find(s=>s.name===owner);if(!source)throw Error('Unknown dialect source.');source.read.copy??={facts:[],evidence:[]};copy=source.read.copy}
 if(kind==='facts')(copy.facts??=[]).push({target,source:path});else (copy.evidence??=[]).push({requirement:target,source:path})
 return finishDialectEdit(next)
}

/** Apply strict omission spelling only to a document edited by our forms.
 * Imported/published JSON is validated unchanged; this is not an input repair.
 * Required empty copy arrays and request/calculation objects retain their meaning. */
export function finishDialectEdit<T extends ExecutableDialect>(mapping:T):T {
 if(mapping.version===2)return mapping
 const next=structuredClone(mapping)
 for(const key of ['sources','unmapped','unmappedEvidence'] as const)if(next[key]?.length===0)delete next[key]
 if(next.case?.parameters&&Object.keys(next.case.parameters).length===0)delete next.case.parameters
 for(const source of next.sources??[])if(source.parameters&&Object.keys(source.parameters).length===0)delete source.parameters
 for(const kind of ['facts','evidence'] as const)if(next.admits?.[kind]&&Object.keys(next.admits[kind]).length===0)delete next.admits[kind]
 return next
}
