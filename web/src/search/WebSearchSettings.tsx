import { useCallback, useId, useMemo, useRef, useState } from 'react'
import { useQueryClient } from '@tanstack/react-query'
import { msg, useLocale } from '../i18n'
import { Disclosure } from '../ui/Disclosure'
import { searchFailureMessage } from './failures'
import { ConnectionRequestError } from '../connections/client'
import { Button, ButtonLink } from '../ui/Button'
import { Input } from '../ui/Input'
import { Select } from '../ui/Select'
import { ProviderIcon } from './ProviderIcon'
import { SettingsSection } from '../ui/SettingsSection'
import { connectionCall } from '../connections/client'
import { useInspectorPortal } from '../shell/InspectorSlot'
import { useInspectorPresentation } from '../shell/InspectorPresentation'
import { useDirtyGuard } from '../shell/useDirtyGuard'
import { useConfirmDiscard, TypedConfirmation } from '../shell/UnsavedChanges'
import { Dialog, DialogActions } from '../ui/Dialog'
import { SEARCH_CONNECTIONS_KEY, useSearchConnections, useSearchPreference, type SearchConnection, type SearchProvider, type SearchTimeout } from './connections'
import styles from './WebSearchSettings.module.css'

/** Only the per-desk research policy lives here; shared credentials live in Connections. */
export function WebSearchSettings(){
 useLocale()
 const connections=useSearchConnections(),preferences=useSearchPreference(),id=useId()
 useDirtyGuard(false, msg('Leaving will discard unsaved changes in the open editors.'), {busy:preferences.saving})
 const pref=preferences.data?.value
 return <SettingsSection title={msg('Research')} level={2} variant="standalone" description={msg('This desk · Choose when to search the web and which connection to use.')}><div className={styles.settings}>
  {preferences.isPending&&<p role="status">{msg('Loading…')}</p>}
  {pref&&<div className={styles.defaults}>
   <div className={styles.field}><label htmlFor={`${id}-mode`}>{msg('Web research')}</label><Select id={`${id}-mode`} value={pref.mode} disabled={preferences.saving||preferences.isError} options={[{value:'auto',label:msg('Search when needed')},{value:'provided',label:msg('Provided sources only')}]} onValueChange={mode=>void preferences.save({...pref,mode:mode as 'auto'|'provided'}).catch(()=>{})}/></div>
   <div className={styles.field}><label htmlFor={`${id}-connection`}>{msg('Default search connection')}</label><Select id={`${id}-connection`} value={pref.connection??'none'} disabled={!connections.available||connections.loading||connections.isError||preferences.saving||preferences.isError} options={[{value:'none',label:msg('No search connection')},...(connections.data?.connections??[]).map(c=>({value:c.id,label:c.name})),...(pref.connection&&!connections.data?.connections.some(c=>c.id===pref.connection)?[{value:pref.connection,label:msg('Connection unavailable')}]:[])]} onValueChange={connection=>void preferences.save({...pref,connection:connection==='none'?null:connection}).catch(()=>{})}/></div>
  </div>}
  {pref?.mode==='auto'&&!pref.connection&&<p className={styles.caption}>{msg('Select a search connection to enable automatic web research.')}</p>}
  <p className={styles.caption}>{msg('Search finds sources. Research reads and cites them. Reasoning effort is configured separately in Assistant.')}</p>
  <div className={styles.toolbar}><ButtonLink variant="quiet" to="/admin#connections-search">{msg('Manage search connections')}</ButtonLink><span className={styles.caption} role="status">{preferences.saving?msg('Saving…'):preferences.saveError?'':preferences.saved?msg('Saved'):msg('Changes save automatically.')}</span></div>
  {!connections.available&&!connections.loading&&<p>{msg('Web search requires an updated local gateway.')} <ButtonLink variant="inline" to="/admin#gateway">{msg('Manage gateway')}</ButtonLink></p>}
  {connections.loading&&<p role="status">{msg('Loading connections…')}</p>}
  {connections.isError&&<p role="alert">{msg('Search connections could not be loaded.')} <Button onClick={()=>void connections.refetch()}>{msg('Retry')}</Button></p>}
  {(preferences.isError||preferences.saveError)&&<p role="alert">{preferences.isError?msg('Search settings could not be read. Reload before making changes.'):msg('Search settings could not be saved. Reload before trying again.')} <Button onClick={()=>void preferences.refetch()}>{msg('Reload')}</Button></p>}
 </div></SettingsSection>
}

