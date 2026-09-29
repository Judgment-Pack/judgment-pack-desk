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
