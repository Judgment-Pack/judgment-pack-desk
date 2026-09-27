import {readFileSync} from 'node:fs'
import {join} from 'node:path'
import {cleanup,fireEvent,screen,waitFor} from '@testing-library/react'
import {afterEach,expect,it,vi} from 'vitest'
import {chassis,drawPack,forgetSlot,served,PACK_DIGEST} from './editHarness'
const original=readFileSync(join(import.meta.dirname,'../__fixtures__/full.pack.json'),'utf8')
afterEach(()=>{cleanup();forgetSlot();vi.unstubAllGlobals();vi.restoreAllMocks();localStorage.clear()})
it('edits a selected rule beside Logic and saves through the existing guarded file writer',async()=>{
 const disk=chassis({content:original,sha256:PACK_DIGEST})
 drawPack(served(original),{inspector:true,path:'/packs/vendor-onboarding?view=logic&layout=list&at=%2Frules%2F0'})
 fireEvent.click(await screen.findByRole('button',{name:'Edit rule'}))
 await screen.findByRole('button',{name:'Back to pack'})
 expect(screen.getByRole('region',{name:'Pack logic'})).toBeTruthy()
 const input=await screen.findByLabelText('Description',{exact:true})
 fireEvent.change(input,{target:{value:'A focused rule edit'}})
 expect(disk.writes).toHaveLength(0)
 // Inspecting another item must not leave the old rule editor on screen or lose its draft.
 fireEvent.click(screen.getByRole('button',{name:'View details: Fallback outcome'}))
 await screen.findAllByRole('heading',{name:'Fallback outcome',level:2})
 expect(screen.queryByLabelText('Description',{exact:true})).toBeNull()
 fireEvent.click(screen.getByRole('button',{name:/View details: screen first/i}))
 expect((await screen.findByLabelText('Description',{exact:true}) as HTMLTextAreaElement).value).toBe('A focused rule edit')
 const save=screen.getByRole('button',{name:'Save'})
 await waitFor(()=>expect((save as HTMLButtonElement).disabled).toBe(false))
 fireEvent.click(save)
 await waitFor(()=>expect(disk.writes).toHaveLength(1))
 const result=JSON.parse(disk.writes[0]!.content),before=JSON.parse(original)
 expect(result.rules[0].description).toBe('A focused rule edit')
 expect(result.rules.slice(1)).toEqual(before.rules.slice(1))
 expect(result.title).toBe(before.title)
 expect(disk.writes[0]!.baseSha256).toBe(PACK_DIGEST)
 expect(disk.writes[0]!.override).toBe(false)
})
