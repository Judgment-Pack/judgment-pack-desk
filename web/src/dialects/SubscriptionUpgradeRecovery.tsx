import {useState} from 'react'
import {SubscriptionUpgradeSummary} from './SubscriptionUpgradeSummary'
import {useQuery,useQueryClient} from '@tanstack/react-query'
import {msg} from '../i18n'
import {Button} from '../ui/Button'
import {Dialog,DialogActions} from '../ui/Dialog'
import {LoadingState} from '../ui/LoadingState'
import {Notice} from '../ui/Notice'
import {dialectAPI,type DialectEvaluation} from './client'
import styles from './DialectsView.module.css'

export function SubscriptionUpgradeRecovery({preparationId,id,opener,onClose,onSaved}:{preparationId:string;id:string;opener:React.RefObject<HTMLButtonElement|null>;onClose:()=>void;onSaved:()=>void}){
 const queryClient=useQueryClient()
 const [busy,setBusy]=useState(false),[checked,setChecked]=useState(false),[error,setError]=useState('')
 const path='/'+preparationId+'/evaluations/'+id
 const query=useQuery({queryKey:['subscription-upgrade',preparationId,id],queryFn:()=>dialectAPI<DialectEvaluation>(path),retry:false,refetchOnWindowFocus:false})
 const b=query.data,submitted=b?.upgrade?.phase==='submitted',done=!!b?.confirmedAt,refused=b?.upgrade?.phase==='refused'
 async function act(action:'apply'|'recover'|'cancel'|'retry'){
  if(!b)return;setBusy(true);setError('')
  try{const result=await dialectAPI<DialectEvaluation>(path+'/subscription-upgrade','POST',{reviewed:true,action},b.revision);queryClient.setQueryData(['subscription-upgrade',preparationId,id],result);onSaved();if(result.upgrade?.phase!=='refused')onClose();else setChecked(false)}
  catch(e){setError((e as Error).message);await query.refetch()}
  finally{setBusy(false)}
 }
 return <Dialog open title={msg('Review subscription upgrade')} openerRef={opener} onOpenChange={open=>{if(!open&&!busy)onClose()}} footer={<DialogActions>
  <Button disabled={busy} onClick={onClose}>{msg('Close')}</Button>
  {b&&!done&&<>
   {refused?<Button disabled={busy} onClick={()=>void act('recover')}>{msg('Check result')}</Button>:<>
   {submitted?<><Button disabled={busy} onClick={()=>void act('recover')}>{msg('Check result')}</Button><Button variant="primary" disabled={busy||!checked} onClick={()=>void act('retry')}>{msg('Retry exact upgrade')}</Button></>:<><Button disabled={busy} onClick={()=>void act('cancel')}>{msg('Cancel upgrade')}</Button><Button variant="primary" disabled={busy||!checked} onClick={()=>void act('apply')}>{msg('Apply upgrade')}</Button></>}
  </>}</>}
 </DialogActions>}><div className={styles.stack}>
  {query.isPending?<LoadingState>{msg('Loading review…')}</LoadingState>:query.error?<Notice tone="error" actions={<Button onClick={()=>void query.refetch()}>{msg('Retry')}</Button>}>{query.error.message}</Notice>:b&&<>
   <p>{b.name}</p><SubscriptionUpgradeSummary evaluation={b}/>
   {done?<Notice>{msg('Upgrade saved. Review and enable the new subscription when ready.')}</Notice>:refused?<Notice tone="warning">{b.upgrade?.problem}</Notice>:<>
    {submitted&&<p>{msg('The result is uncertain. Check result only reads the saved receipt. Retry submits the same reviewed upgrade once; it does not start record runs.')}</p>}
    <label className="checkbox"><input type="checkbox" checked={checked} disabled={busy} onChange={e=>setChecked(e.target.checked)}/>{msg('I reviewed this fixed release and paused subscription upgrade.')}</label>
   </>}
  </>}
  {error&&<Notice tone="error">{error}</Notice>}
 </div></Dialog>
}
