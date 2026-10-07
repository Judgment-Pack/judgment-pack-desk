import { expect,it } from 'vitest'
import fixtures from './fixtures/assistant-profiles.json'
import { applyAssistantProfile, decodeAssistantProfile } from './assistantProfile'
import { DESK_DEFAULTS, type AssistantConfig } from './deskConfig'
for(const fixture of fixtures)it(fixture.name,()=>{
 const read=()=>decodeAssistantProfile(JSON.stringify(fixture.value))
 if(fixture.accepted)expect(read()).toEqual(fixture.value);else expect(read).toThrow()
})
it('overlays only model preferences and never mutates shared defaults',()=>{
 const machine:AssistantConfig={...DESK_DEFAULTS.assistant,engine:'codex',agent:{provider:'openai',authMethod:'subscription',model:'machine',models:['machine'],effort:'high',tools:['get_schema']},endpoint:{kind:'gemini',url:'https://example.invalid',model:'machine-api',models:['machine-api'],tools:[]}}
 const profile=decodeAssistantProfile('{"profileVersion":1,"codex":{"inherit":false,"models":["desk"],"model":"desk"},"api":{"inherit":false,"models":["desk-api"],"model":"desk-api","thinking":"on"}}')
 const result=applyAssistantProfile(machine,{present:true,value:profile})
 expect(result.agent).toEqual({...machine.agent,models:['desk'],model:'desk',effort:undefined})
 expect(result.endpoint).toEqual({...machine.endpoint,models:['desk-api'],model:'desk-api'})
 expect(result.thinking).toBe('on');expect(result.engine).toBe('codex')
 expect(machine.agent?.model).toBe('machine');expect(machine.endpoint?.model).toBe('machine-api')
 expect(applyAssistantProfile(machine,{present:true,value:{profileVersion:1,codex:{inherit:true}}})).toEqual(machine)
})
it('reads a profile up to the desk\'s 64 KiB bound and refuses one byte more, in both versions, as the desk does',()=>{
 // The page writes only what this decoder accepts, and the desk reads at most
 // 65,536 bytes of jpack-assistant.json: the writer refuses what the reader would.
 for(const head of ['{"profileVersion":1}','{"profileVersion":2,"inherit":true}']){
  const at=head+' '.repeat(65536-head.length)
  expect(decodeAssistantProfile(at)).toEqual(JSON.parse(head))
  expect(()=>decodeAssistantProfile(at+' ')).toThrow()
 }
})
