import {useEffect,useState} from 'react'
import {msg} from '../i18n'
import {usePack,usePacks} from '../mcp/queries'
import {Field} from '../ui/Field'
import {Select} from '../ui/Select'
import {Button} from '../ui/Button'
import {Notice} from '../ui/Notice'
import {Disclosure} from '../ui/Disclosure'
import {DialectEditor} from './DialectEditor'
import {useConfirmDiscard} from '../shell/UnsavedChanges'
import type {DialectPackBinding,DialectRecord} from './client'
import {boundPack,packBinding} from './qualification'
import styles from './DialectsView.module.css'
import type {ResearchRecipe} from './research'

export function DialectPackEditor({value,records,research,disabled,onChange,onPendingChange}:{value?:DialectPackBinding;records:DialectRecord[];research?:ResearchRecipe;disabled:boolean;onPendingChange:(pending:boolean)=>void;onChange:(value:DialectPackBinding|undefined)=>void}) {
 const [choice,setChoice]=useState(value?.key??''),[busy,setBusy]=useState(false),[error,setError]=useState(''),confirm=useConfirmDiscard()
 const packs=usePacks(),pack=usePack(choice||undefined)
 useEffect(()=>{setChoice(value?.key??'')},[value?.key])
 useEffect(()=>{onPendingChange(busy);return()=>onPendingChange(false)},[busy,onPendingChange])
 const usePackSnapshot=async()=>{
  if(!pack.data||busy)return
  if(value&&!await confirm(msg('Changing the pack discards its dialect.')))return
  setBusy(true);setError('')
  try{const fresh=await pack.refetch();if(fresh.error)throw fresh.error;if(!fresh.data)throw Error(msg('The pack could not be loaded.'));onChange(await packBinding(choice,fresh.data.raw))}catch(e){setError((e as Error).message)}finally{setBusy(false)}
 }
 const doc=value?boundPack(value):undefined
 const options=packs.data?.packs?.map(p=>({value:p.id,label:p.id}))??[]
 if(value&&!options.some(o=>o.value===value.key))options.push({value:value.key,label:doc?.title??value.key})
 return <section className={styles.stack}>
  <strong>{msg('Dialect')}</strong><p className={styles.note}>{msg('Choose a pack snapshot and map record fields. This does not change its rules or evaluate records.')}</p><div className={styles.qualificationChoice}><Field label={msg('Pack')}>{props=><Select {...props} value={choice} placeholder={msg('Choose a saved pack')} options={options} disabled={disabled||busy} onValueChange={setChoice}/>}</Field>
  <Button disabled={disabled||busy||!pack.data||choice===value?.key&&pack.data.raw===value.pack} onClick={()=>void usePackSnapshot()}>{msg('Use this pack')}</Button></div>
  {(error||packs.error||pack.error)&&<Notice tone="error">{error||(packs.error??pack.error)?.message}</Notice>}
  {value&&doc&&<><p className={styles.note}>{msg('Saved pack snapshot')}: {doc.title??value.key} · {doc.version}</p><DialectEditor value={value} records={records} research={research} disabled={disabled||busy} onChange={onChange}/><Disclosure title={msg('Technical details')}><p className={styles.note}>{value.digest}</p></Disclosure></>}
  {value&&<div className={styles.removeSection}><Button variant="quiet" disabled={disabled||busy} onClick={async()=>{if(await confirm(msg('Changing the pack discards its dialect.')))onChange(undefined)}}>{msg('Remove pack bindings')}</Button></div>}
 </section>
}
