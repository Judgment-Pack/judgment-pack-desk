import {LoadingState} from '../ui/LoadingState'
import { useEffect, useId, useRef, useState, type RefObject } from 'react'
import { useQueryClient } from '@tanstack/react-query'
import { msg, useLocale } from '../i18n'
import { FileRequestError } from '../files/client'
import { Button } from '../ui/Button'
import { Dialog } from '../ui/Dialog'
import { Notice } from '../ui/Notice'
import { SettingsActions, SettingsFeedback } from '../ui/SettingsFeedback'
import { SettingRow } from '../ui/SettingRow'
import { SettingsSection } from '../ui/SettingsSection'
import { applyWebAccess, useWebAccess, WEB_ACCESS_KEY } from './webAccess'
import styles from './WebAccessSettings.module.css'

/** The same policy, explicit write and recovery semantics in Admin and dialogs. */
function useWebAccessForm(onClose?: () => void) {
 useLocale()
 const query=useWebAccess(), cache=useQueryClient(), hint=useId()
 const editBase=useRef<typeof query.data>(undefined)
 const [enabled,setEnabled]=useState(false),[busy,setBusy]=useState(false),[error,setError]=useState(''),[saved,setSaved]=useState(false)
 useEffect(()=>{if(query.data&&!editBase.current)setEnabled(query.data.enabled)},[query.data?.revision])
 const data=query.data, editable=!!data?.local&&data.supported&&!data.managed&&!query.isError
 const dirty=!!data&&enabled!==data.enabled
 const apply=async()=>{
  if(!data||!editable||busy||!dirty||error)return
  setBusy(true);setError('');setSaved(false)
  try {const result=await applyWebAccess(enabled,(editBase.current??data).revision);editBase.current=undefined;cache.setQueryData(WEB_ACCESS_KEY,result);setEnabled(result.enabled);setSaved(true);onClose?.()}
  catch(cause){setError(cause instanceof FileRequestError&&cause.code==='stale'?msg('Web access settings changed. Refresh and review them before applying.'):msg('Could not confirm whether web access settings were saved. Reload settings before trying again.'))}
  finally{setBusy(false)}
 }
 const fields=query.isPending?<LoadingState>{msg('Loading…')}</LoadingState>:query.isError?<Notice tone="error" actions={<Button disabled={query.isFetching} onClick={()=>void query.refetch()}>{msg('Retry')}</Button>}>{msg('Web access settings could not be loaded.')}</Notice>:<>
  <SettingRow separator={false} title={msg('Read websites linked by Google search')} description={<span id={hint}>{msg('Allow Google search results to open the publisher’s website.')} {msg('Other cross-website redirects remain blocked.')}</span>}
   action={<Button role="switch" aria-checked={enabled} aria-label={msg('Read websites linked by Google search')} aria-describedby={hint} disabled={!editable||busy} onClick={()=>{editBase.current??=data;setEnabled(!enabled);setSaved(false)}}>{enabled?msg('On'):msg('Off')}</Button>}/>
  {!editable&&<p className={styles.hint}>{data?.managed?msg('Managed by your organization'):!data?.local?msg('Configure web access on your external gateway.'):msg('Update the local gateway to configure web access.')}</p>}
 </>
 const feedback=<SettingsFeedback tone={error?'error':saved?'success':'neutral'} actions={error&&<Button disabled={busy||query.isFetching} onClick={()=>{void query.refetch().then(result=>{if(!result.isError&&result.data){editBase.current=undefined;setEnabled(result.data.enabled);setError('');setSaved(false)}})}}>{msg('Reload settings')}</Button>}>
  {error||(busy?msg('Applying…'):saved?msg('Changes saved'):dirty?msg('Unsaved changes'):null)}
 </SettingsFeedback>
 const actions=<>
  {(onClose||editable&&dirty)&&<Button disabled={busy} onClick={()=>{editBase.current=undefined;if(data)setEnabled(data.enabled);setError('');setSaved(false);onClose?.()}}>{msg('Cancel')}</Button>}
  {editable&&<Button variant="primary" disabled={busy||!dirty||!!error} onClick={()=>void apply()}>{busy?msg('Applying…'):msg('Apply changes')}</Button>}
 </>
 return {fields,feedback,actions,busy,editable,warning:editable&&dirty&&!saved?msg('Applying changes briefly interrupts source connections.'):undefined}
}

/** Admin keeps a titled section and its existing action footer. */
export function WebAccessSettings() {
 const form=useWebAccessForm()
 return <SettingsSection title={msg('Web access')} variant="plain" footer={form.editable?<SettingsActions hint={form.warning} feedback={form.feedback}>{form.actions}</SettingsActions>:undefined}>
  {form.fields}
 </SettingsSection>
}

/** A single modal header and fixed footer; explanations stay with the fields. */
export function WebAccessDialog({onClose,openerRef,retryContext}:{onClose:()=>void;openerRef:RefObject<HTMLElement|null>;retryContext:'research'|'chat'}) {
 const form=useWebAccessForm(onClose)
 return <Dialog open onOpenChange={open=>{if(!open&&!form.busy)onClose()}} title={msg('Web access')} openerRef={openerRef}
  footer={<SettingsActions feedback={form.feedback}>{form.actions}</SettingsActions>}>
  <div className={styles.dialogBody}>
   {form.fields}
   {form.warning&&<p className={styles.hint}>{form.warning}</p>}
   <p className={styles.hint}>{retryContext==='research'?msg('Research won’t restart automatically.'):msg('The page read won’t restart automatically.')}</p>
  </div>
 </Dialog>
}

/** Keep the opener available after a save so focus returns to the same control. */
export function ChatWebAccessRecovery({disabled=false}:{disabled?:boolean}) {
 const query=useWebAccess(),[open,setOpen]=useState(false),opener=useRef<HTMLButtonElement>(null)
 useLocale()
 const data=query.data, editable=!query.isError&&!!data?.local&&data.supported&&!data.managed
 return <>
  {editable&&<div className={styles.recovery}><span className={styles.problem}>{msg('Website read was blocked.')}</span><Button ref={opener} disabled={disabled} onClick={()=>setOpen(true)}>{msg('Review web access')}</Button></div>}
  {open&&<WebAccessDialog onClose={()=>setOpen(false)} openerRef={opener} retryContext="chat"/>}
 </>
}
