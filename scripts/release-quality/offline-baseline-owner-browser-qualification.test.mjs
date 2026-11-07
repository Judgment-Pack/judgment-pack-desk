import test from 'node:test'
import assert from 'node:assert/strict'
import {readFileSync} from 'node:fs'
import {fileURLToPath} from 'node:url'
import {baselineOptions} from './offline-baseline-owner-browser-qualification.mjs'
import {baselineIdentity,validateApplicationRole,validateCaseEvidence} from './offline-owner-browser-qualification.mjs'
const source=readFileSync(new URL('./offline-owner-browser-qualification.mjs',import.meta.url),'utf8')
const applicationRoot='/supplied-baseline',options=baselineOptions({out:'/out',helper:'/owner',browserBin:'/browser',applicationRoot,publicRoot:'/public'})
test('baseline binds original archive commit/tree and exact reader under its own application root',()=>{
 assert.equal(options.role,'baseline');assert.equal(options.applicationRoot,applicationRoot)
 assert.equal(options.baseCommit,'ef378c406f8b96e2c0c8854bc5e4dff40ac841e2');assert.equal(options.baseTree,'0034c3623b5b6e9bb8741da900392fd1c29e1508')
 assert.deepEqual(options.baselineReader,{path:'/supplied-baseline/scripts/release-quality/baseline-replay-report.mjs',sha256:'2229691d8fc96f5a1a818a9aa53788dceebcc4ab6abbe2e4913f42562922ec53'})
 assert.throws(()=>validateApplicationRole({...options,baseCommit:'a'.repeat(40)}))
 assert.throws(()=>validateApplicationRole({...options,baseTree:'b'.repeat(40)}))
 assert.throws(()=>validateApplicationRole({...options,baselineReader:{...options.baselineReader,path:'/other/reader.mjs'}}))
 assert.throws(()=>validateApplicationRole({...options,baselineReader:{...options.baselineReader,sha256:'c'.repeat(64)}}))
})
test('candidate defaults remain local with no baseline reader; cross-role roots refuse',()=>{
 const candidate=validateApplicationRole({});assert.equal(candidate.role,'candidate');assert.equal(candidate.baselineReader,null)
 assert.equal(candidate.applicationRoot,fileURLToPath(new URL('../..',import.meta.url)).replace(/\/$/,''))
 assert.throws(()=>validateApplicationRole({role:'candidate',applicationRoot}))
 assert.throws(()=>validateApplicationRole({baselineReader:options.baselineReader}))
 assert.throws(()=>validateApplicationRole({...options,applicationRoot:candidate.applicationRoot}))
 assert.equal(baselineIdentity.readerSha256,options.baselineReader.sha256)
})
test('baseline source/assets/owner and reader identity never use candidate equivalents',()=>{
 assert(source.includes('sources=sourceInventory(applicationRoot),assetIdentity=inventory(assets)'))
 assert(source.includes("assets=join(applicationRoot,'web/dist')"))
 assert(source.includes("inventory(join(applicationRoot,'scripts/release-quality/live-owner')).sha256"))
 assert(source.includes("reader:role==='baseline'?baselineReader.sha256:hash(join(here,'replay-report.mjs'))"))
 assert(source.includes('assert.equal(hash(baselineReader.path),baselineReader.sha256)'))
 assert(source.includes("driver:hash(join(here,'live-browser-replay.mjs'))"))
 assert(!source.includes('CaptureDouble'))
})
const empty=()=>({evidence:{canonical:'unavailable-baseline-contract',tools:[],runtimeTools:[]}})
const schema=()=>({evidence:{canonical:'unavailable-baseline-contract',tools:[{callId:'a'.repeat(48),name:'desk_runtime_0',args:{},text:'BEGIN RETRIEVED DATA fixture\n{"isError":false,"structuredContent":{"schema":"recorded"}}\nEND RETRIEVED DATA fixture',isError:false,structuredState:'unavailable-native-contract'}],runtimeTools:[{rpcId:2,name:'get_schema',args:{},text:'{"schema":"recorded"}',structured:{schema:'recorded'},structuredState:'value',isError:false}]}})
test('baseline preserves unavailable canonical and separate native/Runtime evidence',()=>{
 validateCaseEvidence(empty(),0,'/unused','baseline');validateCaseEvidence(schema(),1,'/unused','baseline')
 const fake=schema();fake.evidence.canonical={events:[],operations:[]};assert.throws(()=>validateCaseEvidence(fake,1,'/unused','baseline'))
 const merged=schema();merged.evidence.tools[0].structuredState='value';assert.throws(()=>validateCaseEvidence(merged,1,'/unused','baseline'))
 const missing=schema();missing.evidence.runtimeTools=[];assert.throws(()=>validateCaseEvidence(missing,1,'/unused','baseline'))
 const changed=schema();changed.evidence.runtimeTools[0].structured.schema='other';assert.throws(()=>validateCaseEvidence(changed,1,'/unused','baseline'))
 assert.throws(()=>validateCaseEvidence(empty(),0,'/unused','candidate'))
})
