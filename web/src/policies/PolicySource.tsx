import {useState} from 'react'
import {msg,systemMessage} from '../i18n'
import {Button} from '../ui/Button'
import {SettingsSection} from '../ui/SettingsSection'
import {LoadingState} from '../ui/LoadingState'
import {PolicyRequestError,readPolicySource,type PolicySource as Source} from './client'
import styles from './PolicyPage.module.css'

export function PolicySource({id,source}:{id:string;source:Source}){
 const [busy,setBusy]=useState(false),[text,setText]=useState<string>(),[error,setError]=useState<PolicyRequestError>()
 async function read(download=false){
  if(busy)return
  setBusy(true);setError(undefined)
  try{
   const bytes=await readPolicySource(id)
   if(download){const url=URL.createObjectURL(new Blob([bytes as BlobPart],{type:source.mediaType}));const link=document.createElement('a');link.href=url;link.download=source.name;link.click();URL.revokeObjectURL(url)}
   else setText(new TextDecoder('utf-8',{fatal:true,ignoreBOM:true}).decode(bytes))
  }catch(cause){setError(cause instanceof PolicyRequestError?cause:new PolicyRequestError('unavailable'))}finally{setBusy(false)}
 }
 return <SettingsSection level={2} variant="plain" title={msg('Original source')} description={msg('User-supplied starting material. The original is retained separately from your editable draft; importing does not approve or publish it.')}>
  <dl className={styles.facts}><dt>{msg('File')}</dt><dd>{source.name}</dd><dt>{msg('Media type')}</dt><dd>{source.mediaType}</dd><dt>{msg('Bytes')}</dt><dd>{source.content.bytes}</dd><dt>{msg('Source digest')}</dt><dd className={styles.digest}>{source.content.sha256}</dd></dl>
  <div className={styles.actions}><Button disabled={busy} onClick={()=>void read()}>{msg('Read original')}</Button><Button disabled={busy} onClick={()=>void read(true)}>{msg('Download original')}</Button>{busy&&<LoadingState inline>{msg('Loading original…')}</LoadingState>}</div>
  {error&&<p role="alert">{systemMessage(error.message)}</p>}{text!==undefined&&<pre className={styles.snapshot}>{text}</pre>}
 </SettingsSection>
}
