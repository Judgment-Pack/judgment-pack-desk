import {useLayoutEffect,useRef,useState,type ReactNode,type KeyboardEventHandler} from 'react'
import {msg} from '../i18n'
import {IconChevronDown,IconClose} from '../shell/icons'
import {Button} from './Button'
import {Menu,MenuChoice} from './Menu'
import {SearchInput} from './SearchInput'
import {SortMenu} from './SortMenu'
import {Tooltip} from './Tooltip'
import styles from './CollectionToolbar.module.css'

type Layout={compact:boolean;tight:boolean}
type Choice={label:string;value:string;onValueChange:(value:string)=>void;options:readonly {value:string;label:string;disabled?:boolean}[]}
type Toggle={label:string;checked:boolean;onCheckedChange:(checked:boolean)=>void}

/** One collection control row. Filters collapse before search or selection is squeezed out. */
export function CollectionToolbar({label,searchLabel,search,onSearch,searchSuffix,onSearchKeyDown,filters=[],sort,toggles=[],filtered=false,onReset,actions,padded=false}:{
 label:string;searchLabel:string;search:string;onSearch:(value:string)=>void
 onSearchKeyDown?:KeyboardEventHandler<HTMLInputElement>;searchSuffix?:(layout:Layout)=>ReactNode;filters?:Choice[];sort?:Choice;toggles?:Toggle[]
 filtered?:boolean;onReset?:()=>void;actions?:(layout:Layout)=>ReactNode;padded?:boolean
}){
 const root=useRef<HTMLDivElement>(null),[layout,setLayout]=useState<Layout>({compact:false,tight:false})
 useLayoutEffect(()=>{
  const element=root.current;if(!element)return
  const measure=()=>{const style=getComputedStyle(element),width=element.getBoundingClientRect().width-parseFloat(style.paddingLeft||'0')-parseFloat(style.paddingRight||'0');if(width>0)setLayout({compact:width<800,tight:width<480})}
  measure();const observer=new ResizeObserver(measure);observer.observe(element);return()=>observer.disconnect()
 },[])
 const active=filters.filter(filter=>filter.value!==filter.options[0]?.value).length+toggles.filter(toggle=>toggle.checked).length
 const groups=sort?[...filters,{...sort,label:msg('Sort')}]:filters
 const hasOptions=groups.length>0||toggles.length>0
 return <div ref={root} className={styles.root} data-padded={padded||undefined} data-compact={layout.compact||undefined} data-tight={layout.tight||undefined} role="group" aria-label={label} data-collection-toolbar>
  <SearchInput className={styles.search} aria-label={searchLabel} placeholder={msg('Search…')} value={search} onKeyDown={onSearchKeyDown} onChange={event=>onSearch(event.target.value)} suffix={searchSuffix?.(layout)}/>
  {layout.compact&&hasOptions?<Menu.Root>
   <Menu.Trigger asChild><Button variant="quiet" aria-label={msg('View options')}>{msg('View')}{active>0&&<span className={styles.count}>{active}</span>}<IconChevronDown/></Button></Menu.Trigger>
   <Menu.Portal><Menu.Content align="end" collisionPadding={16}>
    {groups.map(group=><Menu.Group key={group.label}><Menu.Label>{group.label}</Menu.Label><Menu.RadioGroup value={group.value} onValueChange={group.onValueChange} aria-label={group.label}>{group.options.map(option=><Menu.RadioItem key={option.value} value={option.value} disabled={option.disabled} onSelect={event=>event.preventDefault()}>{option.label}</Menu.RadioItem>)}</Menu.RadioGroup></Menu.Group>)}
    {toggles.map(toggle=><Menu.CheckboxItem key={toggle.label} checked={toggle.checked} onCheckedChange={value=>toggle.onCheckedChange(value===true)} onSelect={event=>event.preventDefault()}>{toggle.label}</Menu.CheckboxItem>)}
    {filtered&&onReset&&<><Menu.Separator/><Menu.Item icon={<IconClose/>} onSelect={onReset}>{msg('Clear filters')}</Menu.Item></>}
   </Menu.Content></Menu.Portal>
  </Menu.Root>:<>
   {filters.map(filter=><MenuChoice key={filter.label} {...filter}/>)}
   {sort&&<SortMenu {...sort}/>}
   {toggles.map(toggle=><label key={toggle.label} className={styles.toggle}><input type="checkbox" checked={toggle.checked} onChange={event=>toggle.onCheckedChange(event.target.checked)}/>{toggle.label}</label>)}
  </>}
  {filtered&&onReset&&(!layout.compact||!hasOptions)&&<Tooltip content={msg('Clear filters')}><Button variant="quiet" size="icon" aria-label={msg('Clear filters')} onClick={onReset}><IconClose/></Button></Tooltip>}
  {actions?.(layout)}
 </div>
}
