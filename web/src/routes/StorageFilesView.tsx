import { useEffect, useRef, useState, type CSSProperties } from 'react'
import { useParams } from 'react-router-dom'
import { msg, systemMessage, useLocale } from '../i18n'
import { useEffectiveConfig } from '../config/DeskConfigProvider'
import { useConnections } from '../connections/catalog'
import { ConnectionRequestError } from '../connections/client'
import { providerName } from '../connections/registry'
import { decodeText, encodeBytes, encodeEditedText, storageCall, storageError, STORAGE_MAX_BYTES, type StorageFile, type StoragePage, type StorageRead, type StoragePlan } from '../connections/storage'
import { activeDeskId } from '../desks/scope'
import { useDirtyGuard } from '../shell/useDirtyGuard'
import { TypedConfirmation, useConfirmDiscard } from '../shell/UnsavedChanges'
import { IconDetails, IconFolder } from '../shell/icons'
import { Button, ButtonLink } from '../ui/Button'
import { Input } from '../ui/Input'
import { Dialog, DialogActions } from '../ui/Dialog'
import { PageHeader } from '../ui/PageLayout'
import { PaneDivider } from '../ui/PaneDivider'
import { formatStorageBytes } from '../admin/chatStorage'
import styles from './AuthorView.module.css'
import own from './StorageFilesView.module.css'

