import { act, cleanup, render, screen } from '@testing-library/react'
import { afterEach, beforeEach, expect, it, vi } from 'vitest'
import { CONNECTION_PREFERENCES_KEY, connectionShortcuts, pinConnection, rememberConnection, useConnectionPreferences } from './preferences'

function Observer() { const state=useConnectionPreferences(); return <output>{JSON.stringify(state)}</output> }
const read=()=>JSON.parse(screen.getByRole('status').textContent!)
beforeEach(()=>{ localStorage.clear(); window.dispatchEvent(new StorageEvent('storage',{key:CONNECTION_PREFERENCES_KEY})) })
afterEach(()=>{ cleanup(); vi.restoreAllMocks() })
it('limits shortcuts to connected advertised providers, orders pins and recents, and deduplicates',()=>{
 const available=Array.from({length:8},(_,i)=>({provider:`files-${i}`}))
 expect(connectionShortcuts(available,{pinned:['disconnected','files-5','files-2'],recent:['files-6','files-5','removed','files-1']})).toEqual([available[5],available[2],available[6],available[1],available[0]])
})
it('does not write on discovery, caps pins, and updates subscribers only on explicit actions',()=>{
 const write=vi.spyOn(Storage.prototype,'setItem'); render(<Observer/>); expect(write).not.toHaveBeenCalled()
 const ids=Array.from({length:6},(_,i)=>`files-${i}`)
 act(()=>ids.forEach(id=>pinConnection(id,true,ids)))
 expect(read().pinned).toEqual(ids.slice(0,5))
 act(()=>{pinConnection('files-0',false,ids); pinConnection('files-5',true,ids); rememberConnection('files-2'); rememberConnection('files-2')})
 expect(read().pinned).toEqual(ids.slice(1)); expect(read().recent).toEqual(['files-2'])
 act(()=>pinConnection('unadvertised',true,ids)); expect(read().pinned).toEqual(ids.slice(1))
})
it('validates malformed preferences and responds to cross-tab reset',()=>{
 localStorage.setItem(CONNECTION_PREFERENCES_KEY,JSON.stringify({pinned:['files-1','files-1','bad/id',null,{}],recent:'broken'}))
 render(<Observer/>); expect(read()).toEqual({pinned:['files-1'],recent:[]})
 act(()=>{localStorage.clear(); window.dispatchEvent(new StorageEvent('storage',{key:null}))})
 expect(read()).toEqual({pinned:[],recent:[]})
})
it('retains explicit preferences in memory when browser storage rejects writes',()=>{
 render(<Observer/>); vi.spyOn(Storage.prototype,'setItem').mockImplementation(()=>{throw new Error('blocked')})
 act(()=>{pinConnection('files-one',true,['files-one']);rememberConnection('files-one')})
 expect(read()).toEqual({pinned:['files-one'],recent:['files-one']})
})
