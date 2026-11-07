import {act,cleanup,fireEvent,screen,waitFor} from '@testing-library/react'
import {readFileSync} from 'node:fs'
import {join} from 'node:path'
import {afterEach,expect,it,vi} from 'vitest'
import type {EditingSession} from './editingContext'
import type {BufferIdentity} from './useDocumentBuffer'
import type {useFileEditing} from '../../files/useFileEditing'
import {applyProposal} from '../../assistant/acceptProposal'
import {ownerOf} from './editingContext'
import {buffered,bytesAt} from './writes'
import {chassis,drawPack,forgetSlot,served,FIRST_DRAW,PACK_DIGEST,PACK_PATH} from './editHarness'

const live = vi.hoisted(() => ({} as {session:EditingSession;identity:BufferIdentity;draft:string;editor:ReturnType<typeof useFileEditing>}))
// Render the pane where the route provides its real editing context. No model
// or proposal workflow runs; these fixtures exercise the editor-owned boundary.
vi.mock('../../shell/InspectorSlot', async original => ({...await original<typeof import('../../shell/InspectorSlot')>(),useInspectorPortal:(node:unknown)=>node}))
vi.mock('../../chat/PackAssistant', async () => {
  const {useEditing} = await import('./editingContext')
  return {PackAssistant:({identity,draft}: {identity:BufferIdentity;draft:string}) => {
    live.session=useEditing();live.identity=identity;live.draft=draft;return null
  }}
})
vi.mock('../../files/useFileEditing', async original => {
  const actual=await original<typeof import('../../files/useFileEditing')>()
  return {...actual,useFileEditing:()=>{const editor=actual.useFileEditing();live.editor=editor;return editor}}
})
const original=readFileSync(join(import.meta.dirname,'../__fixtures__/full.pack.json'),'utf8')
const proposal={...JSON.parse(original),title:'Proposed onboarding'}
afterEach(()=>{cleanup();forgetSlot();vi.unstubAllGlobals()})
async function open() {
  // The mocked pane is absent while the next route is loading; never reuse a
  // previous test's captured editor as evidence that this route is ready.
  live.session = undefined!
  const log=chassis({content:original,sha256:PACK_DIGEST,holdWrite:true})
  const view=drawPack(served(original),{path:'/packs/vendor-onboarding?edit=1&builder=build'})
  await waitFor(()=>expect(live.session?.editing).toBe(true),FIRST_DRAW)
  return {log,...view}
}
function captureApply() {
  const {session,identity,draft}=live
  return ()=>session.writeIfCurrent!(identity,draft,current=>applyProposal(current,proposal))
}
it('refuses a captured proposal after a same-tick manual edit without adding an undo entry',async()=>{
  await open()
  const apply=captureApply()
  act(()=>{
    live.session.write(current=>applyProposal(current,{...JSON.parse(original),title:'Manual onboarding'}))
    expect(apply()).toEqual({applied:false})
  })
  expect(JSON.parse(live.draft).title).toBe('Manual onboarding')
  fireEvent.click(screen.getByRole('button',{name:'Undo'}))
  expect(live.draft).toBe(original)
  expect(screen.getByRole('button',{name:'Undo'}).hasAttribute('disabled')).toBe(true)
})
it('refuses and preserves pending operands held before the next render',async()=>{
  await open()
  const apply=captureApply(),pointer='/rules/1/when/conditions/0/value',read=buffered(original)
  const pending={text:'  {"unfinished\n',from:bytesAt(read,pointer)!,owner:ownerOf(read,pointer)}
  act(()=>{
    live.session.hold(pointer,pending)
    expect(apply()).toEqual({applied:false})
  })
  expect(live.draft).toBe(original)
  expect(live.session.pending.get(pointer)).toEqual(pending)
  expect(captureApply()()).toEqual({applied:false})
})
it('refuses the live reload latch before its state render',async()=>{
  const {log}=await open()
  const apply=captureApply(),release=log.hold(PACK_PATH)
  act(()=>{
    live.editor.reload(PACK_PATH,()=>true)
    expect(apply()).toEqual({applied:false})
  })
  expect(live.draft).toBe(original)
  release()
  await waitFor(()=>expect(live.editor.isReloading()).toBe(false))
})
it('refuses a save claimed in the same turn and a form that has become Source',async()=>{
  const {log}=await open()
  act(()=>live.session.write(current=>applyProposal(current,{...JSON.parse(original),title:'Manual onboarding'})))
  const apply=captureApply()
  act(()=>{
    fireEvent.click(screen.getByRole('button',{name:'Save'}))
    expect(apply()).toEqual({applied:false})
  })
  expect(JSON.parse(live.draft).title).toBe('Manual onboarding')
  log.releaseWrite()
  await waitFor(()=>expect(live.editor.write.isPending).toBe(false))
  fireEvent.click(screen.getByRole('button',{name:'Source'}))
  await waitFor(()=>expect(live.session.editing).toBe(false))
  expect(apply()).toEqual({applied:false})
})
it('confirms one application, preserves untouched formatting, and needs one Undo',async()=>{
  await open()
  const apply=captureApply()
  act(()=>expect(apply()).toMatchObject({applied:true}))
  expect(JSON.parse(live.draft)).toEqual(proposal)
  expect(live.draft.replace('Proposed onboarding','Vendor onboarding')).toBe(original)
  fireEvent.click(screen.getByRole('button',{name:'Undo'}))
  expect(live.draft).toBe(original)
  expect(screen.getByRole('button',{name:'Undo'}).hasAttribute('disabled')).toBe(true)
  expect(apply()).toEqual({applied:false})
})
