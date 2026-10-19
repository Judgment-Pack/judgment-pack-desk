import { act, cleanup, renderHook, waitFor } from '@testing-library/react'
import { afterEach, expect, it, vi } from 'vitest'
import { jobsAPI } from './client'
import { readRecentPage, useJournalReader } from './recentJournal'
import { mergeActivity } from './activity'
import type { JournalPage } from './journal'
vi.mock('./client',async original=>({...await original<typeof import('./client')>(),jobsAPI:vi.fn()}))
afterEach(()=>{cleanup();vi.resetAllMocks();vi.useRealTimers()})
const page=(start:number,end:number,more=false):JournalPage=>({items:Array.from({length:Math.max(0,end-start+1)},(_,i)=>({sequence:start+i,kind:'run.completed',at:'2026-01-01'})),next:end,before:start,more,journalBegan:'2026-01-01'})
it('shows the newest of thousands after one journal request and loads older only on demand',async()=>{
 vi.mocked(jobsAPI).mockImplementation(async path=>path==='status'?{capabilities:['journal-backward']}:path.endsWith('tail=true')?page(9951,10000,true):path.endsWith('before=9951')?page(9901,9950,true):Promise.reject(Error(path)))
 const {result}=renderHook(()=>useJournalReader('one',20000))
 await waitFor(()=>expect(result.current.state.phase).toBe('caught-up'))
 let state=result.current.state;if(!('snapshot' in state))throw Error('no snapshot')
 expect(state.snapshot!.entries[49]!.sequence).toBe(10000)
 expect(vi.mocked(jobsAPI).mock.calls.filter(([path])=>path.includes('/events')).map(([path])=>path)).toEqual(['jobs/one/events?tail=true'])
 act(()=>result.current.older());await waitFor(()=>expect('read' in result.current.state&&result.current.state.read).toBe(100))
 state=result.current.state;if(!('snapshot' in state))throw Error('no snapshot')
 expect(state.snapshot!.entries.map(entry=>entry.sequence)).toEqual(Array.from({length:100},(_,i)=>9901+i))
})
it('keeps the newest refresh cursor when older pages arrive, and retries failures from the same boundary',async()=>{
 let fail=true
 vi.mocked(jobsAPI).mockImplementation(async path=>{if(path==='status')return{capabilities:['journal-backward']};if(path.endsWith('tail=true'))return page(101,150,true);if(path.endsWith('before=101')){if(fail)throw Error('offline');return page(51,100,true)};throw Error(path)})
 const {result}=renderHook(()=>useJournalReader('one',20000));await waitFor(()=>expect(result.current.state.phase).toBe('caught-up'))
 act(()=>result.current.older());await waitFor(()=>expect(result.current.state.phase).toBe('stopped'))
 expect('next' in result.current.state&&result.current.state.next).toBe(150)
 fail=false;act(()=>result.current.resume());await waitFor(()=>expect('read' in result.current.state&&result.current.state.read).toBe(100))
 expect('next' in result.current.state&&result.current.state.next).toBe(150)
 expect(vi.mocked(jobsAPI).mock.calls.filter(([path])=>path.endsWith('before=101'))).toHaveLength(2)
})
it('uses legacy forward queries when the capability is absent',async()=>{
 vi.mocked(jobsAPI).mockImplementation(async path=>path==='status'?{capabilities:['activity-journal']}:page(1,2))
 const {result}=renderHook(()=>useJournalReader('old',20000));await waitFor(()=>expect(result.current.state.phase).toBe('caught-up'))
 expect(vi.mocked(jobsAPI).mock.calls.map(([path])=>path)).toEqual(['status','jobs/old/events?after=0'])
 expect('backward' in result.current.state).toBe(false)
})
it('does not mutate the held page after navigation aborts its request',async()=>{
 let release!:(page:JournalPage)=>void
 vi.mocked(jobsAPI).mockImplementation(async path=>path==='status'?{capabilities:['journal-backward']}:new Promise(resolve=>{release=resolve}))
 const {unmount}=renderHook(()=>useJournalReader('one',20000));await waitFor(()=>expect(release).toBeTypeOf('function'))
 const signal=vi.mocked(jobsAPI).mock.calls.at(-1)![3];unmount();expect(signal!.aborted).toBe(true);release(page(1,2))
})
it('refuses malformed pages instead of losing or rounding cursor identities',()=>{
 expect(()=>readRecentPage({...page(1,2),before:undefined},'tail',0)).toThrow()
 expect(()=>readRecentPage({...page(1,2),items:[{sequence:2},{sequence:1}]},'tail',0)).toThrow()
 expect(()=>readRecentPage(page(10,20),'before',15)).toThrow()
 expect(()=>readRecentPage({...page(1,2),next:Number.MAX_SAFE_INTEGER+1},'tail',0)).toThrow()
 expect(()=>readRecentPage({...page(1,0,true)},'tail',0)).toThrow()
})
it('keeps journal selection identities stable when older entries are prepended',()=>{
 const shown=(entries:ReturnType<typeof readRecentPage>)=>mergeActivity({},()=>true,{entries,dropped:0,from:0,show:true,sequence:true}).shown.map(row=>row.key)
 const recent=shown(readRecentPage(page(101,150),'tail',0)),expanded=shown(readRecentPage(page(51,100),'tail',0).concat(readRecentPage(page(101,150),'tail',0)))
 expect(expanded.slice(0,50)).toEqual(recent)
})
