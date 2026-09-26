import { beforeEach, expect, it, vi } from 'vitest'
import { prepareMappedInputs, parseMappedObject, mappedCase } from './mappedInputs'
import { jobsRequest, RetainedJSON } from './wire'
import { jobsAPI } from './client'
import { acquire, seal } from '../research/gatewayClient'
import type { Acquired } from '../research/gatewayClient'
import { DESK_DEFAULTS } from '../config/deskConfig'
import type { MappingV2, ProfileEntry } from './mappingTypes'
vi.mock('./client',()=>({jobsAPI:vi.fn()}))
vi.mock('../research/gatewayClient',()=>({acquire:vi.fn(),seal:vi.fn(),newResearchSession:()=> 'one-session'}))
const profile: ProfileEntry={digest:'sha256:pin',profile:{id:'records',publicKey:'ab'.repeat(32),authority:'gateway:test',source:'records',class:'record',shape:'mcp',adapter:{name:'mcp',version:'1',digest:'sha256:adapter'},endpoint:null,tools:['lookup']}}
const mapping: MappingV2={version:2,sources:[{name:'first',kind:'operation',profile:'records',profileDigest:profile.digest,maxAge:300,arguments:{tool:'lookup',arguments:{}},read:{copy:{facts:[{target:'/id',source:'/id'}],evidence:[]}}},{name:'second',kind:'operation',profile:'records',profileDigest:profile.digest,maxAge:300,arguments:{tool:'lookup',arguments:{id:{$param:'id'}}},parameters:{id:{from:'first',pointer:'/id',type:'integer'}},read:{copy:{facts:[{target:'/name',source:'/name'}],evidence:[]}}}]}
const config={...DESK_DEFAULTS.research,gateway:{url:'http://127.0.0.1:1',authority:profile.profile.authority,signer:{algorithm:'ed25519' as const,public:profile.profile.publicKey}}}
const options=()=>({mapping,caseValue:{},files:{},selections:{},config,profiles:[profile],signal:new AbortController().signal,progress:vi.fn()})
beforeEach(()=>{vi.resetAllMocks();vi.mocked(seal).mockResolvedValue(undefined as never)})
it('retains signed numeric spelling, rejects duplicate keys and bounds the wire body',()=>{
 const text='{"result":{"large":9007199254740993,"fraction":1.0000},"receipt":{}}'
 expect(jobsRequest({response:new RetainedJSON(text)})).toBe(`{"response":${text}}`)
 expect(()=>new RetainedJSON('{"x":1,"x":2}')).toThrow()
 expect(()=>jobsRequest({x:'a'.repeat(2<<20)})).toThrow(/2 MiB/)
 expect(()=>parseMappedObject('{"id":9007199254740993}')).toThrow()
 expect(parseMappedObject('{"id":7}')).toEqual({id:7})
})
it('uses only Runner-expanded requests and one session, preserving exact responses in the final input',async()=>{
 const raw='{"result":{"id":7,"unused":1.000},"receipt":{},"salts":{}}'
 vi.mocked(acquire).mockResolvedValue({text:raw} as Acquired)
 vi.mocked(jobsAPI).mockResolvedValueOnce({next:{name:'first',kind:'operation',profile:'records',source:'records',arguments:{tool:'lookup',arguments:{}}}}).mockResolvedValueOnce({next:{name:'second',kind:'operation',profile:'records',source:'records',arguments:{tool:'lookup',arguments:{id:7}}}}).mockResolvedValueOnce({input:{source:{mappingDigest:'digest'},facts:{id:7}},factsText:'{"id":7}'})
 const result=await prepareMappedInputs(options())
 expect(acquire).toHaveBeenNthCalledWith(2,'one-session','records',{tool:'lookup',arguments:{id:7}},1<<20,expect.any(AbortSignal),undefined)
 expect(jobsRequest(result.input)).toContain(raw)
 expect(jobsAPI).toHaveBeenCalledTimes(3)
 expect(seal).toHaveBeenCalledWith('one-session',expect.any(AbortSignal),undefined)
})
it('stops before a dependent acquisition when Runner rejects the prefix',async()=>{
 vi.mocked(acquire).mockResolvedValue({text:'{"result":{},"receipt":{}}'} as Acquired)
 vi.mocked(jobsAPI).mockResolvedValueOnce({next:{name:'first',kind:'operation',profile:'records',source:'records',arguments:{tool:'lookup',arguments:{}}}}).mockRejectedValueOnce(Error('invalid signature'))
 await expect(prepareMappedInputs(options())).rejects.toThrow('invalid signature')
 expect(acquire).toHaveBeenCalledTimes(1)
})
it('does not fetch a skipped dependent source or invoke operational evaluation',async()=>{
 vi.mocked(jobsAPI).mockResolvedValue({input:{facts:{},source:{mappingDigest:'digest'}},factsText:'{}'})
 await prepareMappedInputs(options())
 expect(acquire).not.toHaveBeenCalled();expect(seal).not.toHaveBeenCalled()
 expect(jobsAPI).toHaveBeenCalledWith('inputs/next',expect.anything(),undefined,expect.any(AbortSignal))
})
it('rejects changed relay pins and profile digests before any acquisition',async()=>{
 for(const modified of [{...config,gateway:null},{...config,gateway:{...config.gateway,authority:'different'}}]) {
  await expect(prepareMappedInputs({...options(),config:modified})).rejects.toThrow(/profile/)
 }
 await expect(prepareMappedInputs({...options(),profiles:[{...profile,digest:'changed'}]})).rejects.toThrow(/profile/)
 expect(jobsAPI).not.toHaveBeenCalled();expect(acquire).not.toHaveBeenCalled()
})
it('ignores late replies after cancellation',async()=>{
 const controller=new AbortController()
 vi.mocked(jobsAPI).mockImplementation(async()=>{controller.abort();return {input:{facts:{}}} as never})
 await expect(prepareMappedInputs({...options(),signal:controller.signal})).rejects.toThrow()
 expect(acquire).not.toHaveBeenCalled()
})
it('places picker values only at explicit typed case parameters',()=>{
 const drive: MappingV2={version:2,case:{parameters:{file:{pointer:'/picked/id',type:'string'},grant:{pointer:'/picked/grant',type:'string'}},facts:[],evidence:[]},sources:[{name:'drive',kind:'selected-file',provider:'google-drive',arguments:{fileId:{$param:'file'},grant:{$param:'grant'}},read:{copy:{facts:[],evidence:[]}}}]}
 expect(mappedCase(drive,{subject:'test'},{drive:{fileId:'selected',grant:'grant'}})).toEqual({subject:'test',picked:{id:'selected',grant:'grant'}})
 drive.case!.parameters!.file!.pointer='/__proto__/polluted'
 expect(()=>mappedCase(drive,{}, {drive:{fileId:'selected',grant:'grant'}})).toThrow()
 expect(({} as Record<string,unknown>).polluted).toBeUndefined()
})
