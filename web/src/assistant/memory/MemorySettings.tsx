import {useUnsavedChanges} from '../../shell/DraftScope'
import {useRegisteredChanges} from '../../shell/UnsavedChanges'
import {privateQuery} from '../../data/privateQuery'
import {Disclosure} from '../../ui/Disclosure'
import {useState,useRef,useEffect} from 'react'
import {useQuery,useQueryClient,useMutation} from '@tanstack/react-query'
import {msg,formatDate} from '../../i18n'
import {Button} from '../../ui/Button'
import {Input} from '../../ui/Input'
import {TextArea} from '../../ui/TextArea'
import {Field} from '../../ui/Field'
import {Dialog,DialogActions} from '../../ui/Dialog'
import {LoadingState} from '../../ui/LoadingState'
import {SettingsFeedback} from '../../ui/SettingsFeedback'
import {CollectionToolbar} from '../../ui/CollectionToolbar'
import {MEMORY_KEY,memoryRequest,type MemoryEntry,type MemoryProposal,type MemoryReviewChange} from './client'
import styles from '../AIConnectionsSettings.module.css'
export function MemorySettings({disabled=false}:{disabled?:boolean}){
 const query=useQuery({...privateQuery,queryKey:MEMORY_KEY,queryFn:()=>memoryRequest(),retry:false}),cache=useQueryClient()
 const [search,setSearch]=useState(''),[filter,setFilter]=useState('current'),[editing,setEditing]=useState<Partial<MemoryEntry>|null>(null),[clearing,setClearing]=useState(false)
 const [proposalEdit,setProposalEdit]=useState<{proposal:MemoryProposal;text:string;revision:string}|null>(null)
 const editRevision=useRef<string|undefined>(undefined)
 const beginEdit=(entry:Partial<MemoryEntry>)=>{editRevision.current=query.data?.sha256;setEditing(entry)}
 const mutation=useMutation({mutationFn:(change:Record<string,unknown>)=>memoryRequest(change,(editing||clearing)?editRevision.current:query.data?.sha256),onSuccess:r=>{cache.setQueryData(MEMORY_KEY,r);setEditing(null);setClearing(false)},onError:()=>{void query.refetch()}})
 const review=useMutation({mutationFn:({change,revision}:{change:MemoryReviewChange;revision:string})=>memoryRequest({action:'review',review:change},revision),onSuccess:r=>{cache.setQueryData(MEMORY_KEY,r);setProposalEdit(null)},onError:()=>{void query.refetch()}})
 useUnsavedChanges(!!proposalEdit&&proposalEdit.text!==proposalEdit.proposal.memory.text)
 useRegisteredChanges(false,'',{kind:'activity',busy:review.isPending})
 const decide=(proposal:MemoryProposal,decision:'accept'|'decline'|'edit',text?:string,revision=query.data!.sha256)=>review.mutate({revision,change:{id:crypto.randomUUID(),runId:proposal.runId,runRevision:proposal.runRevision,proposalId:proposal.memory.id,proposalDigest:proposal.memory.digest,decision,...(text===undefined?{}:{text})}})
 if(query.isPending)return <LoadingState>{msg('Loading project memory…')}</LoadingState>
 if(query.isError&&!query.data)return <SettingsFeedback tone="error" actions={<Button onClick={()=>void query.refetch()}>{msg('Retry')}</Button>}>{msg('Project memory could not be loaded.')}</SettingsFeedback>
 const locked=disabled||mutation.isPending||review.isPending||query.isError,document=query.data!.content
 const settings=(key:'use'|'suggest',checked:boolean)=>mutation.mutate({action:'settings',settings:{...document.settings,[key]:checked}})
 const approve=(id:string)=>mutation.mutate({action:'approve',id})
 const forget=(id:string)=>mutation.mutate({action:'forget',id})
 const clear=()=>mutation.mutate({action:'clear'})
 const save=()=>editing&&mutation.mutate({action:editing.id?'edit':'save',id:editing.id,key:editing.key,text:editing.text,dependencies:editing.dependencies})
 const entries=document.entries.filter(e=>(filter==='all'||filter==='current'&&e.state!=='superseded'||e.state===filter)&&(`${e.key} ${e.text}`).toLowerCase().includes(search.toLowerCase()))
 return <section className={styles.stack} aria-label={msg('Project memory')}>
  {query.isError&&<SettingsFeedback tone="error" actions={<Button onClick={()=>void query.refetch()}>{msg('Retry')}</Button>}>{msg('Project memory could not be loaded.')}</SettingsFeedback>}
  <p className={styles.meta}>{msg('Keep useful preferences and corrections across chats in this project. Current saved objects and your latest instructions take precedence.')}</p>
  {(['use','suggest'] as const).map(key=><label key={key} className={styles.label}><input type="checkbox" checked={document.settings[key]} disabled={locked} onChange={e=>settings(key,e.target.checked)}/>{key==='use'?msg('Use saved memory'):msg('Suggest memories')}</label>)}
  <p className={styles.meta}>{msg('Suggestions require review. Turning memory off keeps saved entries. Memory does not grant approval to run workflows.')}</p>
  <CollectionToolbar label={msg('Memory toolbar')} searchLabel={msg('Search memories')} search={search} onSearch={setSearch} filters={[{label:msg('State'),value:filter,onValueChange:setFilter,options:[{value:'current',label:msg('Current')},{value:'active',label:msg('Saved')},{value:'pending',label:msg('Needs review')},{value:'all',label:msg('All states')}]}]} actions={()=> <><Button disabled={locked} onClick={()=>beginEdit({key:'',text:''})}>{msg('Add memory')}</Button><Button variant="quiet" disabled={locked||!document.entries.length&&!(query.data!.proposals??[]).length} onClick={()=>{editRevision.current=query.data?.sha256;setClearing(true)}}>{msg('Clear memory')}</Button></>}/>
  {(query.data!.proposals??[]).filter(p=>(filter==='all'||filter==='current'||filter==='pending')&&(`${p.memory.kind} ${p.memory.text}`).toLowerCase().includes(search.toLowerCase())).map(proposal=><div key={proposal.runId+'/'+proposal.memory.id} className={styles.row}>
   <div className={styles.info}><span>{proposal.memory.text}</span><span className={styles.meta}>{proposal.reviewable?msg('Needs review'):msg('Source changed')} · {formatDate(new Date(proposal.memory.createdAt),{dateStyle:'medium'})} · <a href={'/chats/'+encodeURIComponent(proposal.chatId)}>{msg('Source chat')}</a></span></div>
   <div className={styles.actions}><Button disabled={locked||!proposal.reviewable||new TextEncoder().encode(proposal.memory.text).length>2000} onClick={()=>decide(proposal,'accept')}>{msg('Remember')}</Button><Button variant="quiet" disabled={locked||!proposal.reviewable} onClick={()=>setProposalEdit({proposal,text:proposal.memory.text,revision:query.data!.sha256})}>{msg('Edit')}</Button><Button variant="quiet" disabled={locked||!proposal.reviewable} onClick={()=>decide(proposal,'decline')}>{msg('Decline')}</Button></div>
  </div>)}
  {!entries.length&&!(query.data!.proposals??[]).length&&<p className={styles.meta}>{msg('No memories match this view.')}</p>}
  {entries.map(entry=><div key={entry.id} className={styles.row}>
   <div className={styles.info}><span className={styles.name}>{entry.key}</span><span>{entry.text}</span><span className={styles.meta}>{entry.stale?msg('Source changed'):entry.state==='pending'?msg('Needs review'):entry.state==='superseded'?msg('Superseded'):msg('Saved')} · {formatDate(new Date(entry.updatedAt),{dateStyle:'medium'})}{entry.source&&<> · <a href={'/chats/'+encodeURIComponent(entry.source.chatId)}>{msg('Source chat')}</a></>}</span></div>
   <div className={styles.actions}>{entry.state==='pending'&&<Button disabled={locked||entry.stale} onClick={()=>approve(entry.id)}>{msg('Remember')}</Button>}<Button variant="quiet" disabled={locked} onClick={()=>beginEdit(entry)}>{msg('Edit')}</Button><Button variant="quiet" disabled={locked} onClick={()=>forget(entry.id)}>{msg('Forget')}</Button></div>
  </div>)}
  {review.error&&<SettingsFeedback tone="error">{review.error.message}</SettingsFeedback>}
  {proposalEdit&&<Dialog open title={msg('Edit memory')} onOpenChange={open=>{if(!open&&!review.isPending)setProposalEdit(null)}} footer={<DialogActions><Button disabled={locked} onClick={()=>setProposalEdit(null)}>{msg('Cancel')}</Button><Button disabled={locked||!proposalEdit.text.trim()||new TextEncoder().encode(proposalEdit.text).length>2000} onClick={()=>decide(proposalEdit.proposal,'edit',proposalEdit.text,proposalEdit.revision)}>{msg('Remember')}</Button></DialogActions>}><Field label={msg('Memory')}>{p=><TextArea {...p} value={proposalEdit.text} maxLength={2000} disabled={locked} onChange={e=>setProposalEdit({...proposalEdit,text:e.target.value})}/>}</Field>{review.error&&<SettingsFeedback tone="error">{review.error.message}</SettingsFeedback>}</Dialog>}
  {mutation.error&&<SettingsFeedback tone="error">{mutation.error.message}</SettingsFeedback>}
  {editing&&<Dialog open title={editing.id?msg('Edit memory'):msg('Add memory')} onOpenChange={open=>{if(!open&&!mutation.isPending)setEditing(null)}} footer={<DialogActions><Button disabled={locked} onClick={()=>setEditing(null)}>{msg('Cancel')}</Button><Button variant="primary" disabled={locked||!editing.text?.trim()||!editing.key?.match(/^[a-z0-9][a-z0-9-]{0,79}$/)} onClick={save}>{msg('Remember')}</Button></DialogActions>}>
   <div className={styles.stack}><Field label={msg('Topic key')} hint={msg('Use a short name with lowercase letters, numbers and hyphens.')}>{p=><Input {...p} disabled={locked||!!editing.id} value={editing.key??''} maxLength={80} onChange={e=>setEditing({...editing,key:e.target.value})}/>}</Field><Field label={msg('Memory')}>{p=><TextArea {...p} disabled={locked} value={editing.text??''} maxLength={2000} onChange={e=>setEditing({...editing,text:e.target.value})}/>}</Field>{mutation.error&&<SettingsFeedback tone="error">{mutation.error.message}</SettingsFeedback>}</div>
  </Dialog>}
  {clearing&&<Dialog open title={msg('Clear project memory?')} onOpenChange={open=>{if(!open&&!mutation.isPending)setClearing(false)}} footer={<DialogActions><Button disabled={locked} onClick={()=>setClearing(false)}>{msg('Cancel')}</Button><Button disabled={locked} onClick={clear}>{msg('Clear memory')}</Button></DialogActions>}><p>{msg('Remove all saved memories and suggestions. Older messages will be excluded from new suggestions. Chats and project files are kept.')}</p>{mutation.error&&<SettingsFeedback tone="error">{mutation.error.message}</SettingsFeedback>}</Dialog>}
 </section>
}
export function MemorySettingsDisclosure({disabled=false}:{disabled?:boolean}){
 const [open,setOpen]=useState(false)
 return <section id="assistant-memory" className={styles.group}><Button variant="quiet" aria-expanded={open} onClick={()=>setOpen(!open)}>{msg('Project memory')}</Button>{open&&<MemorySettings disabled={disabled}/>}</section>
}

/** Pending reviews come from durable memory, not transient tool events. */
export function ChatMemoryReview({chatId,observation}:{chatId:string;observation?:string}){
 const cache=useQueryClient()
 useEffect(()=>{if(observation!==undefined)void cache.invalidateQueries({queryKey:MEMORY_KEY})},[cache,observation])
 const query=useQuery({...privateQuery,queryKey:MEMORY_KEY,queryFn:()=>memoryRequest(),retry:false})
 if(!query.data?.content.entries.some(e=>e.source?.chatId===chatId&&e.state==='pending')&&!query.data?.proposals?.some(p=>p.chatId===chatId))return null
 return <Disclosure title={msg('Project memory')}><MemorySettings/></Disclosure>
}
