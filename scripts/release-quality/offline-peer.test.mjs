import {test} from 'node:test'
import assert from 'node:assert/strict'
import {spawnSync} from 'node:child_process'
import {fileURLToPath} from 'node:url'
const peer=fileURLToPath(new URL('./offline-peer.py',import.meta.url))
const request=(method,params={})=>({jsonrpc:'2.0',id:1,method,params})
const run=messages=>spawnSync('/usr/bin/python3',[peer,'runtime','policy','mcp'],{input:messages.map(q=>JSON.stringify(q)+'\n').join(''),encoding:'utf8',timeout:3000,maxBuffer:16384})
test('empty mounted project UI inventory is metadata and does not alter model offers',()=>{
 const probe=run([request('tools/list'),request('tools/call',{name:'list_packs',arguments:{}})])
 assert.equal(probe.status,0);assert.equal(probe.stderr,'')
 const replies=probe.stdout.trim().split('\n').map(JSON.parse)
 assert.deepEqual(replies[0].result,{tools:[{name:'get_schema',description:'Public recorded schema fixture',inputSchema:{type:'object',properties:{},additionalProperties:false}}]})
 assert.deepEqual(replies[1],{jsonrpc:'2.0',id:1,result:{content:[{type:'text',text:'{"packs":[]}'}],structuredContent:{packs:[]},isError:false}})
})
test('unknown and malformed inventory requests remain refused',()=>{
 for(const params of [{name:'list_packs',arguments:{extra:true}},{name:'list_packs'},{name:'list_packs',arguments:null},{name:'list_packs',arguments:[],extra:true},{name:'list_packs',arguments:{},extra:true},{name:'other',arguments:{}}]){
  const probe=run([request('tools/call',params)])
  assert.equal(probe.status,65);assert.equal(probe.stdout,'');assert.equal(probe.stderr,'')
 }
})
