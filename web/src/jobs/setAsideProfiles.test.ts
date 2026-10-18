import {expect,it} from 'vitest'
import {boundEntry,installedEntry,runnerReason} from './setAsideProfiles'
import type {ProfileEntry} from './dialectTypes'

const entry=(extra:Partial<ProfileEntry>):ProfileEntry=>({digest:'sha256:d',profile:{id:'pages'} as ProfileEntry['profile'],...extra})
it('takes an entry as installed only where its state member is absent',()=>{
 expect(installedEntry(entry({}))).toBe(true)
 for(const state of ['','set-aside',null]){
  expect(installedEntry(entry({state}))).toBe(false)
  expect(()=>boundEntry(entry({state}))).toThrow('The input profile pages was set aside by Runner and cannot be bound. Choose another profile for this source.')
 }
})
it('shows Runner\'s reason without the profile it names already, its code or a final stop',()=>{
 expect(runnerReason('pages','the input profile pages: an entry is refused (input-malformed)')).toBe('an entry is refused')
 expect(runnerReason('pages','the input profile other: an entry is refused.')).toBe('the input profile other: an entry is refused')
 expect(runnerReason('pages',undefined)).toBe('')
})
