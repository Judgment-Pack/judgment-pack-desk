import {expect,it} from 'vitest'
import {recordDigest,sha256} from '@judgment-pack/harness/records'
import {decodeCheckpoint,checkpoint as savedCheckpoint} from '../../chat/checkpoint'
import {INITIAL_STATE} from '../../research/initialState'
import pending from './fixtures/pending.json'
import base from './fixtures/checkpoint.json'
import {decodeSnapshot,parseRunJSON} from './decode'

// Handwritten source and independently spelled canonical map; never captured
// from the producer under test. Opaque payload keys remain case-sensitive.
const source='{"z":2,"a":{"last":1,"first":0},"Fact":false,"fact":null,"zero":0,"array":[false,null,0],"decimal":1.2}'
const value={a:{first:0,last:1},array:[false,null,0],decimal:1.2,Fact:false,fact:null,z:2,zero:0}
const binding={owner:{kind:'chat' as const,id:'chat-one',revision:'a'.repeat(64)},requestId:'message-one'}
const entry=()=>({id:'fixture:item',kind:'artifact',label:'Exact source',value:structuredClone(value),valueJSON:source,bytes:new TextEncoder().encode(source).length,digest:sha256(source)})
const execution=(version:1|2|3=2)=>({...structuredClone(base),version,context:{scope:'fixture',entries:[version>=2?entry():{...entry(),value:JSON.parse(source),valueJSON:undefined}]},operations:[{id:'read-one',name:'read',status:'complete',args:{Fact:false,fact:null,zero:0},result:{content:[],providerExtension:{Fact:false,fact:null,zero:0}}}],policy:{fixture:{value:{Fact:false,fact:null,zero:0},layer:'run'}}})
const snapshot=(cp:unknown)=>decodeSnapshot(parseRunJSON(JSON.stringify({...pending,checkpoint:cp})),binding)

it.each([1,2,3] as const)('keeps checkpoint %s and exact retained source through Desk snapshot and saved-chat readers',version=>{
 const input=execution(version)
 const schema='https://github.com/Judgment-Pack/judgment-pack-agent/blob/main/contract/schemas/checkpoint.json'
 const held={...input,schema,retainUntil:'2030-01-01T00:00:00.000Z',digest:''};held.digest=recordDigest(held)
 const before=JSON.stringify(held)
 const cp=snapshot(held).checkpoint!
 const saved=savedCheckpoint({...structuredClone(INITIAL_STATE),execution:cp},[])
 const restored=decodeCheckpoint(JSON.parse(JSON.stringify(saved))).state.execution!
 expect(JSON.stringify(held)).toBe(before)
 expect(restored.version).toBe(version)
 expect(restored.retainUntil).toBe(held.retainUntil)
 expect(restored.digest).toBe(held.digest)
 expect(restored.context.entries[0]).toEqual(JSON.parse(JSON.stringify(input.context.entries[0])))
 expect(restored.operations[0]!.args).toEqual({Fact:false,fact:null,zero:0})
 expect(restored.operations[0]!.result).toHaveProperty('providerExtension',{Fact:false,fact:null,zero:0})
 expect(restored.policy?.fixture?.value).toEqual({Fact:false,fact:null,zero:0})
 if(version===1)expect(Object.hasOwn(restored.context.entries[0]!,'valueJSON')).toBe(false)
 else expect(restored.context.entries[0]!.valueJSON).toBe(source)
})

it('carries child retained context unchanged rather than reconstructing its valueJSON',()=>{
 const child={...execution(),id:'child-one'}
 const cp={...execution(),durable:{dispatches:[],observations:[],children:[{scope:{execution:'child-one',task:'task-one',role:'author'},checkpoint:child}]}}
 expect(snapshot(cp).checkpoint!.durable!.children[0]!.checkpoint.context.entries[0]).toEqual(entry())
})

it('refuses unsupported versions and undeclared structural members through both Desk checkpoint readers',()=>{
 const cp=execution()
 for(const bad of [{...cp,version:4},{...cp,version:'2'},{...cp,version:undefined},{...cp,authority:true},{...cp,context:{...cp.context,authority:true}},{...cp,version:1}, {...cp,context:{...cp.context,entries:[{...entry(),valueJSON:3}]}}]){
  expect(()=>snapshot(bad)).toThrow()
  expect(()=>decodeCheckpoint({state:{...structuredClone(INITIAL_STATE),execution:bad},sources:[]})).toThrow()
 }
})

it('counts duplicated companion text and nested child snapshots within the unchanged 4 MiB checkpoint bound',()=>{
 const text=JSON.stringify('x'.repeat(1100000))
 const large={...entry(),value:JSON.parse(text),valueJSON:text,bytes:text.length,digest:sha256(text)}
 const cp={...execution(),context:{scope:'fixture',entries:[large]}}
 expect(snapshot(cp).checkpoint!.context.entries[0]!.bytes).toBe(text.length)
 const child={...cp,id:'child-one'}
 const nested={...cp,durable:{dispatches:[],observations:[],children:[{scope:{execution:'child-one',role:'author',task:'task-one'},checkpoint:child}]}}
 expect(new TextEncoder().encode(JSON.stringify(nested)).length).toBeGreaterThan(4*1024*1024)
 expect(()=>snapshot(nested)).toThrow()
})

const route={route:'agent',skills:[],tools:'all',operations:'all',planning:'offered',version:1,decidedBy:'host',reason:'retained fixture',bindingDigest:'binding-one',catalogueDigest:'catalogue-one',widened:false,refusals:[]}
const currentEvents=[
 {type:'repeat_detected',version:2,execution:'execution-one',dispatchId:'dispatch-one',keyDigest:'repeat-one',count:2,reminder:true,stopped:false},
 {type:'route_decided',version:2,route},
 {type:'route_widened',version:2,route},
]
it('retains historical absent/1 and current event 2 without changing supplied versions',()=>{
 for(const event of [{type:'message',text:'Kept.'},{type:'message',text:'Kept.',version:1},...currentEvents]){
  const wire={...pending,events:[{id:'event-one',event}],cursor:'1:1'}
  expect(decodeSnapshot(parseRunJSON(JSON.stringify(wire)),binding).events[0]!.event).toEqual(event)
 }
})

it('refuses future and unknown structural event records at Desk browser admission',()=>{
 for(const event of [{type:'message',text:'Kept.',version:3},{type:'message',text:'Kept.',version:2},{...currentEvents[0],version:1},{...currentEvents[0],authority:true},{type:'future_event',version:2},{type:'message',text:'Kept.',authority:true},{type:'message',text:'Kept.',version:'1'}]){
  const wire={...pending,events:[{id:'event-one',event}],cursor:'1:1'}
  expect(()=>decodeSnapshot(parseRunJSON(JSON.stringify(wire)),binding)).toThrow()
 }
})

it('refuses duplicate checkpoint and event structural members before browser interpretation',()=>{
 const wire=JSON.stringify({...pending,checkpoint:execution(3)})
 expect(()=>parseRunJSON(wire.replace('"version":3','"version":3,"version":3'))).toThrow()
 const events=JSON.stringify({...pending,events:[{id:'event-one',event:currentEvents[0]}],cursor:'1:1'})
 expect(()=>parseRunJSON(events.replace('"version":2','"version":2,"version":2'))).toThrow()
})
