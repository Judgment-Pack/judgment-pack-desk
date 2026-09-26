import { emptyDraftDocument, type DraftPersistence } from '../packs/drafts/model'
/** An isolated private repository for tests; never the production fallback. */
export function memoryDraftPersistence(project: string): DraftPersistence {
 let content: unknown=emptyDraftDocument(), sha256='absent'
 return {read:async()=>({project,content:structuredClone(content),sha256}),write:async(document,digest)=>{
  if(digest!==sha256)throw new Error('stale draft write')
  content=structuredClone(document);sha256=String(Number(sha256==='absent'?0:sha256)+1)
  return {project,content,sha256}
 }}
}