export function StorageFilesView() {
  const {provider = ''} = useParams()
  return <article className="detail authoring" data-measure="full" data-layout="page">
    <StorageFiles key={provider} provider={provider} />
  </article>
}
export function StorageFiles({provider}: {provider:string}) {
 useLocale()
 const effective = useEffectiveConfig()
 const available = effective.desk?.localGateway?.status === 'ready' && !effective.desk?.decoded?.values?.research?.gateway
 const catalog = useConnections(available), entry = catalog.entries.find(item=>item.descriptor.id===provider)
 const ready = Boolean(available && entry?.status.data?.state==='connected' && entry.descriptor.operations.includes('files-list'))
 const title = providerName(provider, entry?.descriptor)
 const [page,setPage]=useState<StoragePage>(), [folder,setFolder]=useState(''), [query,setQuery]=useState('')
 const [submitted,setSubmitted]=useState({folder:'',query:''})
 const [selected,setSelected]=useState<StorageFile>(), [creating,setCreating]=useState(false), [name,setName]=useState('')
 const [base,setBase]=useState<string>(), [content,setContent]=useState<string>(), [buffer,setBuffer]=useState<string>()
 const [media,setMedia]=useState('text/plain'), [busy,setBusy]=useState(false), [error,setError]=useState(''), [notice,setNotice]=useState('')
 const [plan,setPlan]=useState<StoragePlan>(), [review,setReview]=useState(false), [typed,setTyped]=useState('')
 const uploadInput=useRef<HTMLInputElement>(null), frame=useRef<HTMLDivElement>(null), opener=useRef<HTMLElement|null>(null), lock=useRef(false), alive=useRef(true)
 const [room,setRoom]=useState(1000), [width,setWidth]=useState(280), [browsing,setBrowsing]=useState(true), [collapsed,setCollapsed]=useState(false), [createFolder,setCreateFolder]=useState(''), [createContext,setCreateContext]=useState('')
 const compact=room<640, maxWidth=Math.min(480,Math.max(220,room-360)), paneWidth=Math.min(width,maxWidth)
 const confirmDiscard=useConfirmDiscard()
 const currentContent=()=>buffer===undefined?content:encodeEditedText(buffer,content)
 const dirty=creating ? Boolean(name || content || buffer) : selected!==undefined && (buffer!==undefined ? buffer!==decodeText(base??'') : content!==base)
 useDirtyGuard(dirty,msg('This file has unsaved changes that will be lost. Leave anyway?'),{name:creating?name:selected?.name,busy})
 const pendingKey=`jpack.storage-plan.v1:${activeDeskId || "default"}:${provider}`
 const remember=(value:StoragePlan|undefined)=>{setPlan(value);try {if(value)sessionStorage.setItem(pendingKey,value.id);else sessionStorage.removeItem(pendingKey)}catch{/* Optional recovery hint; Gateway owns the durable plan. */}}
 useEffect(()=>{alive.current=true; const el=frame.current;if(!el)return;const observer=new ResizeObserver(()=>setRoom(el.getBoundingClientRect().width));observer.observe(el);return()=>{alive.current=false;observer.disconnect()}},[])
 async function run(work:()=>Promise<void>) {if(lock.current)return;lock.current=true;setBusy(true);setError('');try{await work()}catch(e){if(alive.current)setError(storageError(e))}finally{lock.current=false;if(alive.current)setBusy(false)}}
 async function list(q={folder,query},pageToken='') {const result=await storageCall<StoragePage>(provider,'files-list',{...q,pageToken});if(alive.current){setPage(result);setSubmitted(q)}}
 useEffect(()=>{if(!ready)return;void run(async()=>{
   let id:string|null=null, recoveryError:unknown
   try{id=sessionStorage.getItem(pendingKey)}catch{/* Optional */}
   // Recover before browsing: listing failure must never hide an unresolved write.
   if(id){try{
     const p=await storageCall<StoragePlan>(provider,'files-status',{id})
     if(p.state==='completed'){remember(undefined);setNotice(msg('Change completed.'))}
     else {remember(p);setReview(true)}
    }catch(cause){
     if(cause instanceof ConnectionRequestError && ['selection-expired','source-changed'].includes(cause.code)) remember(undefined)
     else {remember({id,action:'update',target:'',name:title,revision:'',sizeBytes:0,state:'needs-attention',expires:'',confirmation:'',effect:'write',error:'operation-uncertain'});setReview(true)}
     recoveryError=cause
    }}
   await list({folder:'',query:''})
   if(recoveryError)throw recoveryError
  })},[ready])
 const canLeave=()=>!dirty?Promise.resolve(true):confirmDiscard(msg('Discard unsaved changes to this file?'),{name:creating?name:selected?.name})
 function clearEditor(){setSelected(undefined);setCreating(false);setName('');setBase(undefined);setContent(undefined);setBuffer(undefined);setNotice('')}
 async function choose(file:StorageFile) {if(busy||!await canLeave())return;void run(async()=>{clearEditor();if(file.kind==='folder'){setFolder(file.id);setQuery('');await list({folder:file.id,query:''});return}setSelected(file);setBrowsing(false);setMedia(file.mediaType);const read=await storageCall<StorageRead>(provider,'files-read',{id:file.id,revision:file.revision,context:file.context});if(!alive.current)return;setSelected(read.file);setMedia(read.file.mediaType);setBase(read.contentBase64);setContent(read.contentBase64);setBuffer(decodeText(read.contentBase64))})}
 async function reload() {
  if (!selected || busy || !await canLeave()) return
  void run(async()=>{
   const q=provider==='obsidian'?{folder:selected.id.includes('/')?selected.id.slice(0,selected.id.lastIndexOf('/')):'',query:selected.name}:provider==='aws-s3'?{folder:selected.id,query:''}:{folder:'',query:selected.name}
   const page=await storageCall<StoragePage>(provider,'files-list',q)
   const file=page.items.find(item=>item.id===selected.id)
   if(!file||file.context!==selected.context)throw new Error(msg('Find and reopen the file to load its latest version.'))
   const read=await storageCall<StorageRead>(provider,'files-read',{id:file.id,revision:file.revision,context:file.context})
   setSelected(read.file);setMedia(read.file.mediaType);setBase(read.contentBase64);setContent(read.contentBase64);setBuffer(decodeText(read.contentBase64))
   await list(submitted)
  })
 }
 async function fresh(){if(busy||plan||!await canLeave())return;clearEditor();setCreating(true);setCreateFolder(submitted.folder);setCreateContext(page?.context??'');setMedia('text/plain');setContent('');setBuffer('');setBrowsing(false)}
 async function upload(file:File|undefined){if(!file||busy)return;void run(async()=>{if(file.size>STORAGE_MAX_BYTES)throw new Error(msg('Choose a file no larger than 4 MiB.'));const value=encodeBytes(new Uint8Array(await file.arrayBuffer()));setContent(value);setBuffer(decodeText(value));setMedia(file.type?.split(';')[0]||'application/octet-stream');if(creating)setName(file.name)})}
 async function prepare(action:'create'|'update'|'delete') {opener.current=document.activeElement as HTMLElement;void run(async()=>{const p=await storageCall<StoragePlan>(provider,'files-prepare',action==='delete'?{action,id:selected!.id,revision:selected!.revision,context:selected!.context}:{action,...(action==='create'?{folder:createFolder,name:name.trim(),context:createContext}:{id:selected!.id,revision:selected!.revision,context:selected!.context}),mediaType:media,contentBase64:currentContent()});remember(p);setTyped('');setReview(true)})}
 async function commit(check=false){if(!plan)return;void run(async()=>{let p:StoragePlan;try{p=await storageCall<StoragePlan>(provider,check?'files-status':'files-commit',{id:plan.id,...(!check&&plan.action==='delete'?{confirmation:typed}:{})})}catch(e){if(!check){remember({...plan,state:'needs-attention',error:'operation-uncertain'})}throw e}remember(p);if(p.state==='completed'){remember(undefined);setReview(false);clearEditor();setBrowsing(true);setNotice(msg('Change completed.'));await list(submitted)}})}
 function download(){if(base===undefined||!selected)return;const bytes=Uint8Array.from(atob(base),c=>c.charCodeAt(0));const url=URL.createObjectURL(new Blob([bytes],{type:'application/octet-stream'}));const link=document.createElement('a');link.href=url;link.download=selected.name.split('/').at(-1)!;link.click();setTimeout(()=>URL.revokeObjectURL(url),1000)}
 const blocked=busy||!ready||Boolean(plan), editor=selected||creating
 const status=plan?.state==='needs-attention'?msg('The result is uncertain. Check the file at its source before making another change.'):plan?.state==='refused'?msg('This change was refused. Reload the file before trying again.'):undefined
 return <>
  <div ref={frame} className={styles.frame} data-compact={compact||undefined} style={{'--files-width':`${paneWidth}px`} as CSSProperties}>
   <aside className={styles.browser} id="storage-browser" aria-label={msg('Browse files')} hidden={compact?!browsing:collapsed}>
    <PageHeader title={title} actions={<ButtonLink variant="quiet" to="/admin#connections">{msg('Connections')}</ButtonLink>}/>
    <form className={own.search} onSubmit={e=>{e.preventDefault();void run(()=>list())}}>
     <label>{provider==='aws-s3'?msg('Prefix'):msg('Folder')}<Input value={folder} onChange={e=>setFolder(e.target.value)} disabled={busy} /></label>
     <label>{msg('Search files')}<Input value={query} onChange={e=>setQuery(e.target.value)} disabled={busy} /></label>
     <div className={styles.editorActions}><Button type="submit" disabled={busy||!ready}>{msg('Search')}</Button><Button disabled={blocked} onClick={()=>void fresh()}>{msg('New file')}</Button></div>
     <p className="meta">{provider==='google-drive'?msg('Only files authorized for this app. Search uses the Drive index.'):msg('Search matches file names within this location. Contents load only when opened.')}</p>
    </form>
    <div className={styles.fileScroll}>
     {!ready&&<p className="note">{catalog.loading?msg('Loading…'):msg('Connect a supported storage integration in Admin → Connections.')}</p>}
     {ready&&page&&<><ul className={styles.fileList}>{page.items.map(file=><li key={file.id}><button className={styles.fileEntry} disabled={busy} aria-current={selected?.id===file.id?true:undefined} onClick={()=>void choose(file)}>{file.kind==='folder'?<IconFolder/>:<IconDetails/>}<span className={styles.fileText}><span title={file.name}>{file.name}</span><small title={file.id}>{submitted.query?file.id:file.kind==='file'?formatStorageBytes(file.sizeBytes):msg('Folder')}</small></span></button></li>)}</ul>
      {page.items.length===0&&<p className="note">{msg('No files on this page.')}</p>}
      {page.truncated&&<p className="note note-warn">{msg('Results are incomplete. Narrow the folder or search.')}</p>}
      {page.nextPageToken&&<Button variant="quiet" disabled={busy} onClick={()=>void run(()=>list(submitted,page.nextPageToken))}>{msg('Next page')}</Button>}
     </>}
    </div>
    {!compact&&<PaneDivider paneSide="start" label={msg('Resize file pane')} controls="storage-browser" value={paneWidth} min={220} max={maxWidth} onChange={setWidth} onReset={()=>setWidth(280)} onCollapse={()=>setCollapsed(true)} preview={{element:frame.current,property:'--files-width'}}/>}
   </aside>
   <section className={styles.detail} hidden={compact&&browsing} aria-label={msg('Editor')}>
    <PageHeader title={creating?msg('New file'):selected?.name??msg('Storage files')} leading={(compact||collapsed)&&<Button variant="quiet" onClick={()=>{setBrowsing(true);setCollapsed(false)}}>{msg('Files')}</Button>} meta={dirty?msg('unsaved changes'):undefined} actions={editor&&<>
     {selected?.deletable&&<Button variant="quiet" disabled={blocked} onClick={()=>void prepare('delete')}>{msg('Delete file')}</Button>}
     {(creating||selected?.editable)&&<Button variant="primary" disabled={blocked||!dirty||creating&&!name.trim()||content===undefined} onClick={()=>void prepare(creating?'create':'update')}>{msg('Review change')}</Button>}
    </>}/>
    <div className={styles.editorBody}>
     {error&&<p role="alert" className="note note-warn">{systemMessage(error)}</p>}
     {notice&&<p role="status" className="note">{notice}</p>}
     {busy&&<p role="status" className="meta">{msg('Working…')}</p>}
     {plan&&!review&&<Button onClick={()=>setReview(true)}>{msg('Review change')}</Button>}
     {creating&&<p className="meta">{title} · {createFolder || msg('Root folder')}</p>}
     {creating&&<label>{msg('File name')}<Input value={name} disabled={blocked} onChange={e=>setName(e.target.value)}/></label>}
     {selected&&<div className={styles.fileMeta}><code title={selected.id}>{selected.id}</code><span>{formatStorageBytes(selected.sizeBytes)}</span></div>}
     {buffer!==undefined&&<><label className={styles.editorLabel} htmlFor="storage-buffer">{msg('File contents')}</label><textarea id="storage-buffer" className={`code-editor ${styles.buffer}`} spellCheck={false} value={buffer} disabled={blocked||!creating&&!selected?.editable} onChange={e=>setBuffer(e.target.value)}/></>}
     {!editor&&<div className={styles.empty}><IconDetails/><p>{msg('Choose a file to edit.')}</p></div>}
     {editor&&<div className={styles.editorActions}>
      {(creating||selected?.editable)&&<><Button variant="quiet" disabled={blocked} onClick={()=>uploadInput.current?.click()}>{creating?msg('Upload file'):msg('Replace file')}</Button><input ref={uploadInput} type="file" hidden disabled={blocked} onChange={e=>{void upload(e.target.files?.[0]);e.target.value=''}}/></>}
      {selected&&provider==='google-drive'&&<ButtonLink variant="quiet" to={`https://drive.google.com/open?id=${encodeURIComponent(selected.id)}`} target="_blank" rel="noopener noreferrer">{msg('Open original source')}</ButtonLink>}
      {base!==undefined&&<Button variant="quiet" disabled={busy} onClick={download}>{msg('Download')}</Button>}
      {selected&&<Button variant="quiet" disabled={blocked} onClick={()=>void reload()}>{msg('Reload')}</Button>}
     </div>}
    </div>
   </section>
  </div>
  {compact&&browsing&&error&&<p role="alert" className="note note-warn">{systemMessage(error)}</p>}
  <Dialog open={review&&Boolean(plan)} onOpenChange={open=>{if(!busy)setReview(open)}} openerRef={opener} title={plan?.action==='delete'?msg('Delete file'):msg('Review change')} description={plan?.effect==='delete'?msg('S3 deletion may be permanent. Confirm the full object key.'):plan?.effect==='trash'?msg('This file will move to trash.'):msg('Apply this change to the connected storage.')} footer={<DialogActions><Button disabled={busy} onClick={()=>{setReview(false);if(plan?.state==='prepared'||plan?.state==='refused')remember(undefined)}}>{msg('Close')}</Button>{plan?.state==='prepared'?<Button variant={plan.action==='delete'?'danger':'primary'} disabled={busy||plan.action==='delete'&&typed!==plan.confirmation} onClick={()=>void commit()}>{plan.action==='delete'?msg('Delete file'):msg('Save')}</Button>:plan?.state==='needs-attention'?<Button disabled={busy} onClick={()=>void commit(true)}>{msg('Check status')}</Button>:null}</DialogActions>}>
   <p className={own.target}>{plan?.name}</p><p className="meta">{plan?.target}</p>
   {plan?.action!=='delete'&&plan&&<p className="meta">{formatStorageBytes(plan.sizeBytes)}</p>}
   {plan?.action==='delete'&&plan.state==='prepared'&&<TypedConfirmation confirmation={plan.confirmation} value={typed} onChange={setTyped} disabled={busy}/>}
   {status&&<p role="status" className="note note-warn">{status}</p>}
   {error&&<p role="alert" className="note note-warn">{systemMessage(error)}</p>}
  </Dialog>
 </>
}
