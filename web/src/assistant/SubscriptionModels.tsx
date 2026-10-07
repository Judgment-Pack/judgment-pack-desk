import { useState } from 'react'
import { msg } from '../i18n'
import type { ProviderModel } from './providers'
import { Field } from '../ui/Field'
import { Button } from '../ui/Button'
import { Popover } from '../ui/Popover'
import { Select } from '../ui/Select'
import { IconChevronDown } from '../shell/icons'
import { ModelMenuList } from './ModelMenuList'
import styles from './ModelMenuList.module.css'

export function SubscriptionModels({ models, allowed, model, disabled, onChange, label = msg('Allowed models') }: {
  label?:string
  models: readonly ProviderModel[]; allowed: readonly string[]; model: string; disabled: boolean
  onChange: (allowed: string[], model: string) => void
}) {
  const [open,setOpen]=useState(false), [search,setSearch]=useState('')
  const choices=[...models.map(row=>({value:row.id,label:row.id})),...allowed.filter(id=>!models.some(row=>row.id===id)).map(id=>({value:id,label:id}))]
  const select=(next:string[])=>onChange(next,next.includes(model)?model:next[0]??'')
  return <>
    <Field label={label}>{wiring=><Popover title={label} variant="list" size="small" align="start" open={open} onOpenChange={value=>{setOpen(value);if(!value)setSearch('')}}
      trigger={<Button {...wiring} className={styles.trigger} disabled={disabled}>{msg('Models')} · {allowed.length}<IconChevronDown/></Button>}>
      <ModelMenuList options={choices} search={search} onSearch={setSearch} selected={allowed} disabled={disabled} onToggle={(id,checked)=>select(checked?[...allowed,id]:allowed.filter(value=>value!==id))}/>
      <div className={styles.bulk}>
        <Button variant="quiet" disabled={disabled||models.length===0||models.every(row=>allowed.includes(row.id))} onClick={()=>select(models.map(row=>row.id))}>{msg('Select all')}</Button>
        <Button variant="quiet" disabled={disabled||allowed.length===0} onClick={()=>select([])}>{msg('Remove all')}</Button>
      </div>
    </Popover>}</Field>
    <Field label={msg('Default model')}>{wiring=><Select {...wiring} value={model||undefined} placeholder={msg('Choose a model')} disabled={disabled||allowed.length===0}
      options={choices.filter(row=>allowed.includes(row.value))} onValueChange={value=>onChange([...allowed],value)}/>}</Field>
  </>
}
