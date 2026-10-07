import { Fragment } from 'react'
import { RadioGroup } from 'radix-ui'
import { msg } from '../i18n'
import { IconCheck } from '../shell/icons'
import { Input } from '../ui/Input'
import styles from './ModelMenuList.module.css'

export interface ModelMenuOption { value: string; label: string; group?:string }
/** Flat, searchable choices shared by Admin grants and the composer. */
export function ModelMenuList({options,search,onSearch,value,onValueChange,selected,onToggle,disabled=false,pending=false}:{
  options: readonly ModelMenuOption[]; search: string; onSearch: (value:string)=>void
  value?: string; onValueChange?: (value:string)=>void
  selected?: readonly string[]; onToggle?: (value:string,checked:boolean)=>void; disabled?:boolean; pending?:boolean
}) {
  const rows=options.filter(row=>`${row.group??''} ${row.label} ${row.value}`.toLowerCase().includes(search.trim().toLowerCase()))
  return <>
    <div className={styles.search}><Input type="search" aria-label={msg('Search models')} placeholder={msg('Search models')} value={search} onChange={event=>onSearch(event.target.value)} onKeyDown={event=>{
      if(event.key==='ArrowDown'){event.preventDefault();event.currentTarget.parentElement?.nextElementSibling?.querySelector<HTMLElement>('[data-model-option]')?.focus()}
    }}/></div>
    {selected ? <div className={styles.list} role="group" aria-label={msg('Allowed models')} onKeyDown={event=>{
      if(!['ArrowDown','ArrowUp','Home','End'].includes(event.key))return
      const items=[...event.currentTarget.querySelectorAll<HTMLInputElement>('[data-model-option]:not(:disabled)')], index=items.indexOf(event.target as HTMLInputElement)
      if(index<0||!items.length)return
      event.preventDefault();items[event.key==='Home'?0:event.key==='End'?items.length-1:(index+(event.key==='ArrowDown'?1:-1)+items.length)%items.length]?.focus()
    }}>
      {rows.map(row=><label key={row.value} className={styles.row} data-selected={selected.includes(row.value)||undefined}>
        <input data-model-option type="checkbox" disabled={disabled} checked={selected.includes(row.value)} onChange={event=>onToggle?.(row.value,event.target.checked)}/><span>{row.label}</span>
      </label>)}
    </div> : <RadioGroup.Root className={styles.list} aria-label={msg('Model')} orientation="vertical" value={value} onValueChange={onValueChange} disabled={disabled}>
      {rows.map((row,index)=><Fragment key={row.value}>{row.group&&row.group!==rows[index-1]?.group&&<div className={styles.group}>{row.group}</div>}<RadioGroup.Item value={row.value} className={styles.row} data-model-option data-selected={value===row.value||undefined}>
        <span>{row.label}</span><span className={styles.check} aria-hidden="true">{value===row.value&&<IconCheck/>}</span>
      </RadioGroup.Item></Fragment>)}
    </RadioGroup.Root>}
    {rows.length===0&&!pending&&<p className={styles.empty}>{msg('No models match your search.')}</p>}
  </>
}
