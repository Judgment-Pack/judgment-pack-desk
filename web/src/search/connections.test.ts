import { expect,it } from 'vitest'
import { decodeSearchPreference,decodeSearchStatus } from './connections'
it('keeps only non-secret desk choices and rejects unsupported policy',()=>{
 expect(decodeSearchPreference({version:1,connection:'tavily-demo',mode:'auto'}).connection).toBe('tavily-demo')
 for(const raw of [{version:1,connection:null,mode:'all'},{version:1,connection:null,mode:'auto',credential:'secret'}])expect(()=>decodeSearchPreference(raw)).toThrow()
})
it('does not expose credentials from malformed connection status',()=>{
 const provider={id:'tavily',name:'Tavily',kind:'search-results',fields:['api-key'],docs:'https://docs.tavily.com'}
 const connection={id:'demo',revision:'ab'.repeat(32),name:'Demo',provider:'tavily',dailyLimit:100}
 const status={version:1,providers:[provider],connections:[connection]}
 expect(decodeSearchStatus(status).connections).toHaveLength(1)
 expect(()=>decodeSearchStatus({...status,connections:[{...connection,credential:'secret'}]})).toThrow()
 expect(()=>decodeSearchStatus({...status,connections:[connection,connection]})).toThrow()
 expect(()=>decodeSearchStatus({...status,providers:[{...provider,fields:['shell-command']}]})).toThrow()
})

it('accepts legacy status and validates advertised timeout limits',()=>{
 const base={version:1,providers:[],connections:[]}
 expect(decodeSearchStatus(base).timeout).toBeUndefined()
 expect(decodeSearchStatus({...base,timeout:{minSeconds:10,maxSeconds:120,defaultSeconds:45}}).timeout?.maxSeconds).toBe(120)
 for(const timeout of [null,{minSeconds:10,maxSeconds:500,defaultSeconds:45},{minSeconds:10,maxSeconds:120,defaultSeconds:1},{minSeconds:10.5,maxSeconds:120,defaultSeconds:45}])expect(()=>decodeSearchStatus({...base,timeout})).toThrow()
})

// Review round 1, finding 5: a timeout outside the bounds is that connection's
// problem alone. It is kept, without the value and marked; the others are as sent.
it('holds each connection timeout to the advertised bounds, or to 10 to 120 seconds where none are advertised',()=>{
 const provider={id:'tavily',name:'Tavily',kind:'search-results',fields:['api-key'],docs:'https://docs.tavily.com'}
 const connection={id:'public',revision:'a'.repeat(64),name:'Public',provider:'tavily',dailyLimit:100}
 const other={...connection,id:'other',name:'Other',timeoutSeconds:30}
 const status=(timeoutSeconds:unknown,timeout?:object)=>({version:1,providers:[provider],connections:[{...connection,timeoutSeconds},other],...(timeout?{timeout}:{})})
 const refused=(decoded:ReturnType<typeof decodeSearchStatus>)=>{
  expect(decoded.connections).toHaveLength(2)
  expect(decoded.connections[0]).toEqual({...connection,timeoutRefused:true})
  expect(decoded.connections[1]).toEqual(other)
 }
 for(const seconds of [0,10,120]){const decoded=decodeSearchStatus(status(seconds));expect(decoded.connections[0]!.timeoutSeconds).toBe(seconds);expect(decoded.connections[0]).not.toHaveProperty('timeoutRefused')}
 for(const seconds of [9,121,45.5,'45'])refused(decodeSearchStatus(status(seconds)))
 const bounds={minSeconds:20,maxSeconds:60,defaultSeconds:30}
 for(const seconds of [20,60])expect(decodeSearchStatus(status(seconds,bounds)).connections[0]!.timeoutSeconds).toBe(seconds)
 for(const seconds of [19,61])refused(decodeSearchStatus(status(seconds,bounds)))
 // The mark is Desk's: a gateway that sends it is refused.
 expect(()=>decodeSearchStatus({version:1,providers:[provider],connections:[{...connection,timeoutRefused:true}]})).toThrow('Invalid search connection')
 for(const timeout of [{minSeconds:0,maxSeconds:120,defaultSeconds:45},{minSeconds:10,maxSeconds:121,defaultSeconds:45},{minSeconds:50,maxSeconds:40,defaultSeconds:45}])expect(()=>decodeSearchStatus({version:1,providers:[],connections:[],timeout})).toThrow('Invalid search timeout limits')
 expect(decodeSearchStatus({version:1,providers:[],connections:[],timeout:{minSeconds:1,maxSeconds:120,defaultSeconds:120}}).timeout?.maxSeconds).toBe(120)
})
