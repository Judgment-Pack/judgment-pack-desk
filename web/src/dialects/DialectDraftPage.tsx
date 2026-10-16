import {useEffect,useRef,useState,type ReactNode} from 'react'
import {useQuery,useQueryClient} from '@tanstack/react-query'
import {Navigate,useLocation,useNavigate,useParams,useSearchParams} from 'react-router-dom'
import {msg} from '../i18n'
import {useEffectiveConfig} from '../config/DeskConfigProvider'
import {resolveAIConnection} from '../assistant/aiConnections'
import {useChats} from '../chat/ChatProvider'
import {PageHeader,PageBody} from '../ui/PageLayout'
import {Button,ButtonLink} from '../ui/Button'
import {Tabs} from '../ui/Tabs'
import {Disclosure} from '../ui/Disclosure'
import {LoadingState} from '../ui/LoadingState'
import {QueryStatusNotice} from '../shell/QueryStatusNotice'
import {useConfirmDiscard,useRegisteredChanges} from '../shell/UnsavedChanges'
import {DialectAssistant} from './DialectAssistant'
import {RecipePurpose,RecipeSection} from './RecipeSection'
import {RecipeFields} from './RecipeFields'
import {ResearchRecipe} from './ResearchRecipe'
import {RecipePackSection} from './RecipePackSection'
import {defaultResearchRetries,requiresResearchParents,type ResearchRecipe as Research} from './research'
import type {PreparationReply} from './client'
import {dialectTemplates,templateOrigin,type DialectTemplate,type TemplateOrigin} from './templates'
import {ExchangeResearchTemplate} from './ExchangeResearchTemplate'
import {DIALECT_DRAFTS_KEY,dialectDraftQuery,dialectDraftAPI,dialectDraftHref,type DialectDraftReply} from './drafts'
import {useDialectDraft} from './useDialectDraft'
import styles from './DialectDraftPage.module.css'

function DraftPanel({children,full=false}:{children:ReactNode;full?:boolean}){return <div className={styles.panel}><PageBody width={full?'full':'wide'}>{children}</PageBody></div>}
type Companies={source:PreparationReply;recordIds:string[]}
function researchFor(template:DialectTemplate,ai:Research['ai'],companies?:Companies):Research|undefined{
 return template.research?{version:1,...structuredClone(template.research),target:'records',retries:defaultResearchRetries(),limits:{...template.research.limits,searches:0},ai,...(companies?{requireParents:true,companyInputs:companies.source.document.records.filter(r=>companies.recordIds.includes(r.id)&&(r.research?.entity.kind==='company'||r.research?.entity.kind==='record')).map(r=>({preparationId:companies.source.document.id,revision:companies.source.sha256,entityId:r.research!.entity.id}))}:{})}:undefined
}
/** Creation persists an incomplete draft before opening its editor. The stable
 * request key makes StrictMode, interrupted replies and Retry idempotent. */
