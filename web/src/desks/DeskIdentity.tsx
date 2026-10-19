import { useRef, useState, type ReactNode, type Ref } from 'react'
import { msg, useLocale } from '../i18n'
import { BrandMark } from '../ui/BrandMark'
import { Input } from '../ui/Input'
import { Menu } from '../ui/Menu'
import { Tooltip } from '../ui/Tooltip'
import { IconChevronDown } from '../shell/icons'

export type DeskIdentityChoice = { id: string; name: string; mark?: string | null }

/** Shared identity/choice presentation; each host owns access and navigation. */
export function DeskIdentity({ name, mark, value, desks, startup, onSelect, disabled, home, dirty, dirtyLabels = [], opener, children }: {
 name: string; mark?: string | null; value: string; desks: readonly DeskIdentityChoice[]; startup?: string
 onSelect: (id: string) => void; disabled?: boolean; home?: (mark: ReactNode) => ReactNode
 dirty?: boolean; dirtyLabels?: readonly string[]; opener?: Ref<HTMLButtonElement>; children?: ReactNode
}) {
 useLocale()
 const [open, setOpen] = useState(false), [search, setSearch] = useState('')
 const input = useRef<HTMLInputElement>(null), list = useRef<HTMLDivElement>(null)
 const rows = [...desks].sort((a,b) => Number(b.id === value)-Number(a.id === value) || a.name.localeCompare(b.name))
 const filtered = rows.filter(row => row.name.toLocaleLowerCase().includes(search.toLocaleLowerCase()))
 const logo = <BrandMark mark={mark} className="desk-orgmark" />
 return <div className="desk-identity">
  {home ? home(logo) : <span className="desk-home">{logo}</span>}
  <Menu.Root open={open} onOpenChange={next => {setOpen(next); if (!next) setSearch('')}}>
   <Tooltip content={[name,msg('Switch desk'),...(dirty?[msg('Unsaved changes'),...dirtyLabels]:[])].join(' · ')} side="bottom" disabled={open}>
    <Menu.Trigger ref={opener} disabled={disabled} className="desk-chip" aria-label={`${name} · ${msg('Switch desk')}`}>
     <span className="desk-chip-name">{name}</span>{dirty&&<span className="desk-dirty" aria-label={msg('unsaved changes')} role="img"/>}<IconChevronDown/>
    </Menu.Trigger>
   </Tooltip>
   <Menu.Portal><Menu.Content className="desk-switcher-menu" align="start" alignOffset={-32} sideOffset={6} collisionPadding={12}>
    <Menu.Label>{msg('Switch desk')}</Menu.Label>
    <div className="desk-switcher-search"><Input ref={input} autoFocus aria-label={msg('Search desks')} placeholder={msg('Search desks')} value={search} onChange={event=>setSearch(event.target.value)} onKeyDown={event=>{
     if (event.key !== 'Escape') event.stopPropagation()
     if (event.key === 'ArrowDown') {event.preventDefault(); list.current?.querySelector<HTMLElement>('[role="menuitemradio"]')?.focus()}
    }}/></div>
    <Menu.RadioGroup ref={list} value={value} onValueChange={onSelect} className="desk-switcher-list">
     {filtered.map(row=><Menu.RadioItem key={row.id} value={row.id} description={row.id === startup ? msg('Startup') : undefined}>{row.name}</Menu.RadioItem>)}
    </Menu.RadioGroup>
    {!filtered.length&&<Menu.Label>{msg('No desks found.')}</Menu.Label>}
    {children}
   </Menu.Content></Menu.Portal>
  </Menu.Root>
 </div>
}
