import type {PolicyImport} from './client'

export const POLICY_SOURCE_BYTE_LIMIT=128*1024
export class InvalidPolicySource extends Error {}
export async function preparePolicyImport(file:File):Promise<{source:PolicyImport;text:string}>{
 if(file.size>POLICY_SOURCE_BYTE_LIMIT||!/^.+\.(md|txt)$/i.test(file.name)||file.name.trim()!==file.name||new TextEncoder().encode(file.name).length>255||/[\\/:\p{Cc}\p{Cf}]/u.test(file.name)||file.type&&!['text/plain','text/markdown'].includes(file.type))throw new InvalidPolicySource()
 const buffer=await new Promise<ArrayBuffer>((resolve,reject)=>{const reader=new FileReader();reader.onload=()=>resolve(reader.result as ArrayBuffer);reader.onerror=()=>reject(new InvalidPolicySource());reader.readAsArrayBuffer(file)})
 const raw=new Uint8Array(buffer)
 let text:string
 try{text=new TextDecoder('utf-8',{fatal:true,ignoreBOM:true}).decode(raw)}catch{throw new InvalidPolicySource()}
 if(/[\u0000-\u0008\u000b\u000c\u000e-\u001f\u007f-\u009f]/.test(text))throw new InvalidPolicySource()
 let binary=''
 for(const byte of raw)binary+=String.fromCharCode(byte)
 return {source:{name:file.name,mediaType:file.type||(file.name.toLowerCase().endsWith('.md')?'text/markdown':'text/plain'),data:btoa(binary)},text}
}
