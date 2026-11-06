import assert from 'node:assert/strict'
import {test} from 'node:test'
import {dirtyObservation,diagnostic,stages} from './persisted-proposal-report.mjs'
const observation={step:'fill',edge:'before',atMs:12,locator:{count:1,enabled:true,readOnly:false},dirty:false}
const base=()=>({stages:Object.fromEntries(stages.map(k=>[k,null])),click:'not-started',button:'unknown',messages:[],requests:[],readOnly:'none',exceptions:[],stop:'none'})
test('finite handwritten dirty observations and optional legacy diagnostics',()=>{
 assert.deepEqual(dirtyObservation(observation),observation);diagnostic(base());diagnostic({...base(),dirtySteps:[observation,{step:'fill',edge:'after',atMs:13,locator:null,dirty:null}]})
 for(const patch of [{step:'raw text'},{edge:'failed prose'},{atMs:180001},{dirty:'yes'},{locator:{count:9,enabled:true,readOnly:false}},{locator:{count:1,enabled:'yes',readOnly:false}}])assert.throws(()=>dirtyObservation({...observation,...patch}),{name:'AssertionError'})
 assert.throws(()=>dirtyObservation({...observation,message:'raw error'}),{name:'Error',message:'invalid-offline-replay-record'})
 assert.throws(()=>diagnostic({...base(),dirtySteps:[observation,observation]}),{name:'AssertionError'})
 assert.throws(()=>diagnostic({...base(),dirtySteps:Array(29).fill(observation)}),{name:'AssertionError'})
})
