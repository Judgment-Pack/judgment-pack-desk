import {parseJSON} from './canon'
import type {DialectAuthoringReport} from './authoring'

/** Review evidence, never authority to save or publish. */
export interface DialectCheckCheckpoint {document:string;report?:DialectAuthoringReport}
export function validDialectCheckCheckpoint(value:unknown):value is DialectCheckCheckpoint {
 if(!value||typeof value!=='object'||Array.isArray(value))return false
 const v=value as Record<string,unknown>
 if(Object.keys(v).some(k=>!['document','report'].includes(k))||typeof v.document!=='string'||new TextEncoder().encode(v.document).length>131072)return false
 try{parseJSON(v.document)}catch{return false}
 if(v.report===undefined)return true
 if(!v.report||typeof v.report!=='object'||Array.isArray(v.report)||JSON.stringify(v.report).length>4194304)return false
 const r=v.report as Record<string,unknown>
 if(r.document!==undefined&&typeof r.document!=='string'||r.digest!==undefined&&typeof r.digest!=='string')return false
 if(r.compatibility!==undefined){
  if(!r.compatibility||typeof r.compatibility!=='object'||Array.isArray(r.compatibility))return false
  const c=r.compatibility as Record<string,unknown>
  if(!Array.isArray(c.notes)||!c.notes.every(n=>!!n&&typeof n==='object'&&!Array.isArray(n)&&typeof n.code==='string'&&typeof n.kind==='string'&&typeof n.target==='string'&&typeof n.severity==='string'))return false
 }
 return typeof r.status==='string'&&Array.isArray(r.diagnostics)&&r.diagnostics.every(d=>!!d&&typeof d==='object'&&!Array.isArray(d)&&typeof d.code==='string'&&typeof d.message==='string'&&(d.instancePath===undefined||typeof d.instancePath==='string'))
}
