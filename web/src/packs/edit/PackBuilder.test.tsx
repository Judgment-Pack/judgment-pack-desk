import {cleanup, fireEvent, screen, waitFor, within} from '@testing-library/react'
import {readFileSync} from 'node:fs'
import {join} from 'node:path'
import {afterEach, expect, it, vi} from 'vitest'
import {chassis, drawPack, forgetSlot, served, FIRST_DRAW, PACK_DIGEST} from './editHarness'
const text=readFileSync(join(import.meta.dirname,'..','__fixtures__','full.pack.json'),'utf8')
afterEach(()=>{cleanup();forgetSlot();vi.unstubAllGlobals()})
it('edits a selected rule below Build without opening Details and preserves its bytes in Source',async()=>{
 chassis({content:text,sha256:PACK_DIGEST});const {revealed}=drawPack(served(text),{path:'/packs/vendor-onboarding?edit=1&layout=list'})
 const logic=await screen.findByRole('region',{name:'Pack logic'},FIRST_DRAW)
 const rule=within(logic).getAllByRole('button',{name:/View details:/}).find(button=>button.closest('[data-group="rules"]'))!
 fireEvent.click(rule)
 await screen.findByRole('separator',{name:'Resize editor'})
 const editor=screen.getByRole('button',{name:'Close editor'}).closest('section')!
 const description=within(editor).getByLabelText('Description')
 fireEvent.change(description,{target:{value:'Draft selection edit'}})
 expect(revealed).toEqual([])
 fireEvent.click(screen.getByRole('button',{name:'Source'}))
 const raw=await screen.findByLabelText("The document's bytes")
 await waitFor(()=>expect(raw).toHaveProperty('value',expect.stringContaining('Draft selection edit')))
 fireEvent.click(screen.getByRole('button',{name:'Build'}))
 expect(screen.getByDisplayValue('Draft selection edit')).toBeTruthy()
})
it('adds one rule through the existing buffer and opens its bottom editor',async()=>{
 chassis({content:text,sha256:PACK_DIGEST});drawPack(served(text),{path:'/packs/vendor-onboarding?edit=1&layout=list'})
 await screen.findByRole('region',{name:'Pack logic'},FIRST_DRAW)
 fireEvent.click(screen.getByRole('button',{name:'Add item'}));fireEvent.click(screen.getByRole('button',{name:'Rule'}))
 await screen.findByRole('separator',{name:'Resize editor'})
 fireEvent.click(screen.getByRole('button',{name:'Source'}));const raw=await screen.findByLabelText("The document's bytes") as HTMLTextAreaElement
 expect(JSON.parse(raw.value).rules).toHaveLength(JSON.parse(text).rules.length+1)
 fireEvent.click(screen.getByRole('button',{name:'Undo'}));expect(JSON.parse(raw.value).rules).toHaveLength(JSON.parse(text).rules.length)
})

it('retains an unfinished rule operand across Build, Settings and Source',async()=>{
 const doc=JSON.parse(text);doc.rules[0].when={op:'fact',path:'/tag',operator:'equals',value:'green'}
 const source=JSON.stringify(doc);chassis({content:source,sha256:PACK_DIGEST});drawPack(served(source),{path:'/packs/vendor-onboarding?edit=1&layout=list&editItem=/rules/0'})
 const operand=await screen.findByDisplayValue('"green"',undefined,FIRST_DRAW)
 fireEvent.change(operand,{target:{value:'{"unfinished"'}})
 fireEvent.click(screen.getByRole('button',{name:'Settings'}));expect(await screen.findByDisplayValue('{"unfinished"')).toBeTruthy()
 fireEvent.click(screen.getByRole('button',{name:'Source'}));const raw=await screen.findByLabelText("The document's bytes") as HTMLTextAreaElement
 expect(JSON.parse(raw.value).rules[0].when.value).toBe('green')
 fireEvent.click(screen.getByRole('button',{name:'Build'}));expect(await screen.findByDisplayValue('{"unfinished"')).toBeTruthy()
})
it('shows malformed selected items as their bytes rather than editable fields',async()=>{
 const doc=JSON.parse(text);doc.rules[0]=null
 const source=JSON.stringify(doc);chassis({content:source,sha256:PACK_DIGEST});drawPack(served(source),{path:'/packs/vendor-onboarding?edit=1&layout=list&editItem=/rules/0'})
 await screen.findByRole('button',{name:'Close editor'},FIRST_DRAW)
 const editor=screen.getByRole('button',{name:'Close editor'}).closest('section')!
 expect(within(editor).getByText(/not the shape this page draws/)).toBeTruthy()
 expect(within(editor).queryByRole('textbox')).toBeNull()
})
