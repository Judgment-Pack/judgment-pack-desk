import { expect, it } from 'vitest'
import { editableJSON, parseInput } from './inputModel'
import { jobsRequest } from './wire'
it('preserves false, zero, null and omitted evidence without adding defaults',()=>{
 expect(parseInput('{"flag":false,"amount":0,"value":null}',false,'not JSON')).toEqual({facts:{flag:false,amount:0,value:null}})
 expect(parseInput('{}',true,'{}')).toEqual({facts:{},evidence:{}})
})
it('preserves exact numbers on the wire and refuses lossy form editing',()=>{
 const raw='{"amount":9007199254740993,"ratio":0.1234567890123456789}'
 expect(jobsRequest(parseInput(raw,false,''))).toBe('{"facts":'+raw+'}')
 expect(()=>editableJSON(raw)).toThrow('exact numeric values')
})
it('rejects duplicate keys, invalid evidence and malformed JSON',()=>{
 for(const raw of ['{','{"x":1,"x":2}'])expect(()=>parseInput(raw,false,'')).toThrow()
 for(const raw of ['[]','null','{"proof":true}','{"proof":"maybe"}','{"x":"present","x":"absent"}'])expect(()=>parseInput('{}',true,raw)).toThrow()
})
