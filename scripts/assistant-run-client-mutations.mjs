// Mutate esbuild's in-memory inputs only. Never rewrite shared source files.
import { build } from '../web/node_modules/esbuild/lib/main.js'
import { readFile, mkdtemp, rm } from 'node:fs/promises'
import { tmpdir } from 'node:os'
import { resolve, join } from 'node:path'
import { spawnSync } from 'node:child_process'
const root=resolve(import.meta.dirname,'..')
const rows=[
 ['owner source revision','decode.ts','value.owner.revision !== expected.owner.revision','false'],
 ['canonical checkpoint','decode.ts','readExecution(value.checkpoint)','value.checkpoint'],
 ['known semantic discriminator','decode.ts','!Object.hasOwn(SEMANTIC_EVENT_TYPES,value.event.type)','false'],
 ['shared cursor snapshot','stream.ts',"    if (frame.kind === 'snapshot') {","    if (frame.kind === 'snapshot') {\n      if (this.last === frame.cursor) return undefined"],
 ['uncertain POST replay','client.ts','      if (cause instanceof RunRequestError) throw cause\n      throw new RunRequestError', '      if (body) return this.json(path,expected,body,signal)\n      if (cause instanceof RunRequestError) throw cause\n      throw new RunRequestError'],
 ['retained answer digest','decode.ts','questionDigest(q)!==a.questionDigest','false'],
 ['canonical grant receipt','decode.ts','!checkpoint.grants.some(saved=>saved.from===g.from&&saved.to===g.to&&saved.at===g.at)','false']
]
const tmp=await mkdtemp(join(tmpdir(),'assistant-run-client-mutants-'))
try {
 for(const row of [null,...rows]){
  let replaced=false
  const outfile=join(tmp,'probe.mjs')
  await build({entryPoints:[join(root,'scripts/assistant-run-client-mutation-probe.mjs')],outfile,bundle:true,platform:'node',format:'esm',logLevel:'silent',plugins:[{name:'isolated-transport-and-mutant',setup(b){
   b.onLoad({filter:/web\/src\/files\/client\.ts$/},()=>({contents:'export const deskFetch=(...args)=>fetch(...args)',loader:'js'}))
   b.onLoad({filter:/web\/src\/mcp\/session\.ts$/},()=>({contents:"export const sessionBearer=async()=>'session';export const whenSessionEnds=()=>()=>{};export const discardBody=async answer=>{try{await answer.body?.cancel()}catch{}}",loader:'js'}))
   b.onLoad({filter:/web\/src\/assistant\/serverRuns\/(client|decode|stream)\.ts$/},async args=>{
    let contents=await readFile(args.path,'utf8')
    if(row && args.path.endsWith('/'+row[1])){assertSingle(contents,row[2]);contents=contents.replace(row[2],row[3]);replaced=true}
    return {contents,loader:'ts'}
   })
  }}]})
  if(row&&!replaced)throw Error('mutant was not applied: '+row[0])
  const result=spawnSync(process.execPath,[outfile],{encoding:'utf8',timeout:30000})
  if(result.error || result.signal)throw Error('probe infrastructure failed: '+(result.error?.message??result.signal))
  if(!row&&result.status!==0)throw Error('baseline probe failed:\n'+result.stderr)
  if(row&&result.status===0)throw Error('survived: '+row[0])
  const markers=['source revision must remain exact','checkpoint must use canonical reader','unknown union discriminator','same cursor must not suppress snapshot','uncertain POST must not replay','answer digest must match retained question','grant receipt must match canonical checkpoint']
  if(row&&(!result.stderr.includes('AssertionError [ERR_ASSERTION]')||!result.stderr.includes(markers[rows.indexOf(row)])))throw Error('non-semantic mutant failure: '+row[0]+'\n'+result.stderr)
  console.log('Run client mutation: '+(row?row[0]+' caught':'baseline passed'))
 }
} finally {await rm(tmp,{recursive:true,force:true})}
function assertSingle(text,needle){if(text.split(needle).length!==2)throw Error('mutation needle must occur once')}
