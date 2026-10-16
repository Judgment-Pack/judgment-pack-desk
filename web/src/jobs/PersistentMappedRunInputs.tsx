import {useRef} from 'react'
import {useQuery,useQueryClient} from '@tanstack/react-query'
import {readFile,writeFile,FileRequestError,type FileContent} from '../files/client'
import {msg} from '../i18n'
import {LoadingState} from '../ui/LoadingState'
import {Notice} from '../ui/Notice'
import {Button} from '../ui/Button'
import {MappedInputFields} from './MappedInputFields'
import {decodeJobDraft,type MappedDraft} from './drafts'
import type {Job,Release} from './client'
import type {ExecutableDialect,SourceV2} from './dialectTypes'
import type {PackDocument} from '../mcp/types'

export function runInputDraftPath(job:string,release:string){
 if(!/^job_[a-f0-9]{32}$/.test(job)||!/^release_[a-f0-9]{32,64}$/.test(release))throw Error(msg('Invalid job draft.'))
 return `.desk/job-run-inputs/${job}/${release}.json`
}
/** The existing draft decoder admits configuration and research references only.
 * Operational input drafts live outside the job-creation draft collection. */
export function decodeRunInputDraft(content:string,release:Release):MappedDraft {
 const value=decodeJobDraft(content)
 if(value.id!=='run-inputs'||value.values.packId!==release.id||!value.values.mapped)throw Error(msg('Invalid job draft.'))
 if(value.values.mapped.text!==JSON.stringify(release.inputMapping,null,2))throw Error(msg('The saved inputs belong to another job release.'))
 return value.values.mapped
}
export function PersistentMappedRunInputs({job,release,mapping,disabled,onChange,onWorkChange}:{job:Job;release:Release;mapping:ExecutableDialect;disabled:boolean;onChange:(source:SourceV2|undefined)=>void;onWorkChange:(dirty:boolean)=>void}){
 const base=useRef<FileContent|undefined>(undefined),cache=useQueryClient()
 const path=runInputDraftPath(job.id,release.id)
 const queryKey=['job-run-inputs',job.id,release.id]
 const query=useQuery<{draft:MappedDraft;file?:FileContent}>({queryKey,retry:false,staleTime:Infinity,refetchOnWindowFocus:false,queryFn:async()=>{
  try{const file=await readFile(path);const draft=decodeRunInputDraft(file.content,release);base.current=file;return {draft,file}}
  catch(e){if(e instanceof FileRequestError&&e.status===404)return {draft:{text:JSON.stringify(mapping,null,2),caseText:'{"facts": {}, "evidence": {}}'}};throw e}
 }})
 async function persist(draft:MappedDraft,signal:AbortSignal){
  signal.throwIfAborted()
  const content=JSON.stringify({version:1,id:'run-inputs',updatedAt:new Date().toISOString(),status:'draft',values:{name:job.name,packId:release.id,inputMode:'mapped',facts:'{}',supplied:false,evidence:'{}',mapped:draft}})
  decodeRunInputDraft(content,release)
  const file=await writeFile({path,content,baseSha256:base.current?.sha256??query.data?.file?.sha256??'',createParents:true})
  if(file.content!==content)throw Error(msg('The saved draft could not be verified. Your changes are still here.'))
  base.current=file
  if(!signal.aborted)cache.setQueryData(queryKey,{draft,file})
 }
 if(query.isPending)return <LoadingState>{msg('Loading run inputs…')}</LoadingState>
 if(query.error)return <Notice tone="warning" actions={<Button size="small" onClick={()=>void query.refetch()}>{msg('Retry')}</Button>}>{msg('Saved run inputs could not be loaded.')}</Notice>
 return <MappedInputFields key={path} doc={JSON.parse(release.pack) as PackDocument} packText={release.pack} fixed={mapping} disabled={disabled} onChange={onChange} onWorkChange={onWorkChange} draft={query.data?.draft} onPersistResearchDraft={persist}/>
}
