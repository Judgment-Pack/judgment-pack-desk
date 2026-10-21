// Fresh private producers read by the existing browser document, chat and link owners.
import assert from 'node:assert/strict'
import {readFile, readdir} from 'node:fs/promises'
import {join} from 'node:path'
import {ChatStore} from '../web/src/chat/store.ts'
import {linkReading} from '../web/src/chat/linkTools.ts'
import {validLinkStep,linkStepFromResult} from '../web/src/chat/linkStep.ts'
import {verifyDocument} from '../web/src/documents/client.ts'
import {normalizeLink,extractLinks} from '../web/src/documents/link.ts'
import {validWebURL} from '../web/src/documents/record.ts'
import {CONTEXT_DEFAULTS} from '../web/node_modules/@judgment-pack/harness/dist/context/policy.js'
import urlVectors from '../internal/desk/testdata/assistant-link-url.json'
import extractionVectors from '../internal/desk/testdata/assistant-link-extraction.json'
import historicalURLVectors from '../internal/desk/testdata/assistant-search-url.json'
import Ajv from '../web/node_modules/ajv/dist/ajv.js'
const dir=process.argv[2]
assert(dir,'fresh Go producer directory required')
const read=async name=>JSON.parse(await readFile(join(dir,name),'utf8'))
for(const row of urlVectors){const result=normalizeLink(row.input);assert.equal('refused'in result,Boolean(row.refused),row.name);if(!row.refused)assert.deepEqual(result,{fetchUrl:row.fetchUrl,displayUrl:row.displayUrl,anchor:row.anchor,anchorKind:row.anchorKind},row.name)}
for(const row of extractionVectors)assert.deepEqual(extractLinks(row.text),row.links,row.name)
for(const row of historicalURLVectors)assert.equal(validWebURL(row.url),row.browser,row.name)
const config=pin=>({gateway:pin,sources:{search:null,read:null,web:null},limits:{searches:8,reads:12,bytes:8388608,seconds:600},documents:{enabled:true,source:'documents',maxFileBytes:16777216,maxRequestBytes:33554432,maxResponseBytes:16777216}})
async function reader(object,pin,attachment){
 const verified=await verifyDocument(object,pin,attachment.document.digest)
 const deps={available:()=>true,researchEnabled:()=>false,config:()=>config(pin),documents:()=>[attachment],addDocument:()=>{throw Error('browser reader cannot save')},log:()=>{},load:async reference=>{assert.deepEqual(reference,attachment.document);return verifyDocument(object,pin,reference.digest)},ingest:async()=>{throw Error('browser reader cannot acquire')}}
 const tool=linkReading(deps)({turns:()=>[{id:'supplied',role:'user',kind:'message',text:attachment.link.url,at:'2026-10-21T00:00:00Z'}],policy:()=>CONTEXT_DEFAULTS}).find(tool=>tool.name==='read_link')
 assert(tool)
 return {verified,tool}
}
const inputSchema=await read('input-schema.json'), schemaVector={url:'example.org/report',offset:1,find:'alpha beta'}
const ajv=new Ajv({strict:false});assert(ajv.validate(inputSchema,schemaVector))
for(const member of ['url','offset','find']){const scratch=structuredClone(inputSchema);delete scratch.properties[member];assert.equal(ajv.validate(scratch,schemaVector),false,member+' input schema omission')}
let writes=0,engines=0,windows=0
for(const engine of ['api','codex','read-child']){
 const [document,object,pin,expected]=await Promise.all([`${engine}.json`,`${engine}-object.json`,`${engine}-pin.json`,`${engine}-window.json`].map(read))
 const noWrite=async()=>{writes++;throw Error('reader cannot save')}
 const store=new ChatStore('fixture',{read:async()=>({project:'fixture',sha256:'fixture',content:document}),write:noWrite},{read:async()=>({project:'fixture',sha256:'fixture',content:{version:1,drafts:[],deleted:[]}}),write:noWrite})
 try{
  await store.load();assert.equal(store.getSnapshot().error,'')
  const chat=store.getSnapshot().chats.find(chat=>chat.id==='chat-one');assert(chat)
  assert.equal(chat.documents.length,1)
  const attachment=chat.documents[0],response=chat.checkpoint.state.responses.at(-1)
  assert.deepEqual(response.documents,[attachment])
  const row=response.work.items.find(item=>item.name==='read_link');assert(row);assert(validLinkStep(row.link));assert.deepEqual(row.link,linkStepFromResult(expected.structuredContent))
  const {verified,tool}=await reader(object,pin,attachment);assert.equal(verified.record.provenance.source.requestedUrl,attachment.link.url)
  const result=await tool.execute({url:'example.org/report#alpha-beta',find:'alpha beta'},new AbortController().signal)
  assert.deepEqual(result,expected,engine+' browser/private window parity')
  engines++
 }finally{store.dispose()}
}
for(const name of (await readdir(dir)).filter(name=>/^window-\d+\.json$/.test(name)).sort()){
 const fixture=await read(name),{tool}=await reader(fixture.object,fixture.pin,fixture.attachment)
 assert.equal(fixture.arguments.length,fixture.results.length)
 for(let i=0;i<fixture.arguments.length;i++){
  const result=await tool.execute(fixture.arguments[i],new AbortController().signal)
  assert.deepEqual(result,fixture.results[i],fixture.name+' actual browser/private window '+i)
  assert(new TextEncoder().encode(JSON.stringify(result)).length<=CONTEXT_DEFAULTS.toolResultBytes)
  assert(!/[\uD800-\uDBFF](?![\uDC00-\uDFFF])|(?<![\uD800-\uDBFF])[\uDC00-\uDFFF]/u.test(result.content[0].text),fixture.name+' split surrogate')
  windows++
 }
}
const partial=await read('partial-selected.json')
const {tool:reviewedTool}=await reader(partial.object,partial.pin,partial.attachment)
const reviewed=await reviewedTool.execute({url:partial.attachment.link.url},new AbortController().signal)
assert.deepEqual(reviewed,partial.reviewed,'confirmed selected partial page exact browser/private window')
const unreviewedAttachment=structuredClone(partial.attachment);unreviewedAttachment.document.allowPartial=false;unreviewedAttachment.document.needsReview=true
const {tool:unreviewedTool}=await reader(partial.object,partial.pin,unreviewedAttachment)
const unreviewed=await unreviewedTool.execute({url:unreviewedAttachment.link.url},new AbortController().signal)
assert.equal(unreviewed.isError,true);assert.equal(partial.unreviewed.isError,true)
assert(!unreviewed.content[0].text.includes('Exact quote'));assert(!partial.unreviewed.content[0].text.includes('Exact quote'))
assert.equal(writes,0);assert.equal(engines,3);assert(windows>=8)
console.log(`Link shared readers: ${engines} actual API/native/read-child projections, ${windows} UTF16 windows, URL normalization and exact sealed originals passed.`)
