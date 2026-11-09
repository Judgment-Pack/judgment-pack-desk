import {afterEach,expect,it,vi} from 'vitest'
import {loadAssistantProfile,loadDeskLevelConfig} from './queries'

afterEach(()=>vi.unstubAllGlobals())
it('reads the released prompt byte bound from process metadata in either file state',async()=>{
 for(const present of [false,true]){
  vi.stubGlobal('fetch',async()=>new Response(JSON.stringify({path:'/settings/desk.json',present,...(present?{content:'{"deskConfigVersion":1}'}:{}),sha256:'',promptPolicy:{projectInstructionsBytes:4000}})))
  expect((await loadDeskLevelConfig()).promptPolicy).toEqual({projectInstructionsBytes:4000})
 }
})
it('refuses instructions over the reported bound while retaining invalid file bytes for repair',async()=>{
 const content=JSON.stringify({profileVersion:1,projectInstructions:'😀'.repeat(1000)+'x'})
 vi.stubGlobal('fetch',async()=>new Response(JSON.stringify({path:'jpack-assistant.json',content,sha256:'a'.repeat(64),bytes:content.length})))
 const read=await loadAssistantProfile(undefined,4000)
 expect(read.problem).toBeTruthy();expect(read.value).toBeUndefined();expect(read.text).toBe(content)
 expect(read.sha256).toBe('a'.repeat(64))
})
it('retains saved instructions when process metadata has not answered',async()=>{
 const value={profileVersion:2,inherit:true,projectInstructions:'Exact saved text.\n'}
 const content=JSON.stringify(value)
 vi.stubGlobal('fetch',async()=>new Response(JSON.stringify({path:'jpack-assistant.json',content,sha256:'a'.repeat(64),bytes:content.length})))
 expect((await loadAssistantProfile()).value).toEqual(value)
})
