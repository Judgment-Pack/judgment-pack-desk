import {msg} from '../i18n'
import type {PreparationSummary} from '../dialects/client'
import type {PolicySummary} from '../policies/client'
import type {DialectDraft} from '../dialects/drafts'

interface LibraryItemFields {
  /** Kind-qualified browser identity. Never used as a storage or folder key. */
  key: string
  /** Existing artifact/folder identity, retained for existing mutations. */
  id: string
  title: string
  href: string
  description?: string
  detail?: string
  packVersion?: string
  chatId?: string
  draftId?: string
}

export type LibraryItem = LibraryItemFields & (
  | {kind: 'pack' | 'graph'; status: 'draft' | 'finalized'}
  | {kind: 'policy'; status: 'draft' | 'published' | 'external'; updatedAt: string; reviewState: string; publications: number; sourceAuthority?:PolicySummary['sourceAuthority']}
  | {kind: 'dialect'; status: 'draft' | 'saved'; records?: number; updatedAt: string}
)
export type LibraryKind = LibraryItem['kind'] | 'all'
export function libraryKind(value: string | null): LibraryKind {
  return value === 'pack' || value === 'graph' || value === 'dialect' || value === 'policy' ? value : 'all'
}
export function libraryKindLabel(item: Pick<LibraryItem, 'kind'>) {
  return item.kind === 'pack' ? msg('Pack') : item.kind === 'graph' ? msg('Graph') : item.kind === 'policy' ? msg('Policy') : msg('Dialect')
}
/** Dialects and Policies have no folder assignments in the existing v1/v2 folder contract. */
export function folderAssignmentKey(item: LibraryItem): string | undefined {
  return item.kind === 'dialect' || item.kind === 'policy' ? undefined : item.id
}
export function deletableLibraryDraft(item: LibraryItem) {
  return item.status === 'draft' && item.kind !== 'dialect' && item.kind !== 'policy'
}
export function dialectLibraryItems(
  dialects: PreparationSummary[],
  drafts: Pick<DialectDraft, 'id' | 'name' | 'updatedAt'>[],
): LibraryItem[] {
  return [
    ...dialects.map(row => ({key: 'dialect:' + row.id, id: row.id, title: row.name, kind: 'dialect' as const, status: 'saved' as const, href: '/dialects/' + encodeURIComponent(row.id), records: row.records, updatedAt: row.updatedAt})),
    ...drafts.map(row => ({key: 'dialect-draft:' + row.id, id: row.id, title: row.name || msg('Untitled dialect'), kind: 'dialect' as const, status: 'draft' as const, href: '/dialects/drafts/' + encodeURIComponent(row.id), updatedAt: row.updatedAt})),
  ]
}

export function policyLibraryItems(policies:PolicySummary[]):LibraryItem[]{
 return policies.map(row=>{const external=(row.sourceAuthority?.ownership.mode??row.managementMode)==='externally-managed';return {key:'policy:'+row.id,id:row.id,title:row.title||msg('Untitled policy'),kind:'policy',status:external?'external':row.currentCandidatePublished?'published':'draft',href:'/policies/'+encodeURIComponent(row.id),updatedAt:row.updatedAt,packVersion:row.artifactVersion,description:external?[msg('Local publications')+': '+row.publicationCount,msg('External versions')+': '+(row.sourceAuthority?.externalVersionCount??0)].join(' · '):row.publicationCount?msg('Publications')+': '+row.publicationCount:undefined,reviewState:row.reviewState,publications:row.publicationCount,sourceAuthority:row.sourceAuthority}})
}
export function policyLibraryStatus(item:Extract<LibraryItem,{kind:'policy'}>){
 if(item.status==='external'){
  const applied=item.sourceAuthority?.lastApplied
  return applied?[msg('Externally managed'),applied.status==='published'?msg('Retained Published'):msg('Retained source status'),applied.provenance==='source-provided'?msg('Source provided'):msg('Administrator configured')].join(' · '):msg('Externally managed · No retained external version')
 }
 const review=item.reviewState==='approved'?msg('Approved'):item.reviewState==='changes-requested'?msg('Changes requested'):msg('Unreviewed')
 return (item.status==='published'?msg('Published'):msg('Draft'))+' · '+review
}
