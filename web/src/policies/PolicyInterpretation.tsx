import {PolicySourceIdentity} from './PolicySourceIdentity'
import {useRef,useState} from 'react'
import {useNavigate} from 'react-router-dom'
import {useChats} from '../chat/ChatProvider'
import {ChatAction,ChatActions} from '../chat/ChatActions'
import {IconPack,IconGraph,IconAssistant} from '../shell/icons'
import {chatHref} from '../chat/navigation'
import {Dialog,DialogActions} from '../ui/Dialog'
import {Notice} from '../ui/Notice'
import {Disclosure} from '../ui/Disclosure'
import {msg,useLocale} from '../i18n'
import {useRegisteredChanges} from '../shell/UnsavedChanges'
import {usePolicyEditorBlocked} from './chatEditing'
import {preparePolicyInterpretation,openPolicyInterpretation,type PolicyInterpretationProposal} from './interpretation'
import type {PolicyAuthoringSource,PolicySourceSelection} from './authoringSource'
import styles from './PolicyPage.module.css'

/** Both the Policy editor and accepted chat handoffs review the same preparation. */
export function PolicyInterpretationAction({selection,disabled=false,proposal,chatId,artifactKind=proposal?.artifactKind??'pack'}:{selection:PolicySourceSelection;disabled?:boolean;proposal?:PolicyInterpretationProposal;chatId?:string;artifactKind?:'pack'|'graph'}){
 useLocale()
 const {store}=useChats(),navigate=useNavigate(),editorBlocked=usePolicyEditorBlocked(selection.policyId)
 const [source,setSource]=useState<PolicyAuthoringSource>(),[busy,setBusy]=useState(false),[error,setError]=useState(''),flight=useRef(false),opened=useRef(proposal?.openedChatId)
 const blocked=disabled||editorBlocked,blockedRef=useRef(blocked);blockedRef.current=blocked
 const clearActivity=useRegisteredChanges(false,'',{kind:'activity',busy})
 const review=async()=>{
  if(blockedRef.current||flight.current)return
  flight.current=true;setBusy(true);setError('')
  try{const exact=await preparePolicyInterpretation(selection);if(!blockedRef.current)setSource(exact)}catch{setError(msg('The selected Policy source could not be read. Sign in or reload before continuing.'))}finally{flight.current=false;setBusy(false)}
 }
 const open=async()=>{
  if(!store||!source||blockedRef.current||flight.current)return
  flight.current=true;setBusy(true);setError('')
  try{
   // Recheck current access without substituting a later Policy head.
   const exact=await preparePolicyInterpretation(selection)
   if(blockedRef.current)return
   if(JSON.stringify(exact)!==JSON.stringify(source))throw Error('source changed')
   const parent=chatId?[...store.getSnapshot().chats,...store.getSnapshot().drafts].find(c=>c.id===chatId):undefined
   const prior=parent?.policyInterpretations?.find(p=>p.id===proposal?.id)?.openedChatId??opened.current??proposal?.openedChatId
   const chat=openPolicyInterpretation(store,source,prior,artifactKind);opened.current=chat.id
   if(proposal&&chatId){const parent=[...store.getSnapshot().chats,...store.getSnapshot().drafts].find(c=>c.id===chatId);if(parent){store.update(chatId,{policyInterpretations:parent.policyInterpretations?.map(p=>p.id===proposal.id?{...p,openedChatId:chat.id}:p)})}}
   await store.checkpoint(new AbortController().signal)
   setSource(undefined);clearActivity();navigate(chatHref(chat))
  }catch{setError(msg('The authoring conversation could not be opened. Save or recover chat history before continuing.'))}finally{flight.current=false;setBusy(false)}
 }
 return <><ChatAction icon={artifactKind==='graph'?<IconGraph/>:<IconPack/>} disabled={blocked||busy} onClick={()=>void review()}>{proposal?.openedChatId||opened.current?msg('Open authoring chat'):(artifactKind==='graph'?msg('Design graph'):msg('Design pack'))}</ChatAction>{error&&!source&&<Notice tone="warning">{error}</Notice>}
 <Dialog open={!!source} onOpenChange={open=>{if(!open&&!busy)setSource(undefined)}} title={(artifactKind==='graph'?msg('Design graph from Policy'):msg('Design pack from Policy'))} footer={<DialogActions><ChatAction disabled={busy} onClick={()=>setSource(undefined)}>{msg('Cancel')}</ChatAction><ChatAction icon={<IconAssistant/>} disabled={blocked||busy||!store} onClick={()=>void open()}>{msg('Open authoring chat')}</ChatAction></DialogActions>}>
  {source&&<><PolicySourceIdentity source={source}/><Disclosure title={msg('Saved policy text')}><pre className={styles.snapshot}>{source.narrative}</pre></Disclosure></>}
  <p>{(artifactKind==='graph'?msg('Open an ordinary graph authoring chat with an unsent request. Review and send it when ready.'):msg('Open an ordinary pack authoring chat with an unsent request. Review and send it when ready.'))}</p>
  {error&&<Notice tone="warning">{error}</Notice>}
 </Dialog></>
}
export function PolicyInterpretationProposals({chatId,disabled=false,history=false}:{chatId:string;disabled?:boolean;history?:boolean}){
 useLocale()
 const {chats,drafts,bindings,store}=useChats(),chat=[...chats,...drafts].find(c=>c.id===chatId)
 const proposals=chat?.policyInterpretations??[],state=bindings?.get(chatId)?.state??chat?.checkpoint?.state
 const finished=!!state&&['complete','ready'].includes(state.status)&&!state.unknowns.length&&!state.questions?.some(q=>!q.resolution)
 return <>{(history?proposals:proposals.slice(-1)).map(p=><section key={p.id} aria-label={(p.artifactKind==='graph'?msg('Proposed graph interpretation'):msg('Proposed pack interpretation'))}><ChatActions detail={<>{(p.artifactKind==='graph'?msg('Proposed graph interpretation'):msg('Proposed pack interpretation'))} · {p.publicationId?msg('Selected publication'):msg('Saved revision · no publication selected')}</>}><PolicyInterpretationAction chatId={chatId} proposal={p} selection={p.publicationId?{policyId:p.policyId,publicationId:p.publicationId}:{policyId:p.policyId,revisionId:p.revisionId}} disabled={disabled||!finished||chat?.interaction==='plan'||!!store?.running}/></ChatActions></section>)}</>
}
