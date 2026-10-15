import {dialectListQuery,dialectQuery} from './queries'
import {NewDialectDialog} from './NewDialectDialog'
import {LoadingState} from '../ui/LoadingState'
import { DialectSubscriptions } from './DialectSubscriptions'
import { DialectTestsDialog } from './DialectTestsDialog'
import { QueryStatusNotice } from '../shell/QueryStatusNotice'
import {DialectAssistant} from './DialectAssistant'
import { useCollectionSelection } from '../ui/useCollectionSelection'
import { SelectionTable, SelectionHeader, SelectionCell, SelectionActions } from '../ui/SelectionTable'
import { exportSelectedRows } from '../ui/selectionExport'
import { CollectionRow } from '../ui/CollectionRow'
import { IconChevronDown, IconMatrix, IconCheck, IconPreview, IconDownload } from '../shell/icons'
import { Fragment, useEffect, useRef, useState } from 'react'
import { useQuery, useQueryClient } from '@tanstack/react-query'
import { Link, useNavigate, useParams, useSearchParams } from 'react-router-dom'
import { msg, useLocale, formatDate } from '../i18n'
import { Button } from '../ui/Button'
import { Dialog, DialogActions } from '../ui/Dialog'
import { Field, FieldGroup } from '../ui/Field'
import { Input } from '../ui/Input'
import { TextArea } from '../ui/TextArea'
import { Menu, MenuChoice } from '../ui/Menu'
import { Notice } from '../ui/Notice'
import { PageBody, PageHeader, CollectionHeading } from '../ui/PageLayout'
import {CollectionToolbar} from '../ui/CollectionToolbar'
import {OverflowTooltip} from '../ui/Tooltip'
import { CollectionLayout, CollectionEmpty } from '../ui/CollectionLayout'
import { Pagination } from '../ui/Pagination'
import { usePagination } from '../ui/usePagination'
import { Tabs } from '../ui/Tabs'
import { useRegisteredChanges } from '../shell/UnsavedChanges'
import { dialectAPI, exportDialect, type DialectRecord, type PreparationReply, type DialectEvaluation, prepareRecords } from './client'
import { DialectPackEditor } from './DialectPackEditor'
import { ReviewRecords } from './ReviewRecords'
import { DialectActivity } from './DialectActivity'
import {ConversionPreview} from './ConversionPreview'
import { EvaluationResult, EvaluationSummary, latestRecordEvaluation } from './EvaluationHistory'
import {ResearchRecordReview} from './ResearchRecordReview'
import {BulkRecordReview} from './BulkRecordReview'
import {currentRecordReview,researchReviewLabel,recordsGuidance,dialectEvaluationProblem} from './recordReview'
import {ResearchRecipe} from './ResearchRecipe'
import {ResearchActivityDialog} from './ResearchActivity'
import {ResearchReview,ResearchProgress} from './ResearchRun'
import {visibleResearchRun,type ResearchSummary,type ResearchRun} from './research'
import styles from './DialectsView.module.css'
const key=['input-preparations']
const stamp=(at:string)=>{const date=new Date(at);return Number.isFinite(date.getTime())?formatDate(date,{dateStyle:'medium',timeStyle:'short'}):msg('Not recorded')}
const problem=(error:unknown)=>error instanceof Error?error.message:msg('The request could not be completed.')
function download(value:unknown,name:string){const url=URL.createObjectURL(new Blob([JSON.stringify(value,null,2)],{type:'application/json'}));const link=document.createElement('a');link.href=url;link.download=name;link.click();setTimeout(()=>URL.revokeObjectURL(url),1000)}