export function NewDialectPage(){
 const location=useLocation(),navigate=useNavigate(),cache=useQueryClient(),effective=useEffectiveConfig()
 const [error,setError]=useState<unknown>(),[attempt,setAttempt]=useState(0),key=useRef(crypto.randomUUID()),request=useRef<Promise<DialectDraftReply>|undefined>(undefined)
 useEffect(()=>{let current=true;setError(undefined)
  if(!request.current){
   const companies=(location.state as {companies?:Companies}|null)?.companies,template=dialectTemplates().find(t=>t.id===(companies?'related-records':'blank'))!
   const resolved=resolveAIConnection(effective),assistant=resolved.assistant,config=assistant.engine==='codex'?assistant.agent:assistant.endpoint
   request.current=templateOrigin(template).then(origin=>dialectDraftAPI<DialectDraftReply>('','POST',{key:key.current,name:companies?msg('Records related to {{name}}',{name:companies.source.document.name}):'',recipe:{intent:template.intent,template:origin,research:researchFor(template,{connection:resolved.connection?.id??'',revision:resolved.connection?.revision??'',model:config?.model??''},companies)}}))
  }
  void request.current.then(saved=>{if(current){cache.setQueryData([...DIALECT_DRAFTS_KEY,saved.document.id],saved);void cache.invalidateQueries({queryKey:DIALECT_DRAFTS_KEY,exact:true});navigate(dialectDraftHref(saved.document),{replace:true})}},e=>{if(current){request.current=undefined;setError(e)}})
  return()=>{current=false}
 },[attempt])
 return <><PageHeader title={msg('Dialects')} titleHref="/dialects" context={msg('New dialect')}/><QueryStatusNotice id="new-dialect-draft" label={msg('Dialect draft unavailable')} error={error} onRetry={()=>setAttempt(n=>n+1)}/><PageBody loading={!error?msg('Creating draft…'):undefined}>{!!error&&<div className={styles.stack}><p>{msg('The draft could not be opened. Retry to continue.')}</p><div><Button onClick={()=>setAttempt(n=>n+1)}>{msg('Retry')}</Button></div></div>}</PageBody></>
}
export function DialectDraftPage(){
 const {draftId=''}=useParams(),query=useQuery(dialectDraftQuery(draftId))
 return <><QueryStatusNotice id={'dialect-draft-read:'+draftId} label={msg('Dialect draft unavailable')} error={query.error} retrying={query.isFetching} onRetry={()=>query.refetch()}/>{query.data?<DialectDraftEditor key={draftId} initial={query.data}/>:<><PageHeader title={msg('Dialects')} titleHref="/dialects" context={msg('Dialect draft')}/><PageBody loading={query.isPending?msg('Loading draft…'):undefined}>{query.error&&<p>{msg('Dialect draft unavailable')}</p>}</PageBody></>}</>
}
export function DialectDraftEditor({initial}:{initial:DialectDraftReply}){
 const editor=useDialectDraft(initial),{value,setValue,saved,busy,dirty,error,flush,clear,reload}=editor
 const [params,setParams]=useSearchParams(),navigate=useNavigate(),cache=useQueryClient(),confirm=useConfirmDiscard(),{store}=useChats()
 const [working,setWorking]=useState(false),[pending,setPending]=useState(false),[problem,setProblem]=useState<unknown>(),active=useRef(false)
 const section=['overview','fields','sources','execution','pack'].includes(params.get('tab')??'')?params.get('tab')!:'overview'
 const setSection=(tab:string)=>setParams(previous=>{const next=new URLSearchParams(previous);next.set('tab',tab);return next},{replace:true})
 const clearWork=useRegisteredChanges(false,msg('Wait for the dialect to finish saving.'),{kind:'activity',busy:working})
 const disabled=working||pending,research=value.recipe.research
 const companySelection=['company-people','related-records'].includes(value.recipe.template?.id??'')||requiresResearchParents(research)
 const update=(recipe:typeof value.recipe)=>setValue({...value,recipe})
 const publish=async()=>{
  if(active.current)return;active.current=true;setWorking(true);setProblem(undefined)
  try{
   const current=await flush(),result=await dialectDraftAPI<DialectDraftReply>('/'+current.document.id+'/publish','POST',{},current.sha256)
   cache.setQueryData([...DIALECT_DRAFTS_KEY,result.document.id],result)
   void cache.invalidateQueries({queryKey:DIALECT_DRAFTS_KEY});void cache.invalidateQueries({queryKey:['input-preparations']})
   const origin=result.document.origin,chat=origin?store?.getSnapshot().chats.find(c=>c.id===origin.chatId):undefined
   if(chat)store?.update(chat.id,{inputDrafts:chat.inputDrafts?.map(d=>d.draftId===origin!.draftId?{...d,savedInputId:result.document.savedInputId}:d)})
   clear();clearWork();navigate(dialectDraftHref(result.document),{replace:true})
  }catch(e){setProblem(e)}finally{active.current=false;setWorking(false)}
 }
 const discard=async()=>{if(active.current||busy||!await confirm(msg('Discard this dialect draft? Its chat will remain.')))return;active.current=true;setWorking(true);setProblem(undefined);try{
  await editor.settled();const current=editor.current();await dialectDraftAPI('/'+current.document.id,'DELETE',undefined,current.sha256);clear();clearWork();void cache.invalidateQueries({queryKey:DIALECT_DRAFTS_KEY});navigate('/dialects')
 }catch(e){setProblem(e)}finally{active.current=false;setWorking(false)}}
 const applyTemplate=async(template:DialectTemplate,source?:TemplateOrigin)=>{
  if(active.current)return;active.current=true
  if((value.recipe.intent||research?.fields.length)&&!await confirm(msg('Replace this draft’s instructions and fields with the selected template?'))){active.current=false;return}
  setWorking(true);setProblem(undefined)
  try{const origin=source??await templateOrigin(template),next=researchFor(template,research?.ai??{connection:'',revision:'',model:''});if(next&&research){next.requireParents=companySelection;next.companyInputs=research.companyInputs;next.companyIds=research.companyIds}update({...value.recipe,intent:template.intent,template:origin,research:next})}catch(e){setProblem(e)}finally{active.current=false;setWorking(false)}
 }
 const overview=<div className={styles.stack}>
  <p className={styles.note}>{msg('Draft changes save automatically and stay in Dialects if this chat is deleted. Research starts after a separate review.')}</p>
  <RecipePurpose name={value.name} intent={value.recipe.intent} onName={name=>setValue({...value,name})} onIntent={intent=>update({...value.recipe,intent})} disabled={disabled}/>
  <ResearchRecipe key="overview" value={research} section="overview" companySelection={companySelection} records={[]} disabled={disabled} allowRemove={false} onChange={research=>update({...value.recipe,research})}/>
  <RecipeSection title={msg('Template')}><ExchangeResearchTemplate current={value.recipe.template} purpose={value.recipe.intent} disabled={disabled} onApply={applyTemplate}/></RecipeSection>
  <Disclosure title={msg('Discard draft')}><Button variant="danger" disabled={disabled||busy} onClick={()=>void discard()}>{msg('Discard draft')}</Button></Disclosure>
 </div>
 const fieldsIssue=research&&(!research.fields.length||new Set(research.fields.map(f=>f.name)).size!==research.fields.length||research.fields.some(f=>! /^[a-z][a-zA-Z0-9_]{0,63}$/.test(f.name)||['constructor','prototype','__proto__'].includes(f.name)||!f.description.trim()))
 const limitsIssue=research&&(research.limits.records<1||research.limits.modelCalls<1||research.limits.searches>0&&!research.search)
 const setupIssue=!value.name.trim()?msg('Enter a name.'):research&&(!research.ai.connection||!research.ai.revision||!research.ai.model)?msg('Choose an AI connection and model.'):companySelection&&!(research?.companyIds?.length||research?.companyInputs?.length)?msg('Select companies to research.'):fieldsIssue?msg('Complete the requested fields with unique names and descriptions.'):limitsIssue?msg('Review the research limits.'):undefined
 if(saved.document.savedInputId)return <Navigate replace to={dialectDraftHref(saved.document)}/>
 if(saved.document.discarded)return <><PageHeader title={msg('Dialects')} titleHref="/dialects" context={msg('Dialect draft')}/><PageBody><p>{msg('This dialect draft was discarded.')}</p><ButtonLink to="/dialects">{msg('Back to Dialects')}</ButtonLink></PageBody></>
 return <>
  <DialectAssistant draft={{...saved.document,...value}} editing/>
  <QueryStatusNotice id={'dialect-draft-save:'+saved.document.id} label={msg('Draft changes could not be saved')} error={error} retrying={busy} onRetry={()=>flush().catch(()=>{})}/>
  <QueryStatusNotice id={'dialect-draft-action:'+saved.document.id} label={msg('Dialect needs attention')} error={problem} retrying={working} retryLabel={msg('Dismiss')} onRetry={()=>{setProblem(undefined)}}/>
  <PageHeader title={msg('Dialects')} titleHref="/dialects" context={value.name||msg('Untitled dialect')} meta={msg('Draft')} actions={<Button variant="primary" disabled={disabled||!!setupIssue} onClick={()=>void publish()}>{working?msg('Saving…'):msg('Save to Dialects')}</Button>}/>
  <Tabs scrollable label={msg('Dialect draft')} variant="page" fillPanel={section} value={section} onValueChange={setSection} tabs={[
   {value:'overview',label:msg('Overview'),panel:<DraftPanel>{overview}</DraftPanel>},
   {value:'fields',label:msg('Requested fields')+(research?' · '+research.fields.length:''),panel:<DraftPanel full>{research?<RecipeFields expanded value={research.fields} onChange={fields=>update({...value.recipe,research:{...research,fields}})} disabled={disabled}/>:<ResearchRecipe value={research} records={[]} disabled={disabled} onChange={research=>update({...value.recipe,research})}/>}</DraftPanel>},
   ...(['sources','execution'] as const).map(tab=>({value:tab,label:tab==='sources'?msg('Sources'):msg('Execution'),panel:<DraftPanel><ResearchRecipe key={tab} section={tab} value={research} records={[]} disabled={disabled} allowRemove={false} onChange={research=>update({...value.recipe,research})}/></DraftPanel>})),
   {value:'pack',label:msg('Pack'),panel:<DraftPanel><RecipePackSection value={value.recipe.qualification} records={[]} research={research} disabled={working} onPendingChange={setPending} onChange={qualification=>update({...value.recipe,qualification})}/></DraftPanel>}
  ]}/>
  <footer className={styles.footer}><span role="status">{busy?<LoadingState inline>{msg('Saving draft…')}</LoadingState>:error?msg('Draft changes could not be saved'):dirty?msg('Changes pending'):msg('Draft saved')}</span>
   {error?<div className={styles.actions}><Button size="small" disabled={busy} onClick={()=>void flush().catch(()=>{})}>{msg('Retry')}</Button><Button size="small" variant="quiet" disabled={busy} onClick={async()=>{if(await confirm(msg('Reload the saved draft and discard these unsaved changes?')))void reload().catch(setProblem)}}>{msg('Reload saved draft')}</Button></div>:setupIssue&&<Button variant="inline" size="small" disabled={disabled} onClick={()=>setSection(!value.name.trim()?'overview':companySelection&&!(research?.companyIds?.length||research?.companyInputs?.length)?'overview':fieldsIssue?'fields':'execution')}>{setupIssue}</Button>}
  </footer>
 </>
}
