import {msg} from '../i18n'
import type {ProfileEntry} from './dialectTypes'

/** An installed profile: an entry with no state member. One with a state,
 * whatever its value (set-aside, the one Runner lists, but also an empty
 * string or null), is shown with its reason and never offered or bound. */
export const installedEntry=(entry:ProfileEntry)=>entry.state===undefined

/** The listed entry Runner set aside under this id, if any. Runner refuses a
 * pin that names it whatever digest the pin names. */
export function setAsideEntry(entries:ProfileEntry[]|undefined,id:string|undefined):ProfileEntry|undefined {
 return id===undefined?undefined:entries?.find(entry=>entry.profile.id===id&&!installedEntry(entry))
}

/** Runner's reason as Desk shows it: without a leading "the input profile
 * <id>: " where it names the same profile, which the message names already,
 * without Runner's code at its end, and without a final stop. */
export function runnerReason(profile:string,reason?:string):string {
 let text=(reason??'').trim()
 const named=`the input profile ${profile}: `
 if(profile&&text.startsWith(named))text=text.slice(named.length)
 return text.replace(/\s*\([a-z][a-z0-9_-]*\)$/,'').trim().replace(/\.$/,'')
}

/** What a picker or a review shows for a set-aside profile. */
export function setAsideNote(entry:Pick<ProfileEntry,'profile'|'reason'>):string {
 const reason=runnerReason(entry.profile.id,entry.reason)
 return reason?msg('Set aside by Runner. Runner’s reason: {{reason}}',{reason}):msg('Set aside by Runner.')
}

/** Desk's refusal of a binding to a set-aside profile: it names the profile,
 * gives Runner's reason, labelled as Runner's, and says what the user can do.
 * Only where the binding is a job's does it also offer upgrading the job. */
export function setAsideRefusal(profile:string,reason:string|undefined,job:boolean):string {
 const shown=runnerReason(profile,reason)
 if(job)return shown?msg('The input profile {{profile}} was set aside by Runner and cannot be bound. Runner’s reason: {{reason}}. Choose another profile for this source, or upgrade the job to a release that does not name it.',{profile,reason:shown})
  :msg('The input profile {{profile}} was set aside by Runner and cannot be bound. Choose another profile for this source, or upgrade the job to a release that does not name it.',{profile})
 return shown?msg('The input profile {{profile}} was set aside by Runner and cannot be bound. Runner’s reason: {{reason}}. Choose another profile for this source.',{profile,reason:shown})
  :msg('The input profile {{profile}} was set aside by Runner and cannot be bound. Choose another profile for this source.',{profile})
}

/** A registration's answer, bound only when it is an installed profile. */
export function boundEntry(entry:ProfileEntry|undefined):ProfileEntry {
 if(!entry)throw Error(msg('The request could not be completed.'))
 if(!installedEntry(entry))throw Error(setAsideRefusal(entry.profile.id,entry.reason,false))
 return entry
}