export function DialectsContent(){
 useLocale();const {preparationId}=useParams()
 return preparationId?<PreparationView key={preparationId} id={preparationId}/>:<PreparationList/>
}
function PreparationList(){
 const opener=useRef<HTMLButtonElement>(null)
 const list=useQuery(dialectListQuery())
 const [search,setSearch]=useState(''),[sort,setSort]=useState('recent'),[filter,setFilter]=useState('all'),[open,setOpen]=useState(false)
 const rows=(list.data?.items??[]).filter(row=>row.name.toLocaleLowerCase().includes(search.toLocaleLowerCase())&&(filter==='all'||(filter==='provided'?row.records>0:row.records===0))).sort((a,b)=>sort==='name'?a.name.localeCompare(b.name):b.updatedAt.localeCompare(a.updatedAt))
 const pagination=usePagination(rows.length,[search,filter,sort].join('|'))

 const selection=useCollectionSelection(rows,row=>row.id,'inputs')
 return <><QueryStatusNotice id="inputs" label={msg('Dialects unavailable')} error={list.error} retrying={list.isFetching} onRetry={()=>list.refetch()}>{msg('Dialects could not be refreshed. Previously loaded dialects may be out of date.')}</QueryStatusNotice><DialectAssistant/><PageHeader title={msg('Dialects')} variant="collection" navigation={<CollectionHeading>{msg('Dialects')}</CollectionHeading>} actions={<Button ref={opener} variant="primary" onClick={()=>setOpen(true)}>{msg('New dialect')}</Button>}/><CollectionLayout label={msg('Dialects')} loading={list.isPending?msg('Loading dialects…'):undefined} scrollKey={JSON.stringify([pagination.page,pagination.size,search,filter,sort])} footer={!list.isPending&&!list.error&&<Pagination pagination={pagination}/>} toolbar={<CollectionToolbar label={msg('Dialects')} searchLabel={msg('Search dialects')} search={search} onSearch={setSearch}
  filters={[{label:msg('State'),value:filter,onValueChange:setFilter,options:[{value:'all',label:msg('All states')},{value:'provided',label:msg('Records provided')},{value:'empty',label:msg('No records')}]}]}
  sort={{label:msg('Sort inputs'),value:sort,onValueChange:setSort,options:[{value:'recent',label:msg('Recently updated')},{value:'name',label:msg('Name')}]}}
  filtered={!!search||filter!=='all'} onReset={()=>{setSearch('');setFilter('all')}}
  actions={({compact,tight})=><SelectionActions selection={selection} compact={compact} tight={tight} onExport={()=>exportSelectedRows('inputs',selection.selectedRows)}/>}/>}>
  {list.error&&!list.data?.items.length?<CollectionEmpty title={msg('Dialects unavailable')}>{null}</CollectionEmpty>:rows.length?<SelectionTable selection={selection} variant="index" aria-label={msg('Dialects')} className={styles.indexTable}><colgroup><col data-selection-column/><col data-name-column/><col className={styles.countColumn}/><col className={styles.statusColumn}/><col/></colgroup><thead><tr><SelectionHeader selection={selection}/><th scope="col">{msg('Name')}</th><th scope="col" data-numeric>{msg('Records')}</th><th scope="col">{msg('Status')}</th><th scope="col">{msg('Updated')}</th></tr></thead><tbody>{rows.slice(pagination.start,pagination.end).map(row=><CollectionRow key={row.id} data-selection-selected={selection.has(row.id)||undefined}><SelectionCell selection={selection} id={row.id} name={row.name}/><td><OverflowTooltip selector="[data-overflow-text]"><Link data-row-primary data-collection-name to={'/dialects/'+row.id}><IconMatrix/><span data-overflow-text>{row.name}</span></Link></OverflowTooltip></td><td data-numeric>{row.records}</td><td><OverflowTooltip><span data-overflow-text>{row.records?msg('Ready to review'):msg('No records')}</span></OverflowTooltip></td><td><OverflowTooltip><span data-overflow-text>{stamp(row.updatedAt)}</span></OverflowTooltip></td></CollectionRow>)}</tbody></SelectionTable>:<CollectionEmpty title={search||filter!=='all'?msg('No matching inputs'):msg('Prepare data for your decisions')}><p>{search||filter!=='all'?msg('Try another search or clear the filters.'):msg('Organize supplied files and URLs, retain their origins, and review the values before evaluation.')}</p></CollectionEmpty>}
 </CollectionLayout>{open&&<NewDialectDialog opener={opener} onClose={()=>setOpen(false)}/>}</>
}
function PreparationView({id}:{id:string}){
 const navigate=useNavigate()
 const cache=useQueryClient(),[params,setParams]=useSearchParams(),opener=useRef<HTMLButtonElement>(null)
 const query=useQuery(dialectQuery(id))
 const [reviewing,setReviewing]=useState(false),reviewOpener=useRef<HTMLButtonElement|null>(null)
 const [importing,setImporting]=useState(false),[detail,setDetail]=useState<DialectRecord>(),[search,setSearch]=useState(''),[filter,setFilter]=useState('all')
 const importOpener=useRef<HTMLButtonElement|null>(null),detailOpener=useRef<HTMLButtonElement|null>(null),tab=['records','recipe','activity'].includes(params.get('tab')??'')?params.get('tab')!:'records'
 const doc=query.data?.document
 const [testRecordIds,setTestRecordIds]=useState<string[]>()
 const [peopleSelection,setPeopleSelection]=useState<{source:PreparationReply;recordIds:string[]}>()
 const researchReviewIds=params.get('review')?.split(',').filter(Boolean)
 const setResearchReviewIds=(ids:string[]|undefined)=>setParams(previous=>{const next=new URLSearchParams(previous);if(ids?.length){next.set('review',ids.join(','));next.set('record',ids[0])}else{next.delete('review');next.delete('record')}return next})
 const [bulkReviewIds,setBulkReviewIds]=useState<string[]>()
 const [reprocessSelection,setReprocessSelection]=useState<{saved:PreparationReply;ids:string[]}>()
 const suppliedRecords=(doc?.records??[]).filter(record=>record.class==='asserted')
 const [recipeEditing,setRecipeEditing]=useState(false),[activityRun,setActivityRun]=useState<string>(),activityOpener=useRef<HTMLButtonElement|null>(null)
 const openActivity=(runId:string,button:HTMLButtonElement)=>{activityOpener.current=button;setActivityRun(runId)}
 const [researchReview,setResearchReview]=useState(false),[stopping,setStopping]=useState(false),[researchError,setResearchError]=useState(''),[importKind,setImportKind]=useState('csv')
 const research=useQuery({queryKey:['input-research',id],enabled:!!doc,queryFn:()=>dialectAPI<{items:ResearchSummary[]}>('/'+id+'/research'),refetchInterval:2000})
 const researchRun=visibleResearchRun(research.data?.items??[]),researchAt=researchRun?.updatedAt
 useEffect(()=>{if(researchAt)void cache.invalidateQueries({queryKey:[...key,id]})},[cache,id,researchAt])
 const refreshResearch=()=>cache.invalidateQueries({queryKey:['input-research',id]})
 const researchStarted=(run:ResearchRun)=>{
  const summary:ResearchSummary={outcomes:run.contractVersion===2?{added:0,updated:0,unchanged:0,needsCorrection:0,pending:0}:undefined,reprocess:run.reprocess,selectionCount:run.selection?.length,id:run.id,state:run.state,updatedAt:run.updatedAt,recordCount:run.records.length,stopRequested:run.stopRequested,problem:run.problem}
  cache.setQueryData<{items:ResearchSummary[]}>(['input-research',id],current=>({items:[summary,...(current?.items??[]).filter(item=>item.id!==run.id)]}))
  void refreshResearch()
 }
 const stopResearch=async()=>{if(!researchRun||stopping)return;setStopping(true);setResearchError('');try{await dialectAPI('/'+id+'/research/'+researchRun.id+'/stop','POST',{});await refreshResearch()}catch(e){setResearchError(problem(e))}finally{setStopping(false)}}
 const openImport=(kind:string)=>{setImportKind(kind);importOpener.current=opener.current;setImporting(true)}
 const startResearch=()=>{if(recipeEditing)return;if(doc?.recipe.research)setResearchReview(true);else setParams(previous=>{const next=new URLSearchParams(previous);next.set('tab','recipe');return next})}
 const preparing=researchRun?.state==='queued'||researchRun?.state==='running'||researchRun?.state==='paused'
 const prepareMenu=<Menu.Root><Menu.Trigger asChild><Button ref={opener} variant="primary" disabled={!doc}>{msg('Prepare records')}<IconChevronDown/></Button></Menu.Trigger><Menu.Portal><Menu.Content align="end" onCloseAutoFocus={event=>{if(importing||researchReview)event.preventDefault()}}><Menu.Item disabled={recipeEditing||preparing||research.isLoading||!!research.error} onSelect={startResearch}>{recipeEditing?msg('Save recipe before research'):researchRun?.state==='paused'?msg('Resolve the research approval first'):preparing?msg('Research in progress'):msg('Research with AI…')}</Menu.Item><Menu.Item onSelect={()=>openImport('csv')}>{msg('Import CSV…')}</Menu.Item><Menu.Item onSelect={()=>openImport('urls')}>{msg('Add URLs…')}</Menu.Item></Menu.Content></Menu.Portal></Menu.Root>
 const [evaluationFilter,setEvaluationFilter]=useState('all'),[retrying,setRetrying]=useState(false),[evaluationError,setEvaluationError]=useState('')
 const evaluations=useQuery({queryKey:['input-evaluations',id],enabled:!!doc,queryFn:()=>dialectAPI<{items:DialectEvaluation[]}>('/'+id+'/evaluations'),refetchInterval:3000})
 const batches=evaluations.data?.items??[]
 const prepared=useQuery({queryKey:['input-preparation-states',id,query.data?.sha256],enabled:!!doc?.recipe.qualification&&!!suppliedRecords.length,queryFn:()=>prepareRecords(query.data!,suppliedRecords),retry:false,refetchOnWindowFocus:false})
 const refreshEvaluations=()=>cache.invalidateQueries({queryKey:['input-evaluations',id]})
 const retryPending=async()=>{if(!batches[0]||retrying)return;setRetrying(true);setEvaluationError('');try{await dialectAPI('/'+id+'/evaluations/'+batches[0].id+'/resume','POST',{reviewed:true});await refreshEvaluations()}catch(e){setEvaluationError(problem(e))}finally{setRetrying(false)}}
 const rows=(doc?.records??[]).filter(row=>row.name.toLocaleLowerCase().includes(search.toLocaleLowerCase())&&(filter==='all'||(filter==='issues'?prepared.data?.rows.some(r=>r.id===row.id&&r.issues.length):row.class==='generated'?filter==='unreviewed'?!currentRecordReview(row,doc!.recipeVersion):currentRecordReview(row,doc!.recipeVersion)?.decision===filter:row.status===filter))&&(evaluationFilter==='all'||(latestRecordEvaluation(batches,row.id)?.row.state??'not-run')===evaluationFilter))
 const pagination=usePagination(rows.length,[id,search,filter,evaluationFilter].join('|')),shown=rows.slice(pagination.start,pagination.end)
 const selection=useCollectionSelection(rows,row=>row.id,id),selectedRows=selection.selectedRows
 const selectedResearch=selectedRows.filter(record=>record.class==='generated')
 const selectedCompanies=selectedRows.filter(record=>record.research?.entity.kind==='company')
 const reprocessProblem=selectedResearch.length!==selectedRows.length?msg('Select only records with saved research.'):recipeEditing?msg('Save or discard Recipe edits first.'):preparing?msg('Finish the active research first.'):!doc?.recipe.research?msg('Save a research recipe first.'):undefined
 const evaluationProblem=dialectEvaluationProblem(selectedRows,doc?.recipeVersion??0)
 const refresh=async(saved:PreparationReply)=>{cache.setQueryData([...key,id],saved);await cache.invalidateQueries({queryKey:key,exact:true})}
 const researchStatus=(researchRun||research.isLoading||researchError)?<div className={styles.researchStatus} aria-label={msg('Research status')}>
  {research.isLoading&&!researchRun&&<LoadingState>{msg('Loading research status…')}</LoadingState>}
  <ResearchProgress preparationId={id} editsPending={recipeEditing} run={researchRun} onStop={()=>void stopResearch()} busy={stopping} onSteps={button=>researchRun&&openActivity(researchRun.id,button)}/>
  {researchError&&<Notice tone="error">{researchError}</Notice>}
 </div>:undefined
 const records=<CollectionLayout label={msg('Records')} scrollKey={JSON.stringify([pagination.page,pagination.size,search,filter,evaluationFilter])} footer={<Pagination pagination={pagination}/>} toolbar={<><CollectionToolbar label={msg('Record controls')} searchLabel={msg('Search records')} search={search} onSearch={setSearch}
  filters={[
   {label:msg('Preparation'),value:filter,onValueChange:setFilter,options:[{value:'all',label:msg('All states')},{value:'provided',label:msg('Provided data')},{value:'needs-research',label:msg('Needs research')},{value:'unreviewed',label:msg('Research needs review')},{value:'accepted',label:msg('Ready for evaluation')},{value:'needs-information',label:msg('Needs more information')},{value:'rejected',label:msg('Excluded')},{value:'issues',label:msg('Conversion issues')}]},
   {label:msg('Evaluation'),value:evaluationFilter,onValueChange:setEvaluationFilter,options:[{value:'all',label:msg('All evaluations')},{value:'not-run',label:msg('Not run')},{value:'completed',label:msg('Completed')},{value:'failed',label:msg('Failed')},{value:'running',label:msg('Running')}]}
  ]}
  filtered={!!search||filter!=='all'||evaluationFilter!=='all'} onReset={()=>{setSearch('');setFilter('all');setEvaluationFilter('all')}}
  actions={({compact,tight})=><SelectionActions selection={selection} triggerRef={reviewOpener} compact={compact} tight={tight}>
   {selectedCompanies.length>0&&<Menu.Item icon={<IconMatrix/>} disabled={selectedCompanies.length!==selectedRows.length||recipeEditing} onSelect={()=>setPeopleSelection({source:query.data!,recordIds:selectedCompanies.map(r=>r.id)})}>{msg('Find people at selected companies…')}</Menu.Item>}
   {selectedResearch.length>0&&<Menu.Item icon={<IconPreview/>} disabled={selectedResearch.length!==selectedRows.length} onSelect={()=>{detailOpener.current=reviewOpener.current;setResearchReviewIds(selectedResearch.map(record=>record.id))}}>{msg('Review research')}</Menu.Item>}
   {selectedResearch.length>0&&<Menu.Item icon={<IconCheck/>} disabled={selectedResearch.length!==selectedRows.length} onSelect={()=>{detailOpener.current=reviewOpener.current;setBulkReviewIds(selectedResearch.map(record=>record.id))}}>{msg('Set review decision…')}</Menu.Item>}
   {selectedResearch.length>0&&<Menu.Item icon={<IconPreview/>} disabled={!!reprocessProblem||research.isLoading||!!research.error} description={reprocessProblem} onSelect={()=>{detailOpener.current=reviewOpener.current;setReprocessSelection({saved:query.data!,ids:selectedRows.map(row=>row.id)})}}>{msg('Reprocess from saved sources…')}</Menu.Item>}
   {<Menu.Item icon={<IconPreview/>} disabled={!!evaluationProblem} onSelect={()=>{if(doc?.recipe.qualification)setReviewing(true);else setParams(previous=>{const next=new URLSearchParams(previous);next.set('tab','recipe');return next})}}>{msg('Review records')}</Menu.Item>}
   <Menu.Item icon={<IconCheck/>} aria-label={msg('Evaluate…')} aria-describedby={evaluationProblem?'input-evaluation-'+id:undefined} description={evaluationProblem&&<span id={'input-evaluation-'+id}>{evaluationProblem}</span>} aria-disabled={!!evaluationProblem} onSelect={()=>{if(doc?.recipe.qualification)setReviewing(true);else setParams(previous=>{const next=new URLSearchParams(previous);next.set('tab','recipe');return next})}}>{msg('Evaluate…')}</Menu.Item>
   <Menu.Item icon={<IconMatrix/>} disabled={!!evaluationProblem} onSelect={()=>{if(doc?.recipe.qualification)setTestRecordIds(selectedRows.map(r=>r.id));else setParams(previous=>{const next=new URLSearchParams(previous);next.set('tab','recipe');return next})}}>{msg('Add to pack tests…')}</Menu.Item>
   <Menu.Item icon={<IconDownload/>} onSelect={()=>download(exportDialect(doc!,selectedRows),id+'-inputs.json')}>{msg('Export')}</Menu.Item>
  </SelectionActions>}/>
 <DialectSubscriptions preparationId={id} evaluations={batches}/>
 {batches[0]&&!batches[0].subscription&&<EvaluationSummary batch={batches[0]} onRetry={()=>void retryPending()} busy={retrying}/>}
 {evaluationError&&<Notice tone="error">{evaluationError}</Notice>}
 </>}>
 {rows.length?<SelectionTable selection={selection} className={styles.table}><thead><tr><SelectionHeader selection={selection}/><th>{msg('Record')}</th><th>{msg('Preparation')}</th><th>{msg('Evaluation')}</th><th>{msg('Decision')}</th></tr></thead><tbody>{shown.map(row=><CollectionRow key={row.id} data-selection-selected={selection.has(row.id)||undefined}><SelectionCell selection={selection} id={row.id} name={row.name}/><td><button data-row-primary className={`${styles.record} ${styles.itemLink}`} onClick={event=>{detailOpener.current=event.currentTarget;if(row.research)setResearchReviewIds([row.id]);else setDetail(row)}}><IconMatrix/><span>{row.name}</span></button></td><td className={prepared.data?.rows.find(r=>r.id===row.id)?.issues.length?styles.problem:undefined}>{prepared.data?.rows.find(r=>r.id===row.id)?.issues.length?msg('Needs review'):row.status==='needs-research'?msg('Needs research'):row.class==='generated'?researchReviewLabel(row,doc!.recipeVersion):!doc?.recipe.qualification?msg('Ready to review'):prepared.isPending?<LoadingState inline>{msg('Checking…')}</LoadingState>:prepared.error?msg('Not known'):msg('Ready')}</td><td>{evaluations.error&&!evaluations.data?msg('Unavailable'):<EvaluationResult result={latestRecordEvaluation(batches,row.id)} recipeVersion={doc!.recipeVersion}/>}</td><td>{latestRecordEvaluation(batches,row.id)?.row.result?.disposition.outcomeId??latestRecordEvaluation(batches,row.id)?.row.result?.disposition.kind??'—'}</td></CollectionRow>)}</tbody></SelectionTable>:<RecordsEmptyState total={doc?.records.length??0} run={researchRun} onReset={()=>{setSearch('');setFilter('all');setEvaluationFilter('all')}}/>}
 </CollectionLayout>
 return <>
 <QueryStatusNotice id={'input:'+id} label={msg('Dialect unavailable')} context={doc?.name} error={query.error} retrying={query.isFetching} onRetry={()=>query.refetch()}>{msg('The dialect could not be refreshed. Previously loaded records may be out of date.')}</QueryStatusNotice>
 <QueryStatusNotice id={'input-research:'+id} label={msg('Research status unavailable')} context={doc?.name} error={research.error} retrying={research.isFetching} onRetry={()=>research.refetch()}>{msg('Research status could not be refreshed. The last known status is shown; the run may still be active.')}</QueryStatusNotice>
 <QueryStatusNotice id={'input-evaluations:'+id} label={msg('Evaluation history unavailable')} context={doc?.name} error={evaluations.error} retrying={evaluations.isFetching} onRetry={()=>evaluations.refetch()}>{msg('Evaluation history could not be loaded. Existing results are not changed.')}</QueryStatusNotice>
 <QueryStatusNotice id={'input-preparation:'+id} label={msg('Preparation status unavailable')} context={doc?.name} error={prepared.error} retrying={prepared.isFetching} onRetry={()=>prepared.refetch()}>{msg('Prepared values could not be checked. Existing records are not changed.')}</QueryStatusNotice>
 <DialectAssistant key={id} id={id} selection={researchReviewIds??selectedRows.map(record=>record.id)} tab={tab} editing={recipeEditing}/>
 {researchReviewIds?.length&&query.data?<ResearchRecordReview key={researchReviewIds.join(',')} saved={query.data} recordIds={researchReviewIds} initialRecordId={params.get('record')??undefined} opener={detailOpener} onRecordChange={recordId=>setParams(previous=>{const next=new URLSearchParams(previous);next.set('record',recordId);return next},{replace:true})} onClose={()=>setResearchReviewIds(undefined)} onSaved={refresh} onReload={async()=>{const result=await query.refetch();if(result.error)throw result.error;return result.data!}}/>:<><PageHeader title={msg('Dialects')} titleHref="/dialects" context={doc?.name} actions={prepareMenu}/>{query.isPending?<PageBody loading={msg('Loading dialect…')}>{null}</PageBody>:query.error&&!doc?<PageBody><p className={styles.note}>{msg('Dialect unavailable')}</p></PageBody>:doc&&<Tabs label={msg('Dialects')} status={researchStatus} activationMode="manual" variant="page" fillPanel={tab} value={tab} onValueChange={tab=>setParams(previous=>{const next=new URLSearchParams(previous);next.set('tab',tab);return next})} scrollable tabs={[{value:'records',label:msg('Records'),panel:records},{value:'recipe',label:msg('Recipe'),panel:<Recipe saved={query.data!} onSaved={refresh} onEditing={setRecipeEditing}/>},{value:'activity',label:msg('Activity'),panel:<DialectActivity document={doc} batches={batches} research={research.data?.items??[]} onResearch={openActivity} loading={evaluations.isPending} error={evaluations.error}/> }]}/>}</>}
 {peopleSelection&&<NewDialectDialog opener={reviewOpener} companies={peopleSelection} onClose={()=>setPeopleSelection(undefined)}/>}
 {testRecordIds&&query.data&&doc?.recipe.qualification&&<DialectTestsDialog packId={doc.recipe.qualification.key} packText={doc.recipe.qualification.pack} initial={query.data} recordIds={testRecordIds} opener={reviewOpener} onClose={()=>setTestRecordIds(undefined)} onSaved={()=>{setTestRecordIds(undefined);navigate('/packs/'+encodeURIComponent(doc.recipe.qualification!.key)+'/matrix')}}/>}
 {reviewing&&query.data&&<ReviewRecords saved={query.data} records={selectedRows} opener={reviewOpener} onClose={()=>setReviewing(false)} onEvaluated={()=>void refreshEvaluations()}/>}
 {activityRun&&<ResearchActivityDialog preparationId={id} runId={activityRun} opener={activityOpener} onClose={()=>setActivityRun(undefined)}/>}
 {researchReview&&query.data&&<ResearchReview saved={query.data} opener={opener} onClose={()=>setResearchReview(false)} onStarted={researchStarted}/>}
 {query.data&&<ImportInputs key={importKind} initialKind={importKind} saved={query.data} open={importing} onOpenChange={setImporting} opener={importOpener} onSaved={refresh}/>}
 {reprocessSelection&&<ResearchReview saved={reprocessSelection.saved} recordIds={reprocessSelection.ids} opener={detailOpener} onClose={()=>setReprocessSelection(undefined)} onStarted={researchStarted}/>}
 {bulkReviewIds&&query.data&&<BulkRecordReview saved={query.data} recordIds={bulkReviewIds} opener={detailOpener} onClose={()=>setBulkReviewIds(undefined)} onSaved={refresh}/>}
 <Dialog open={!!detail} onOpenChange={open=>{if(!open)setDetail(undefined)}} title={detail?.name??msg('Record')} openerRef={detailOpener} size="wide">{detail&&<div className={styles.stack}><p className={styles.note}>{recordsGuidance([detail])}</p><dl className={styles.detail}>{Object.entries(detail.values).map(([key,value])=><Fragment key={key}><dt>{key}</dt><dd>{value||msg('Not provided')}</dd></Fragment>)}</dl><p className={styles.note}>{msg('Source: {{source}} · Row {{row}}',{source:doc?.imports.find(source=>source.id===detail.sourceId)?.name??msg('Supplied URLs'),row:detail.row})}</p></div>}</Dialog></>
}
function Recipe({saved,onSaved,onEditing}:{saved:PreparationReply;onSaved:(saved:PreparationReply)=>Promise<void>;onEditing:(editing:boolean)=>void}){
 const [base,setBase]=useState(saved)
 const [name,setName]=useState(saved.document.name),[intent,setIntent]=useState(saved.document.recipe.intent),[busy,setBusy]=useState(false),[error,setError]=useState('')
 const [qualificationPending,setQualificationPending]=useState(false)
 const [qualification,setQualification]=useState(saved.document.recipe.qualification)
 const [research,setResearch]=useState(saved.document.recipe.research)
 const dirty=name!==base.document.name||intent!==base.document.recipe.intent||JSON.stringify(qualification)!==JSON.stringify(base.document.recipe.qualification)||JSON.stringify(research)!==JSON.stringify(base.document.recipe.research)
 useEffect(()=>{onEditing(dirty||busy||qualificationPending);return()=>onEditing(false)},[dirty,busy,qualificationPending,onEditing])
 // Background refetches may update a clean form, never an in-progress edit.
 // A dirty edit keeps its starting digest so concurrent changes are refused.
 useEffect(()=>{if(!dirty&&!busy&&saved.sha256!==base.sha256){setBase(saved);setName(saved.document.name);setIntent(saved.document.recipe.intent);setQualification(saved.document.recipe.qualification);setResearch(saved.document.recipe.research)}},[saved,base.sha256,dirty,busy])
 useRegisteredChanges(dirty,msg('Leaving will discard unsaved changes in the open editors.'),{name:base.document.name,busy:busy||qualificationPending,shouldBlock:({currentLocation,nextLocation})=>currentLocation.pathname!==nextLocation.pathname||new URLSearchParams(nextLocation.search).get('tab')!=='recipe'})
 const save=async()=>{if(busy||qualificationPending)return;setBusy(true);setError('');try{const next=await dialectAPI<PreparationReply>('/'+base.document.id,'PUT',{name,recipe:{...base.document.recipe,intent,research,qualification}},base.sha256);await onSaved(next);setBase(next);setName(next.document.name);setIntent(next.document.recipe.intent);setQualification(next.document.recipe.qualification);setResearch(next.document.recipe.research)}catch(error){setError(problem(error))}finally{setBusy(false)}}
 return <div className={styles.recipePane}><PageBody><FieldGroup className={styles.form}><Field label={msg('Name')}>{props=><Input {...props} disabled={busy||qualificationPending} maxLength={160} value={name} onChange={e=>setName(e.target.value)}/>}</Field><Field label={msg('What records do you need?')} hint={msg('Describe the records to find and what to research. Set record counts, request budgets and retries in Limits and retries. Qualification rules stay separate.')}>{props=><TextArea {...props} disabled={busy||qualificationPending} rows={6} maxLength={12000} value={intent} onChange={e=>setIntent(e.target.value)}/>}</Field><ResearchRecipe value={research} onChange={setResearch} records={base.document.records} disabled={busy||qualificationPending}/><DialectPackEditor value={qualification} records={base.document.records} research={research} disabled={busy||qualificationPending} onChange={setQualification} onPendingChange={setQualificationPending}/>{qualification&&<ConversionPreview saved={base} value={qualification} disabled={busy||qualificationPending}/>}<p className={styles.note}>{msg('Recipe version {{version}}. Changes do not rewrite earlier imports or evaluate records.',{version:base.document.recipeVersion})}</p></FieldGroup></PageBody><footer className={styles.recipeFooter} aria-label={msg('Recipe actions')}><div className={styles.recipeFooterInner}>{error&&<Notice tone="error">{error}</Notice>}<div className={styles.reviewSummary}><p className={styles.note} role="status">{busy?msg('Saving…'):dirty?msg('Unsaved changes'):msg('All changes saved')}</p><div className={styles.formActions}><Button disabled={busy||qualificationPending||!dirty} onClick={()=>{setName(base.document.name);setIntent(base.document.recipe.intent);setQualification(base.document.recipe.qualification);setResearch(base.document.recipe.research);setError('')}}>{msg('Discard changes')}</Button><Button variant="primary" disabled={busy||qualificationPending||!name.trim()||!dirty} onClick={()=>void save()}>{busy?msg('Saving…'):msg('Save recipe')}</Button></div></div></div></footer></div>
}
function ImportInputs({saved,open,onOpenChange,opener,onSaved,initialKind='csv'}:{initialKind?:string;saved:PreparationReply;open:boolean;onOpenChange:(open:boolean)=>void;opener:React.RefObject<HTMLButtonElement|null>;onSaved:(saved:PreparationReply)=>Promise<void>}){
 const [kind,setKind]=useState(initialKind),[content,setContent]=useState(''),[name,setName]=useState(''),[busy,setBusy]=useState(false),[error,setError]=useState('')
 const reading=useRef(0)
 const close=(next:boolean)=>{if(busy)return;reading.current++;setContent('');setName('');setError('');onOpenChange(next)}
 const submit=async()=>{if(busy||!content.trim())return;setBusy(true);setError('');try{await onSaved(await dialectAPI<PreparationReply>('/'+saved.document.id+'/imports','POST',{kind,name:name||msg('Supplied URLs'),content},saved.sha256));setContent('');setName('');onOpenChange(false)}catch(error){setError(problem(error))}finally{setBusy(false)}}
 return <Dialog open={open} onOpenChange={close} title={msg('Prepare records')} openerRef={opener} footer={<DialogActions><Button disabled={busy} onClick={()=>close(false)}>{msg('Cancel')}</Button><Button variant="primary" disabled={busy||!content.trim()} onClick={()=>void submit()}>{msg('Import records')}</Button></DialogActions>}><FieldGroup><MenuChoice label={msg('Input type')} value={kind} onValueChange={next=>{reading.current++;setKind(next);setContent('');setName('');setError('')}} options={[{value:'csv',label:msg('CSV file')},{value:'urls',label:msg('Supplied URLs')}]}/>{kind==='csv'?<Field label={msg('CSV file')} hint={msg('Up to 1,000 records. Column values are preserved as provided.')}>{props=><Input {...props} type="file" accept=".csv,text/csv" disabled={busy} onChange={async event=>{const file=event.target.files?.[0];setContent('');setError('');const version=++reading.current;if(!file)return;if(file.size>1_500_000){setError(msg('Choose a file smaller than 1.5 MB.'));return}try{const text=await file.text();if(reading.current===version){setName(file.name);setContent(text)}}catch{if(reading.current===version)setError(msg('The file could not be read.'))}}}/>}</Field>:<Field label={msg('Supplied URLs')} hint={msg('One complete HTTP or HTTPS URL per line. No websites are read during import.')}>{props=><TextArea {...props} rows={7} value={content} disabled={busy} onChange={e=>setContent(e.target.value)}/>}</Field>}{error&&<Notice tone="error">{error}</Notice>}</FieldGroup></Dialog>
}