export function SearchConnectionsSettings(){
 useLocale()
 const connections=useSearchConnections()
 const [selected,setSelected]=useState<SearchConnection|null|undefined>(),[width,setWidth]=useState(480)
 const opener=useRef<HTMLElement|null>(null),closeRef=useRef<()=>void>(()=>{})
 const close=useCallback(()=>closeRef.current(),[])
 const presentation=useMemo(()=>({title:msg('Web search'),available:selected!==undefined,open:selected!==undefined,onOpenChange:(open:boolean)=>{if(!open)close()},width,onResize:setWidth,onReset:()=>setWidth(480),minimumMainWidth:560,maximumWidth:560,closeOnEscape:true,restoreFocusRef:opener}),[selected,width,close])
 useInspectorPresentation(selected!==undefined?presentation:null)
 const pane=useInspectorPortal(selected!==undefined&&connections.data?<SearchEditor key={selected?.id??'new'} original={selected} providers={connections.data.providers} timeout={connections.data.timeout} onClose={()=>setSelected(undefined)} registerClose={action=>{closeRef.current=action}}/>:null)
 return <SettingsSection title={msg('Web search')} variant="plain" description={msg('Shared on this computer · Credentials, request limits and timeouts.')}><div className={styles.settings}>
  <div className={styles.toolbar}><ButtonLink variant="quiet" to="/admin#research">{msg('Choose a default for this desk')}</ButtonLink><Button disabled={!connections.available||!connections.data||selected!==undefined} onClick={e=>{opener.current=e.currentTarget;setSelected(null)}}>{msg('Add connection')}</Button></div>
  {!connections.available&&!connections.loading&&<p>{msg('Web search requires an updated local gateway.')} <ButtonLink variant="inline" to="/admin#gateway">{msg('Manage gateway')}</ButtonLink></p>}
  {connections.loading&&<p role="status">{msg('Loading connections…')}</p>}
  {connections.isError&&<p role="alert">{msg('Search connections could not be loaded.')} <Button onClick={()=>void connections.refetch()}>{msg('Retry')}</Button></p>}
  <div className={styles.connections}>{connections.data?.connections.map(c=><div className={styles.connectionRow} key={c.id}>
   <ProviderIcon provider={c.provider}/><div><strong>{c.name}</strong><p>{connections.data.providers.find(p=>p.id===c.provider)?.name}</p><p>{msg('Configured')} · {msg('{{used}} of {{limit}} requests today',{used:c.requests??0,limit:c.dailyLimit})}</p></div>
   <Button variant="quiet" aria-label={msg('Manage {{name}}',{name:c.name})} disabled={selected!==undefined} onClick={e=>{opener.current=e.currentTarget;setSelected(c)}}>{msg('Manage')}</Button>
  </div>)}</div>
  {connections.data&&!connections.data.connections.length&&<p className={styles.caption}>{msg('Add a search connection to enable automatic web research.')}</p>}
  {pane}
 </div></SettingsSection>
}
function SearchEditor({original,providers,timeout,onClose,registerClose}:{original:SearchConnection|null;providers:SearchProvider[];timeout?:SearchTimeout;onClose:()=>void;registerClose:(close:()=>void)=>void}){
 const client=useQueryClient(),confirm=useConfirmDiscard(),id=useId()
 const initial={id:original?.id??`search-${crypto.randomUUID().slice(0,8)}`,revision:original?.revision??'',name:original?.name??'',provider:original?.provider??providers[0]?.id??'',project:original?.project??'',location:original?.location??'global',model:original?.model??'gemini-2.5-flash',dailyLimit:original?.dailyLimit??100,...(timeout?{timeoutSeconds:original?.timeoutSeconds??0}:{}),credential:''}
 const [form,setForm]=useState(initial),[baseline]=useState(initial),[busy,setBusy]=useState(false),[error,setError]=useState(''),[notice,setNotice]=useState(''),[removing,setRemoving]=useState(false),[typed,setTyped]=useState('')
 const dirty=JSON.stringify(form)!==JSON.stringify(baseline)
 const clearDirty=useDirtyGuard(dirty,msg('Leaving will discard unsaved changes in the open editors.'),{busy,shouldBlock:()=>true})
 const close=async()=>{if(busy)return;if(!dirty||await confirm(msg('Leaving will discard unsaved changes in the open editors.'))){clearDirty();onClose()}}
 registerClose(()=>void close())
 const provider=providers.find(p=>p.id===form.provider)
 function update(key:string,value:string|number){setForm(f=>({...f,[key]:value}));setError('');setNotice('')}
 async function act(action:'save'|'test'|'remove'){
  if(busy)return
  // The gateway refuses a timeout outside the bounds it advertised; refuse it here first, so nothing is sent.
  if(action==='save'&&timeout&&form.timeoutSeconds!==0&&(!Number.isSafeInteger(form.timeoutSeconds)||form.timeoutSeconds!<timeout.minSeconds||form.timeoutSeconds!>timeout.maxSeconds)){setError(msg('Choose a search timeout from {{min}} to {{max}} seconds.',{min:timeout.minSeconds,max:timeout.maxSeconds}));return}
  setBusy(true);setError('');setNotice('')
  try{
   if(action==='save'){
    const google=provider?.fields.includes('project')
    await connectionCall('configure',{...form,project:google?form.project:'',location:google?form.location:'',model:google?form.model:''},undefined,'web-search')
    setForm(f=>({...f,credential:''}));clearDirty();await client.invalidateQueries({queryKey:SEARCH_CONNECTIONS_KEY});onClose()
   }else if(action==='test'){
    await connectionCall('test',{id:form.id,revision:form.revision},undefined,'web-search');setNotice(msg('Connection test passed.'))
   }else{
    await connectionCall('disconnect',{id:form.id,revision:form.revision},undefined,'web-search');clearDirty();await client.invalidateQueries({queryKey:SEARCH_CONNECTIONS_KEY});onClose()
   }
  }catch(cause){setError(cause instanceof ConnectionRequestError&&cause.code==='search-timeout'?searchFailureMessage('search-timeout')!:action==='test'?msg('Connection test failed. Check credentials, permissions and quota.'):msg('The connection could not be saved. Check the fields or reload after another edit.'))}
  finally{setBusy(false);void client.invalidateQueries({queryKey:SEARCH_CONNECTIONS_KEY})}
 }
 const google=provider?.fields.includes('service-account-json')
 return <>
  <form className={styles.form} onSubmit={e=>{e.preventDefault();void act('save')}}>
   <h3>{original?msg('Edit connection'):msg('Add connection')}</h3>
   <label className={styles.field}>{msg('Name')}<Input required maxLength={80} value={form.name} disabled={busy} onChange={e=>update('name',e.target.value)}/></label>
   <div className={styles.field}><label htmlFor={`${id}-provider`}>{msg('Provider')}</label><Select id={`${id}-provider`} value={form.provider} disabled={busy} options={providers.map(p=>({value:p.id,label:p.name}))} onValueChange={provider=>{setForm(f=>({...f,provider,credential:''}));setNotice('')}}/></div>
   {provider?.fields.includes('project')&&<label className={styles.field}>{msg('Google Cloud project')}<Input required value={form.project} disabled={busy} onChange={e=>update('project',e.target.value)}/></label>}
   {provider?.fields.includes('location')&&<label className={styles.field}>{msg('Location')}<Input required value={form.location} disabled={busy} onChange={e=>update('location',e.target.value)}/></label>}
   {provider?.fields.includes('model')&&<label className={styles.field}>{msg('Model')}<Input required value={form.model} disabled={busy} onChange={e=>update('model',e.target.value)}/></label>}
   <label className={styles.field}>{google?msg('Service account JSON'):msg('API key')}{google?<textarea className={styles.credential} value={form.credential} disabled={busy} spellCheck={false} autoComplete="off" maxLength={8192} onChange={e=>update('credential',e.target.value)}/>:<Input type="password" autoComplete="new-password" value={form.credential} disabled={busy} maxLength={256} onChange={e=>update('credential',e.target.value)}/>}</label>
   {original&&form.provider===original.provider&&<p className={styles.caption}>{msg('Leave credentials blank to keep the saved value.')}</p>}
   <p className={styles.caption}>{msg('Credentials stay in Gateway and are never included in chat or desk files.')}</p>
   {provider&&<a href={provider.docs} target="_blank" rel="noreferrer">{msg('Setup instructions')}</a>}
   <label className={styles.field}>{msg('Daily request limit')}<Input type="number" required min={1} max={10000} value={form.dailyLimit} disabled={busy} onChange={e=>update('dailyLimit',Number(e.target.value))}/></label>
   <p className={styles.caption}>{msg('Testing makes one provider request and counts toward this limit. Limits reset at midnight UTC.')}</p>
   {timeout&&<Disclosure title={msg('Advanced settings')}><label className={styles.field}>{msg('Search timeout (seconds)')}<Input type="number" required min={timeout.minSeconds} max={timeout.maxSeconds} step={1} value={form.timeoutSeconds||timeout.defaultSeconds} disabled={busy} onChange={e=>update('timeoutSeconds',Number(e.target.value))}/></label><p className={styles.caption}>{msg('Maximum wait for one search, including authentication. Default: {{seconds}} seconds.',{seconds:timeout.defaultSeconds})}</p>{original?.timeoutRefused&&<p className={styles.caption} role="status">{msg('The saved timeout is outside the range this gateway supports. Saving sets the value shown.')}</p>}</Disclosure>}
   {error&&<p role="alert">{error}</p>}{notice&&<p role="status">{notice}</p>}
   <div className={styles.footer}>{original&&<Button variant="quiet" disabled={busy||dirty} onClick={()=>setRemoving(true)}>{msg('Remove connection')}</Button>}<Button disabled={busy||dirty||!form.revision} onClick={()=>void act('test')}>{msg('Test connection')}</Button><Button onClick={()=>void close()} disabled={busy}>{msg('Cancel')}</Button><Button type="submit" variant="primary" disabled={busy||!form.name.trim()||!dirty}>{busy?msg('Saving…'):msg('Save')}</Button></div>
  </form>
  <Dialog open={removing} onOpenChange={setRemoving} title={msg('Remove connection')} description={msg('Saved sources remain available. Future searches using this connection will stop.')} footer={<DialogActions><Button disabled={busy} onClick={()=>setRemoving(false)}>{msg('Cancel')}</Button><Button variant="danger" disabled={busy||typed.trim()!==form.name.trim()} onClick={()=>void act('remove')}>{msg('Remove')}</Button></DialogActions>}><TypedConfirmation value={typed} onChange={setTyped} confirmation={form.name} disabled={busy}/>{error&&<p role="alert">{error}</p>}</Dialog>
 </>
}
