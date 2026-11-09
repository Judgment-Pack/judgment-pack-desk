// Private measurement evidence, never execution authority or a RunReport reader.
import assert from 'node:assert/strict'
import {constants,openSync,closeSync,fstatSync,writeFileSync,readFileSync} from 'node:fs'
import {join,isAbsolute} from 'node:path'
import {parseApplicationSnapshot,keys,digest} from './replay-report.mjs'
const fail=()=>{throw Error('invalid-canonical-evidence')}
function bindingOf(v){
 keys(v,['role','case','caseId','chatId','runId'])
 if(v.role!=='candidate'||![1,2].includes(v.case)||[v.caseId,v.chatId,v.runId].some(x=>typeof x!=='string'||! /^[A-Za-z0-9_.:/-]{1,128}$/.test(x)))fail()
 return v
}
export function validateCanonicalEvidence(v,binding){
 keys(v,['kind','binding','events','operations'])
 if(v.kind!=='capture-canonical-evidence'||!Array.isArray(v.events)||!Array.isArray(v.operations))fail()
 bindingOf(v.binding);assert.deepEqual(v.binding,bindingOf(binding))
 parseApplicationSnapshot(JSON.stringify(v));return v
}
export function canonicalReference(value){
 const text=JSON.stringify(value)+'\n';parseApplicationSnapshot(text)
 return {kind:'capture-canonical-reference',file:'canonical-case-'+value.binding.case+'.json',binding:structuredClone(value.binding),sha256:digest(text),bytes:Buffer.byteLength(text)}
}
function privateStat(fd){const s=fstatSync(fd);if(!s.isFile()||s.uid!==process.getuid()||(s.mode&0o777)!==0o600||s.size>4*1024*1024)fail();return s}
export function privateCanonicalRetainer(dir){
 if(!isAbsolute(dir))fail()
 return value=>{
  validateCanonicalEvidence(value,value.binding);const ref=canonicalReference(value)
  const fd=openSync(join(dir,ref.file),constants.O_CREAT|constants.O_EXCL|constants.O_WRONLY|constants.O_NOFOLLOW,0o600)
  try{privateStat(fd);writeFileSync(fd,JSON.stringify(value)+'\n');if(privateStat(fd).size!==ref.bytes)fail()}finally{closeSync(fd)}
  return ref
 }
}
export function readCanonicalEvidence(dir,ref,binding){
 keys(ref,['kind','file','binding','sha256','bytes']);bindingOf(binding)
 if(!isAbsolute(dir)||ref.kind!=='capture-canonical-reference'||ref.file!=='canonical-case-'+binding.case+'.json'||!Number.isSafeInteger(ref.bytes)||ref.bytes<1||ref.bytes>4*1024*1024||! /^[a-f0-9]{64}$/.test(ref.sha256))fail()
 assert.deepEqual(ref.binding,binding)
 const fd=openSync(join(dir,ref.file),constants.O_RDONLY|constants.O_NOFOLLOW)
 try{if(privateStat(fd).size!==ref.bytes)fail();const text=readFileSync(fd,'utf8');if(digest(text)!==ref.sha256)fail();return validateCanonicalEvidence(parseApplicationSnapshot(text),binding)}finally{closeSync(fd)}
}
