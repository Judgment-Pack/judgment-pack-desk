import {digestOf,jsonIdentity} from '../research/checkCandidate'
import type {TestCase,TestSuite} from '../packs/test-workspace/model'
import type {InputPreview,JobInput} from '../jobs/client'
import {exportDialect,type PreparationReply,type DialectRecord} from './client'

export interface DialectTestSnapshot {
 version:1; identity:string; preparationId:string; recordId:string; recipeVersion:number; packDigest:string; capturedAt:string
 source:ReturnType<typeof exportDialect>; input:JobInput
}
/** Test fixtures are frozen observations. They carry no inferred expectation. */
export async function dialectTestCase(saved:PreparationReply,record:DialectRecord,preview:InputPreview):Promise<TestCase> {
 const q=saved.document.recipe.qualification
 if(!q)throw Error('Save a pack mapping in the Dialects recipe first.')
 const {review:_review,...content}=record
 const identity=await digestOf(jsonIdentity({preparation:saved.document.id,record:content,recipeVersion:saved.document.recipeVersion,qualification:q}))
 const id='input-'+identity.slice(0,32)
 return structuredClone({id,name:record.name,revision:1,row:{id,facts:preview.input.facts??{},...(preview.input.evidence?{evidenceAvailability:preview.input.evidence}:{})},sources:[],sourceMappings:{},origin:'import',rationale:'Frozen Dialects record. Review the expected decision independently before running as a test.',inputSnapshot:{version:1,identity,preparationId:saved.document.id,recordId:record.id,recipeVersion:saved.document.recipeVersion,packDigest:q.digest,capturedAt:new Date().toISOString(),source:exportDialect(saved.document,[record]),input:preview.input}})
}
export function appendDialectTestCases(suite:TestSuite,cases:TestCase[]):TestSuite {
 const next=structuredClone(suite),seen=new Set(next.cases.flatMap(c=>c.inputSnapshot?[c.inputSnapshot.identity]:[]))
 for(const c of cases){
  if(!c.inputSnapshot)throw Error('The input snapshot is missing.')
  if(seen.has(c.inputSnapshot.identity))continue
  if(next.cases.some(existing=>existing.id===c.id))throw Error('A case with this ID already exists. Review it before adding the snapshot.')
  next.cases.push(structuredClone(c));seen.add(c.inputSnapshot.identity)
 }
 if(next.cases.length>256)throw Error('A suite supports up to 256 cases. Reduce the selection.')
 return next
}
