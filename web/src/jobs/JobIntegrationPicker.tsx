import { useEffect, useId, useLayoutEffect, useRef, useState } from 'react'
import { createPortal } from 'react-dom'
import { useQuery, useQueryClient } from '@tanstack/react-query'
import { msg } from '../i18n'
import { useEffectiveConfig } from '../config/DeskConfigProvider'
import { useConnections } from '../connections/catalog'
import { useConnectionsPane } from '../connections/ConnectionPaneContext'
import { connectionState } from '../connections/ConnectionDirectory'
import { providerName } from '../connections/registry'
import { ProviderIcon } from '../connections/ProviderIcon'
import { Disclosure } from '../ui/Disclosure'
import { Dialog, DialogActions } from '../ui/Dialog'
import { Button } from '../ui/Button'
import { Input } from '../ui/Input'
import { IconFolder, IconGear, IconPlus } from '../shell/icons'
import { jobsAPI } from './client'
import type { MappingSource, ProfileEntry } from './mappingTypes'
import { calculatorLabel } from './calculatedValues'
import { matchesJobGateway, supportsJobProfile } from './jobIntegrations'
import styles from './JobsView.module.css'

export function JobIntegrationPicker({profiles,disabled,onPick,current,triggerTarget}: {
 profiles:ProfileEntry[]; disabled:boolean; onPick:(entry?:ProfileEntry)=>void; current?:MappingSource; triggerTarget?:HTMLElement|null
}) {
 const effective=useEffectiveConfig(),config=effective.config.research
 const available=effective.desk?.localGateway?.status==='ready' && !effective.desk?.decoded?.values?.research?.gateway
 const catalog=useConnections(available), pane=useConnectionsPane(), client=useQueryClient(), id=useId()
 const [open,setOpen]=useState(false),[search,setSearch]=useState(''),[returned,setReturned]=useState(''),[pickError,setPickError]=useState('')
 const [layer,setLayer]=useState(0),previousTarget=useRef(triggerTarget)
 // A responsive pane may open its own modal drawer after this chooser.
 // Register the chooser above it again, keeping search and selection here.
 useLayoutEffect(()=>{if(previousTarget.current!==triggerTarget){previousTarget.current=triggerTarget;if(open)setLayer(value=>value+1)}},[triggerTarget,open])
 const opener=useRef<HTMLButtonElement>(null),handoff=useRef(false),mounted=useRef(true)
 useEffect(()=>{mounted.current=true;return()=>{mounted.current=false}},[])
 const background=useQuery({queryKey:['job-background-connections'],queryFn:()=>jobsAPI<{gatewayProfiles:string[]}>('background-connections'),enabled:open})
 const permitted=catalog.entries.filter(e=>!['blocked','unavailable'].includes(connectionState(e)??'')&&!e.status.isError)

 const descriptorFor=(p:ProfileEntry)=>catalog.entries.find(e=>e.descriptor.source?.id===p.profile.source && e.descriptor.source.shape===p.profile.shape || !e.descriptor.source && (e.descriptor.id==='google-drive'?'drive':e.descriptor.id)===p.profile.source)
 const canAdd=available&&!catalog.loading&&!catalog.isError&&permitted.some(e=>profiles.some(p=>supportsJobProfile(p)&&matchesJobGateway(p,config)&&descriptorFor(p)?.descriptor.id===e.descriptor.id)&&['connected','not-connected','setup-required'].includes(connectionState(e)??''))
 const title=(p:ProfileEntry)=>{const entry=descriptorFor(p),label=entry?`${providerName(entry.descriptor.id,entry.descriptor)} · ${p.profile.id}`:p.profile.id;return p.profile.calculator?`${label} · ${calculatorLabel(p.profile.calculator)}`:label}
 const matches=(value:string)=>value.toLocaleLowerCase().includes(search.trim().toLocaleLowerCase())
 function reason(p:ProfileEntry) {
  if(!supportsJobProfile(p))return msg('Jobs support is not available for this integration.')
  if(!matchesJobGateway(p,config))return msg('The source profile does not match this Desk gateway.')
  if(available&&(catalog.loading||catalog.isError))return catalog.loading?msg('Loading…'):msg('Connections could not be loaded.')
  const entry=descriptorFor(p)
  if(entry && connectionState(entry)!=='connected')return msg('Connect this integration before using it in a job.')
  if(p.profile.source==='drive'&&(!config.documents?.enabled||config.managedLocal!==true))return msg('Enable document processing in Admin → Document processing before attaching Drive files.')
  return undefined
 }
 function choose(p?:ProfileEntry) {
  if(disabled || p && reason(p))return
  try {onPick(p);setOpen(false);setReturned('');setPickError('')} catch {setPickError(msg('The inputs could not be mapped.'))}
 }
 function addIntegration() {
  if(!canAdd||disabled)return
  handoff.current=true;setOpen(false)
 }
 const selected=current?.profile?profiles.find(p=>p.profile.id===current.profile):undefined
 const label=current?(selected?title(selected):current.profile??msg('Local JSON file')):msg('Add source')
 const trigger=<div className={current?styles.integrationField:undefined}>
   {current&&<label id={`${id}-label`}>{msg('Integration')}</label>}
   <Button ref={opener} disabled={disabled} aria-labelledby={current?`${id}-label ${id}-name`:undefined} onClick={()=>{setSearch('');setOpen(true)}}>
    {current?.provider==='local-file'?<IconFolder/>:current?<IconGear/>:<IconPlus/>}<span id={`${id}-name`}>{label}</span>
   </Button>
  </div>
 return <>
  {triggerTarget===undefined?trigger:triggerTarget?createPortal(trigger,triggerTarget):null}
  <Dialog key={layer} open={open} onOpenChange={setOpen} title={msg('Choose integration')} description={msg('Reuse an integration, then configure its request and output mapping.')} openerRef={opener}
   onCloseAutoFocus={event=>{
    event.preventDefault()
    if(!mounted.current||open)return
    if(!handoff.current){opener.current?.focus();return}
    handoff.current=false
    pane.open({opener:opener.current,purpose:'job-source',onConnected:provider=>{
     if(!mounted.current)return
     pane.close?.({restoreFocus:false})
     void client.invalidateQueries({queryKey:['job-input-profiles']})
     void client.invalidateQueries({queryKey:['job-background-connections']})
     setReturned(provider);setSearch('');setOpen(true)
    }})
   }}
   footer={<DialogActions><Button onClick={()=>setOpen(false)}>{msg('Cancel')}</Button><Button disabled={disabled||!canAdd} onClick={addIntegration}>{msg('Add integration')}</Button></DialogActions>}>
   <div className={styles.fields}>
    {pickError&&<p role="alert" className={styles.problem}>{pickError}</p>}
    <Input value={search} onChange={e=>setSearch(e.target.value)} placeholder={msg('Search integrations…')} aria-label={msg('Search integrations…')}/>
    {returned&&<p role="status" className={styles.note}>{msg('Integration connected. Choose a supported source below; background access is checked separately.')}</p>}
    {available&&(catalog.loading||catalog.entries.some(e=>!e.status.data&&!e.status.isError))&&<p role="status" className={styles.note}>{msg('Loading…')}</p>}
    <div className={styles.integrationList}>
     {matches(msg('Local JSON file'))&&<Button variant="quiet" className={styles.integrationOption} disabled={disabled} onClick={()=>choose()}><IconFolder/><span><strong>{msg('Local JSON file')}</strong><small>{msg('This computer')}</small></span></Button>}
     {profiles.filter(p=>matches(title(p))).map(p=>{
      const entry=descriptorFor(p),problem=reason(p)
      const status=problem??(p.profile.source==='drive'?msg('Interactive file selection'):background.isPending?msg('Loading…'):background.isError?msg('Unavailable'):background.data?.gatewayProfiles?.includes(p.profile.id)?msg('Background access configured'):msg('Background access is not configured.'))
      return <div key={p.profile.id} className={styles.integrationItem}>
       <Button variant="quiet" className={styles.integrationOption} aria-label={title(p)} aria-describedby={`${id}-${p.profile.id}-status`} disabled={disabled||Boolean(problem)} onClick={()=>choose(p)}>
        {entry?<ProviderIcon provider={entry.descriptor.id} descriptor={entry.descriptor}/>:<IconGear/>}<span><strong>{title(p)}</strong><small>{p.profile.source} · {p.profile.class}</small></span>
       </Button><p id={`${id}-${p.profile.id}-status`} className={styles.note}>{status}</p>
      </div>
     })}
     <Disclosure title={msg('Other integrations')}>
     {catalog.entries.filter(e=>!profiles.some(p=>descriptorFor(p)?.descriptor.id===e.descriptor.id)&&matches(providerName(e.descriptor.id,e.descriptor))).map(e=><div className={styles.integrationItem} key={e.descriptor.id}><div className={styles.integrationOption}><ProviderIcon provider={e.descriptor.id} descriptor={e.descriptor}/><span><strong>{providerName(e.descriptor.id,e.descriptor)}</strong><small>{connectionState(e)===undefined?msg('Loading…'):connectionState(e)==='blocked'?msg('Managed by your organization'):connectionState(e)==='unavailable'||e.status.isError?msg('Unavailable'):connectionState(e)==='connected'?msg('Connected'):connectionState(e)==='setup-required'?msg('Setup required'):msg('Not connected')}</small></span></div><p className={styles.note}>{msg('Jobs support is not available for this integration.')}</p></div>)}
     </Disclosure>
    </div>
    {catalog.isError&&<p role="alert" className={styles.problem}>{msg('Connections could not be loaded.')} <Button variant="inline" onClick={()=>void catalog.refetch()}>{msg('Retry')}</Button></p>}
    {!available&&<p className={styles.note}>{msg('Connect integrations through the configured Gateway. Local setup is unavailable.')}</p>}
    {!canAdd&&available&&!catalog.loading&&<p className={styles.note}>{msg('Additional integrations need a compatible Jobs source profile from the installation owner.')}</p>}
    <p className={styles.note}>{msg('Credentials stay with the integration. Changing it requires a new input preview.')}</p>
   </div>
  </Dialog>
 </>
}
