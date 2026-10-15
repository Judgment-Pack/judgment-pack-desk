import {privateQuery} from '../data/privateQuery'
import {dialectListQuery} from './queries'
import {LoadingState} from '../ui/LoadingState'
import {useState} from 'react'
import {useQuery} from '@tanstack/react-query'
import {Link} from 'react-router-dom'
import type {McpToolResult} from '../assistant/engine'
import {usePack} from '../mcp/queries'
import {useMcp} from '../mcp/McpProvider'
import {msg} from '../i18n'
import {Dialog,DialogActions} from '../ui/Dialog'
import {Button} from '../ui/Button'
import {Field} from '../ui/Field'
import {Select} from '../ui/Select'
import {Notice} from '../ui/Notice'
import {Disclosure} from '../ui/Disclosure'
import {CodeBlock} from '../ui/CodeBlock'
import {CollectionToolbar} from '../ui/CollectionToolbar'
import {SelectionTable,SelectionHeader,SelectionCell} from '../ui/SelectionTable'
import {useCollectionSelection} from '../ui/useCollectionSelection'
import {jobsAPI,type InputPreview} from '../jobs/client'
import {inspectPreparedFactTypes} from '../packs/test-workspace/proposals'
import {readTests,updateTests} from '../packs/test-workspace/store'
import type {TestCase} from '../packs/test-workspace/model'
import {dialectAPI,prepareRecords,type PreparationReply} from './client'
import {currentRecordReview} from './recordReview'
import {dialectTestCase,appendDialectTestCases} from './testSnapshots'
import styles from './DialectsView.module.css'

