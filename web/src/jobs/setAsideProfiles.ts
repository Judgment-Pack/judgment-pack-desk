import {msg} from '../i18n'
import type {ProfileEntry} from './dialectTypes'

/** An installed profile: an entry with no state. One with any state, of which
 * Runner lists set-aside, is shown with its reason and never offered or bound. */
export const installedEntry=(entry:ProfileEntry)=>!entry.state

/** What a picker shows for a set-aside profile, in place of choosing it. */
export function setAsideNote(entry:Pick<ProfileEntry,'reason'>):string {
 return entry.reason?msg('Set aside by Runner: {{reason}}',{reason:entry.reason}):msg('Set aside by Runner.')
}

/** Desk's refusal of a binding to a set-aside profile: it names the profile,
 * gives Runner's reason, and says what the user can do. */
export function setAsideRefusal(profile:string,reason?:string):string {
 return reason?msg('The input profile {{profile}} was set aside by Runner and cannot be bound: {{reason}}. Choose another profile for this source, or upgrade the job to a release that does not name it.',{profile,reason})
  :msg('The input profile {{profile}} was set aside by Runner and cannot be bound. Choose another profile for this source, or upgrade the job to a release that does not name it.',{profile})
}

/** A registration's answer, bound only when it is an installed profile. */
export function boundEntry(entry:ProfileEntry|undefined):ProfileEntry {
 if(!entry)throw Error(msg('The request could not be completed.'))
 if(!installedEntry(entry))throw Error(setAsideRefusal(entry.profile.id,entry.reason))
 return entry
}
