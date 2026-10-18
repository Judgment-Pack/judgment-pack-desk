import {graphDraftFolderId,graphDraftKey,type GraphDraftIdentity} from '../../graphs/identity'
import {folderDocumentVersion,type FolderDocument} from './model'

/** Read-only projection. Persisted only with the next explicit folder action. */
export function projectGraphPlacements(document:FolderDocument,drafts:readonly (GraphDraftIdentity&{id:string;saved?:boolean})[],deleted:readonly string[]=[]){
 const assignments={...document.assignments},active=new Set(drafts.map(graphDraftFolderId)),owners=new Map<string,Set<string>>()
 for(const key of [...drafts.map(graphDraftKey),...deleted]){
  const [origin,id]=key.split(':');if(!origin||!id)continue
  if(!owners.has(id))owners.set(id,new Set())
  owners.get(id)!.add(origin)
 }
 const ambiguous:string[]=[]
 for(const key of Object.keys(assignments)){
  if(!key.startsWith('graph-draft:'))continue
  const parts=key.split(':')
  if(parts.length===3){if(!active.has(key))delete assignments[key];continue}
  const origins=owners.get(parts[1]!)??new Set<string>()
  if(origins.size>1){
   // Keep the original placement recoverable until every surviving draft has
   // an explicit placement. Never project the same alias onto two origins.
   const surviving=[...origins].map(origin=>'graph-draft:'+origin+':'+parts[1]).filter(id=>active.has(id))
   if(surviving.length&&surviving.every(id=>Object.hasOwn(assignments,id)))delete assignments[key]
   else ambiguous.push(key)
  }else{
   const scoped='graph-draft:'+([...origins][0]??'')+':'+parts[1]
   if(active.has(scoped)&&!Object.hasOwn(assignments,scoped))assignments[scoped]=assignments[key]!
   delete assignments[key]
  }
 }
 const savedFolders=new Map<string,Set<string>>()
 for(const draft of drafts){
  const folder=assignments[graphDraftFolderId(draft)];if(!draft.saved||!folder)continue
  const key='graph:'+draft.id
  if(!savedFolders.has(key))savedFolders.set(key,new Set())
  savedFolders.get(key)!.add(folder)
 }
 for(const [key,folders] of savedFolders)if(!Object.hasOwn(assignments,key)&&folders.size===1)assignments[key]=[...folders][0]!
 return {document:folderDocumentVersion({...document,assignments}),ambiguous}
}