function RecordsEmptyState({total,run,onReset}:{total:number;run?:ResearchSummary;onReset:()=>void}){
 if(total)return <div className={styles.empty}><strong>{msg('No matching records')}</strong><p className={styles.note}>{msg('Try another search or clear the filters.')}</p><Button variant="quiet" onClick={onReset}>{msg('Clear filters')}</Button></div>
 const active=run?.state==='running'||run?.state==='queued',paused=run?.state==='paused'
 return <div className={styles.empty}>
  <strong>{active?msg('Research is in progress'):paused?msg('Research paused'):run?msg('No records saved'):msg('No records')}</strong>
  <p className={styles.note}>{active?msg('Records will appear here as they are saved. Use View steps to follow the research.'):paused?msg('No records have been saved yet. Review the request increase to continue, or finish without records.'):run?msg('This run did not save any records. View steps for the outcome before starting another run.'):msg('Use Prepare records to research with AI, import a CSV file or add URLs.')}</p>
  {!!run?.sourceCount&&<p className={styles.note}>{msg('{{count}} sources retained. Sources are research material, not saved records.',{count:run.sourceCount})}</p>}
  {!active&&run?.explanation&&<p className={styles.note}>{msg('AI explanation: {{explanation}}',{explanation:run.explanation})}</p>}
 </div>
}