export function DialectTestsDialog({packId,packText,initial,recordIds,opener,onClose,onSaved}:{packId:string;packText:string;initial?:PreparationReply;recordIds?:string[];opener:React.RefObject<HTMLButtonElement|null>;onClose:()=>void;onSaved:()=>void}){
 const {client,status}=useMcp(),livePack=usePack(packId)
 const [collection,setCollection]=useState(initial?.document.id??''),[search,setSearch]=useState(''),[busy,setBusy]=useState(false),[error,setError]=useState(''),[reviewed,setReviewed]=useState(false)
 const [proposal,setProposal]=useState<{saved:PreparationReply;cases:TestCase[];project:string}>()
 const list=useQuery(dialectListQuery())
 const query=useQuery({...privateQuery,queryKey:['input-test-source',collection],enabled:!!collection,queryFn:()=>dialectAPI<PreparationReply>('/'+collection),initialData:initial?.document.id===collection?initial:undefined})
 const saved=query.data,q=saved?.document.recipe.qualification,compatible=q?.key===packId&&q.pack===packText
 const rows=(saved?.document.records??[]).filter(r=>(!recordIds||recordIds.includes(r.id))&&r.name.toLocaleLowerCase().includes(search.toLocaleLowerCase()))
 const selection=useCollectionSelection(rows,r=>r.id,collection)
 const selected=recordIds?rows:selection.selectedRows
 const ineligible=selected.some(r=>r.class==='generated'&&currentRecordReview(r,saved!.document.recipeVersion)?.decision!=='accepted')
 async function prepare(){if(!saved||!compatible||!client)return;setBusy(true);setError('');try{
  const prepared=await prepareRecords(saved,selected)
  if(prepared.rows.some(r=>r.issues.length))throw Error('Resolve conversion issues in the Dialects recipe before creating cases.')
  const cases:TestCase[]=[]
  for(const row of prepared.rows){const preview=await jobsAPI<InputPreview>('inputs/preview',row.input);cases.push(await dialectTestCase(saved,selected.find(r=>r.id===row.id)!,preview))}
  const checks=await inspectPreparedFactTypes(packText,cases.map(c=>c.row),(name,args)=>client.callTool({name,arguments:args}) as Promise<McpToolResult>,new AbortController().signal)
  const problems=checks.flatMap(c=>c.status==='complete'?c.findings.map(f=>f.message):[c.message??'Fact checks are unavailable.'])
  if(problems.length)throw Error(problems.slice(0,6).join('\n'))
  setProposal({saved:structuredClone(saved),cases,project:(await readTests()).project});setReviewed(false)
 }catch(e){setError((e as Error).message)}finally{setBusy(false)}}
 async function save(){if(!proposal||!reviewed)return;setBusy(true);setError('');try{
  const fresh=await dialectAPI<PreparationReply>('/'+proposal.saved.document.id)
  if(fresh.sha256!==proposal.saved.sha256)throw Error('Dialects changed. Close this dialog and review the selection again.')
  const currentPack=await livePack.refetch();if(currentPack.error||currentPack.data?.raw!==packText)throw Error('The pack changed. Review the dialect and snapshots again.')
  await updateTests(packId,suite=>appendDialectTestCases(suite,proposal.cases),proposal.project);onSaved()
 }catch(e){setError((e as Error).message)}finally{setBusy(false)}}
 return <Dialog open title={msg('Add cases from Dialects')} size="wide" openerRef={opener} onOpenChange={open=>{if(!open&&!busy)onClose()}} footer={<DialogActions><Button disabled={busy} onClick={()=>proposal?setProposal(undefined):onClose()}>{proposal?msg('Back'):msg('Cancel')}</Button><Button variant="primary" disabled={busy||status!=='ready'||(proposal?!reviewed:!compatible||!selected.length||selected.length>50||ineligible)} onClick={()=>void(proposal?save():prepare())}>{busy?<LoadingState inline>{msg('Checking inputs…')}</LoadingState>:proposal?msg('Add snapshots'):msg('Review snapshots')}</Button></DialogActions>}><div className={styles.stack}>
  {error&&<Notice tone="error">{error}</Notice>}
  {proposal?<>
   <p>{msg('{{count}} frozen records. Expected decisions are left unset; review each case against the pack policy before running tests.',{count:proposal.cases.length})}</p>
   {proposal.cases.map(c=><Disclosure key={c.id} title={c.name}><CodeBlock text={JSON.stringify(c.row,null,2)} lang="json"/></Disclosure>)}
   <label className="checkbox"><input type="checkbox" checked={reviewed} onChange={e=>setReviewed(e.target.checked)}/>{msg('I reviewed the prepared facts and evidence. Existing snapshots will be skipped.')}</label>
  </>:<>
   {!initial&&<Field label={msg('Dialects')}>{props=><Select {...props} value={collection} disabled={busy} options={(list.data?.items??[]).map(r=>({value:r.id,label:r.name}))} placeholder={msg('Choose a dialect')} onValueChange={setCollection}/>}</Field>}
   {(list.error||query.error)&&<Notice tone="error">{(list.error??query.error)?.message}</Notice>}
   {saved&&!compatible&&<Notice tone="warning">{msg('Save a dialect to this pack version in the Dialects recipe first.')} <Link to={'/dialects/'+saved.document.id+'?tab=recipe'}>{msg('Open recipe')}</Link></Notice>}
   <p className={styles.note}>{msg('Select up to 50 records. AI research must have a current accepted review. Creating a test preserves its origin; it does not verify the source or run a decision.')}</p>
   {ineligible&&<Notice tone="warning">{msg('Review the selected research records before preparing cases.')}</Notice>}
   {!recordIds&&<CollectionToolbar label={msg('Input records')} searchLabel={msg('Search records')} search={search} onSearch={setSearch}/>}
   <div className={styles.tableWrap}><SelectionTable selection={selection} className={styles.table}><thead><tr>{!recordIds&&<SelectionHeader selection={selection}/>}<th>{msg('Record')}</th><th>{msg('Origin')}</th></tr></thead><tbody>{rows.map(r=><tr key={r.id}>{!recordIds&&<SelectionCell selection={selection} id={r.id} name={r.name}/>}<td>{r.name}</td><td>{r.class==='generated'?msg('AI research'):msg('Supplied')}</td></tr>)}</tbody></SelectionTable></div>
  </>}
 </div></Dialog>
}
