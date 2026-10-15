import { mappingFixture } from '../dialect/__fixtures__/desk-mappings'
import { beforeEach, expect, it, vi } from 'vitest'
import { prepareMappedInputs, parseMappedObject, parseMapping, mappedCase } from './mappedInputs'
import { jobsRequest, RetainedJSON } from './wire'
import { jobsAPI } from './client'
import { parseMapping as legacyParse, prepareMappedInputs as legacyPrepare } from '../dialect/__fixtures__/legacy-producer/producer/mappedInputs'
import { jobsAPI as legacyAPI } from '../dialect/__fixtures__/legacy-producer/producer/client'
import { acquire, seal } from '../research/gatewayClient'
import type { Acquired } from '../research/gatewayClient'
import { ConnectionRequestError } from '../connections/client'
import { DESK_DEFAULTS } from '../config/deskConfig'
import type { DialectV2, ProfileEntry } from './dialectTypes'
vi.mock('./client',()=>({jobsAPI:vi.fn()}))
vi.mock('../dialect/__fixtures__/legacy-producer/producer/client',()=>({jobsAPI:vi.fn()}))
vi.mock('../research/gatewayClient',()=>({acquire:vi.fn(),seal:vi.fn(),newResearchSession:()=> 'one-session'}))
const profile: ProfileEntry={digest:'sha256:pin',profile:{id:'records',publicKey:'ab'.repeat(32),authority:'gateway:test',source:'records',class:'record',shape:'mcp',adapter:{name:'mcp',version:'1',digest:'sha256:adapter'},endpoint:null,tools:['lookup']}}
const mapping: DialectV2=mappingFixture('sequential')
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
it('restores the legacy default sources while retaining explicit members',()=>{
 for(const text of ['{"version":2,"case":{"facts":[],"evidence":[]}}','{"version":2,"case":{"facts":[],"evidence":[]},"provider":"","facts":null,"evidence":null}']) {
  expect(parseMapping(text)).toEqual({...JSON.parse(text),sources:[]})
 }
 expect(parseMapping('{"version":2}')).toEqual({version:2,sources:[]})
 expect(()=>parseMapping('{"version":2,"case":{"facts":[],"evidence":[]},"sources":null}')).toThrow()
 expect(()=>parseMapping('{"version":2,"version":2,"case":{"facts":[],"evidence":[]}}')).toThrow(/duplicate_member/)
 expect(Object.is(mappedCase(mapping,{zero:-0},{}).zero,-0)).toBe(false)
})
it('accepts the legacy sequential mapping without case',()=>{
 expect(mapping).not.toHaveProperty('case')
 expect(parseMapping(JSON.stringify(mapping))).toEqual(legacyParse(JSON.stringify(mapping)))
})
it.each([{}, {facts:[]}, {evidence:[]}])('refuses partial copy arrays as the legacy producer did: %j',copy=>{
 expect(()=>parseMapping(JSON.stringify({version:2,sources:[{name:'local',kind:'selected-file',provider:'local-file',read:{copy}}]}))).toThrow('The inputs could not be mapped.')
 expect(()=>parseMapping(JSON.stringify({version:2,case:copy}))).toThrow('The inputs could not be mapped.')
 expect(()=>legacyParse(JSON.stringify({version:2,sources:[{name:'local',kind:'selected-file',provider:'local-file',read:{copy}}]}))).toThrow('The inputs could not be mapped.')
 expect(()=>legacyParse(JSON.stringify({version:2,case:copy}))).toThrow('The inputs could not be mapped.')
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
it('leaves a missing Runner mapping digest undefined, as the legacy producer did',async()=>{
 vi.mocked(jobsAPI).mockResolvedValue({input:{facts:{}},factsText:'{}'})
 vi.mocked(legacyAPI).mockResolvedValue({input:{facts:{}},factsText:'{}'})
 const result=await prepareMappedInputs(options())
 expect(result).toEqual(await legacyPrepare({...options(),mapping:legacyParse(JSON.stringify(mapping)),profiles:[{...profile,profile:{...profile.profile,shape:'mcp'}}]}))
 expect(result.input.source?.mappingDigest).toBeUndefined()
 expect(jobsRequest(result.input)).not.toContain('"mappingDigest"')
 expect(vi.mocked(jobsAPI).mock.calls[0][1]).toEqual(expect.objectContaining({source:expect.not.objectContaining({mappingDigest:expect.anything()})}))
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
 const drive: DialectV2=mappingFixture('drive-selection')
 expect(mappedCase(drive,{subject:'test'},{drive:{fileId:'selected',grant:'grant'}})).toEqual({subject:'test',picked:{id:'selected',grant:'grant'}})
 drive.case!.parameters!.file!.pointer='/__proto__/polluted'
 expect(()=>mappedCase(drive,{}, {drive:{fileId:'selected',grant:'grant'}})).toThrow()
 expect(({} as Record<string,unknown>).polluted).toBeUndefined()
})

it('normalizes a Drive acquisition reconnect refusal and still seals the abandoned session',async()=>{
 const drive:DialectV2=mappingFixture('drive-reconnect')
 vi.mocked(jobsAPI).mockResolvedValue({next:{name:'drive',kind:'selected-file',provider:'google-drive',profile:'records',source:'records',arguments:{fileId:'chosen',grant:'grant'}}})
 vi.mocked(acquire).mockRejectedValue(Error('source failed: reconnect-required'))
 await expect(prepareMappedInputs({...options(),mapping:drive,selections:{drive:{fileId:'chosen',grant:'grant'}},config:{...config,managedLocal:true,documents:{enabled:true,source:'documents',maxFileBytes:200000,maxRequestBytes:1000000,maxResponseBytes:1000000}}})).rejects.toEqual(new ConnectionRequestError('reconnect-required','google-drive'))
 expect(seal).toHaveBeenCalledWith('one-session',expect.any(AbortSignal),'local-documents')
})
