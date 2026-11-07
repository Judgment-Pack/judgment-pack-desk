// Transform scratch bundles in memory; shared source and node_modules stay unchanged.
import { build } from '../web/node_modules/esbuild/lib/main.js'
import { readFile, mkdtemp, rm } from 'node:fs/promises'
import { tmpdir } from 'node:os'
import { resolve, join } from 'node:path'
import { spawnSync } from 'node:child_process'
const root=resolve(import.meta.dirname,'..')
const rows=[
 ['latest failed check','presentation.ts','packReview:{...c.packReview!,check}','packReview:{...c.packReview!,check:c.packReview!.check??check}','latest failed check must replace older pass'],
 ['persist before POST','controller.ts','const revision=await this.ports.prepare(this.base,requestId,this.marker!.admissionId,this.scope.signal)',"const revision='a'.repeat(64)",'original request must be saved before admission'],
 ['lost acknowledgement replay','controller.ts',"if(cause instanceof RunRequestError&&cause.delivery==='uncertain') {","if(cause instanceof RunRequestError&&cause.delivery==='uncertain') { await client.start({admissionId:this.marker!.admissionId,purpose:'chat',ownerId:this.ownerId,revision,requestId},this.scope.signal)",'lost acknowledgement must never repeat POST'],
 // F1's unreconciled gate independently protects unacknowledged terminals. Remove
 // both retry defenses explicitly: a single ACK edit is now a redundant mutant.
 ['paired terminal acknowledgement and reconciliation retry gates','controller.ts',[
  ['if(this.unreconciled)return false;',''],
  ['!!this.snapshot?.chatProjection?.acknowledged','!!this.snapshot'],
 ],null,'terminal without acknowledgement must stay pinned'],
 ['required admission identity','decode.ts','!text(value.admissionId,128)','false','admission identity must remain required'],
 ['required approval reference','decode.ts',"if(value.state==='awaiting-input'&&checkpoint?.approval?.state==='asked'&&value.approvalReference===undefined)return invalid()",'','asked approval must carry its exact reference'],
 ['dirty field conflict','reconcile.ts','if(!same(old[key],saved[key])&&!same(current[key],saved[key]))throw Error', 'if(false)throw Error','same-field conflict must retain local draft without write'],
 ['edited numeric custody','wire.ts','old&&!edited.has(key)','old','an edited numeric field must not reuse old raw bytes'],
]
const scratch=await mkdtemp(join(tmpdir(),'saved-chat-mutants-'))
try{
 for(const row of [null,...(process.argv.includes('--latest-check-only')?rows.filter(r=>r[0]==='latest failed check'):rows)]){
  let applied=false
  const outfile=join(scratch,'probe.mjs')
  await build({entryPoints:[join(root,'scripts/saved-chat-consumer-probe.mjs')],outfile,bundle:true,platform:'node',format:'esm',logLevel:'silent',plugins:[{name:'scratch-only',setup(b){
   b.onLoad({filter:/web\/src\/i18n\/index\.ts$/},()=>({contents:'export const msg=x=>x;export const systemMessage=x=>x;export const useLocale=()=>"en";',loader:'js'}))
   b.onLoad({filter:/web\/src\/packs\/test-workspace\/client\.ts$/},()=>({contents:'export const readTests=async()=>{throw Error("Unexpected suite read")}',loader:'js'}))
   b.onLoad({filter:/web\/src\/mcp\/session\.ts$/},()=>({contents:"export class NoSession extends Error{};export const refusalCode=()=>null;export const forgetSession=()=>{};export const sessionBearer=async()=>'session';export const whenSessionEnds=()=>()=>{};export const discardBody=async r=>{try{await r.body?.cancel()}catch{}}",loader:'js'}))
   b.onLoad({filter:/web\/src\/(assistant\/serverRuns|chat)\/(controller|decode|reconcile|wire|presentation)\.ts$/},async args=>{
    let contents=await readFile(args.path,'utf8')
    if(row&&args.path.endsWith('/'+row[1])){
     const edits=Array.isArray(row[2])?row[2]:[[row[2],row[3]]]
     // Validate every exact needle before applying any part of a paired mutation.
     for(const [needle] of edits)if(contents.split(needle).length!==2)throw Error('mutation needle not unique: '+row[0]+': '+needle)
     for(const [needle,replacement] of edits)contents=contents.replace(needle,replacement)
     applied=true
    }
    return {contents,loader:'ts'}
   })
  }}]})
  if(row&&!applied)throw Error('mutation not applied: '+row[0])
  const result=spawnSync(process.execPath,[outfile],{encoding:'utf8',timeout:20000})
  if(result.error||result.signal)throw Error('probe infrastructure failed: '+(result.error?.message??result.signal))
  if(!row&&result.status!==0)throw Error('baseline failed:\n'+result.stderr)
  if(row&&(result.status===0||!result.stderr.includes('AssertionError')||!result.stderr.includes(row[4])))throw Error('mutation survived or failed nonsemantically: '+row[0]+'\n'+result.stderr)
  console.log('Saved-chat mutation: '+(row?row[0]+' caught':'baseline passed'))
 }
}finally{await rm(scratch,{recursive:true,force:true})}
