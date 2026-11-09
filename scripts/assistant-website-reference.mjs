// Fresh private website/page producers consumed by the existing browser owners.
import assert from 'node:assert/strict'
import {readFile} from 'node:fs/promises'
import {join} from 'node:path'
import {validateDiscovery,verifyWebsite} from '../web/src/documents/website.ts'
import {verifyDocument,selectedPages} from '../web/src/documents/client.ts'
import {validWebURL} from '../web/src/documents/record.ts'
import {ChatStore} from '../web/src/chat/store.ts'
import {linkReading} from '../web/src/chat/linkTools.ts'
import vectors from '../internal/desk/testdata/assistant-website-discovery.json'
import {CONTEXT_DEFAULTS} from '../web/node_modules/@judgment-pack/harness/dist/context/policy.js'
import Ajv from '../web/node_modules/ajv/dist/ajv.js'
for(const row of vectors.cases){const value=structuredClone(vectors.base);for(const edit of row.edits){let at=value;for(const key of edit.path.slice(0,-1))at=at[key];const key=edit.path.at(-1);if(edit.remove)delete at[key];else at[key]=edit.value}let valid=true;try{validateDiscovery(value,vectors.base.seed)}catch{valid=false}assert.equal(valid,row.valid,row.name)}
for(const row of vectors.origins)assert.equal(validWebURL(row.a)&&validWebURL(row.b)&&new URL(row.a).origin===new URL(row.b).origin,row.same,JSON.stringify(row))
const dir=process.argv[2]
if(!dir){console.log(`Website structural readers: ${vectors.cases.length} structures and ${vectors.origins.length} origins passed.`);process.exit(0)}
const read=async name=>JSON.parse(await readFile(join(dir,name),'utf8'))
const schema=await read('input-schema.json'),ajv=new Ajv({strict:false}),schemaVector={url:'example.org/report'}
assert(ajv.validate(schema,schemaVector));const scratch=structuredClone(schema);delete scratch.properties.url;assert.equal(ajv.validate(scratch,schemaVector),false,'url schema omission')
let engines=0
for(const engine of ['api','codex','research-child']){
 const [document,website,page,pin,expected]=await Promise.all([`${engine}.json`,`${engine}-website.json`,`${engine}-page.json`,`${engine}-pin.json`,`${engine}-window.json`].map(read))
 const deny=async()=>{throw Error('browser reader cannot write')}
 const store=new ChatStore('fixture',{read:async()=>({project:'fixture',sha256:'fixture',content:document}),write:deny},{read:async()=>({project:'fixture',sha256:'fixture',content:{version:1,drafts:[],deleted:[]}}),write:deny})
 try{
  await store.load();assert.equal(store.getSnapshot().error,'')
  const chat=store.getSnapshot().chats.find(c=>c.id==='chat-one');assert(chat);assert.equal(chat.websites.length,1);assert.equal(chat.documents.length,1)
  const response=chat.checkpoint.state.responses.at(-1);assert.deepEqual(response.websites,chat.websites);assert.deepEqual(response.documents,chat.documents);assert(response.work.items.some(item=>item.name==='explore_website'));assert(response.work.items.some(item=>item.name==='read_link'))
  const ref=chat.websites[0],found=await verifyWebsite(website,ref,pin),file=chat.documents[0],verified=await verifyDocument(page,pin,file.document.digest)
  assert(found.discovery.pages.some(p=>p.url===file.link.url&&p.status==='discovered'));assert.equal(page.proof.web.site,ref.seed);assert(selectedPages(verified,file.document).every(p=>p.citation.startsWith(`attachment:${file.id}/`)))
  const config={gateway:pin,sources:{search:null,read:null,web:{source:'web',discovery:'web-discovery'}},limits:{searches:8,reads:12,bytes:8388608,seconds:600},documents:{enabled:true,source:'documents',maxFileBytes:16777216,maxRequestBytes:33554432,maxResponseBytes:16777216}}
  const factory=linkReading({available:()=>true,researchEnabled:()=>true,discoveryAvailable:()=>true,config:()=>config,websites:()=>chat.websites,documents:()=>chat.documents,loadWebsite:async r=>verifyWebsite(website,r,pin),load:async r=>verifyDocument(page,pin,r.digest),addDocument:deny,ingest:deny,ingestSite:deny,log:()=>{}})
  const tools=factory({turns:()=>chat.checkpoint.state.turns,policy:()=>CONTEXT_DEFAULTS}),tool=tools.find(t=>t.name==='read_link');assert(tool)
  const actual=await tool.execute({url:'https://example.org/child-0',find:'alpha beta'},new AbortController().signal);assert.deepEqual(actual,expected,engine+' page window/citation parity');engines++
 }finally{store.dispose()}
}
console.log(`Website shared readers: ${engines} actual API/native/research-child maps and page citations; ${vectors.cases.length} structures/${vectors.origins.length} origins passed.`)
