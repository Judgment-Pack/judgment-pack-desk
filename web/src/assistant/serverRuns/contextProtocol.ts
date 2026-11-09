import { exact, object, text } from './decode'
import type { RunContextSelector } from './types'

export type ContextRequestKind='document'|'pane'|'release-document'|'release-pane'
export const contextToken=(value:unknown):value is string=>typeof value==='string'&&/^[a-f0-9]{64}$/.test(value)
export const panePurpose=(value:unknown):value is 'author-pack'|'describe-pack'|'author-graph'=>value==='author-pack'||value==='describe-pack'||value==='author-graph'
export function contextSelector(value:unknown,purpose:unknown):value is RunContextSelector {
 if(!object(value)||!exact(value,['document','pane'])||!contextToken(value.document))return false
 return purpose==='chat'?!Object.hasOwn(value,'pane'):panePurpose(purpose)&&contextToken(value.pane)
}
export function contextRequest(kind:ContextRequestKind,value:unknown):boolean {
 if(!object(value))return false
 switch(kind){
  case 'document':return exact(value,['registrationId'])&&text(value.registrationId,128)
  case 'pane':return exact(value,['document','registrationId','purpose'])&&contextToken(value.document)&&text(value.registrationId,128)&&panePurpose(value.purpose)
  case 'release-document':return exact(value,['document'])&&contextToken(value.document)
  case 'release-pane':return exact(value,['document','pane'])&&contextToken(value.document)&&contextToken(value.pane)
 }
}
