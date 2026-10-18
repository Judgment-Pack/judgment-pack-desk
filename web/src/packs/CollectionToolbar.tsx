import {Menu as DropdownMenu} from '../ui/Menu'
import {msg} from '../i18n'
import {Button} from '../ui/Button'
import {CollectionToolbar as CollectionControls} from '../ui/CollectionToolbar'
import {Tooltip} from '../ui/Tooltip'
import {IconChevronDown} from '../shell/icons'
import {SelectionActions,type BrowserItem} from './CollectionActions'
import styles from './PacksPane.module.css'

interface Props {
  filter: string; onFilter: (value: string) => void
  kind: string; onKind: (value: string) => void
  status: string; onStatus: (value: string) => void
  sort: string; onSort: (value: string) => void
  canScope: boolean; everywhere: boolean; onScope: (value: boolean) => void
  filtered: boolean; reset: () => void
  selected: BrowserItem[]; clearSelection: () => void
  onDelete: (items: BrowserItem[], opener?: HTMLElement) => void
}

export function CollectionToolbar(props:Props){
 const {filter,onFilter,kind,onKind,status,onStatus,sort,onSort,canScope,everywhere,onScope,filtered,reset,selected,clearSelection,onDelete}=props
 const scope=everywhere?msg('All folders'):msg('This folder')
 return <CollectionControls padded label={msg('Library list controls')} searchLabel={msg('Search library')} search={filter} onSearch={onFilter}
  filters={[{label:msg('Item type'),value:kind,onValueChange:onKind,options:[{value:'all',label:msg('All types')},{value:'pack',label:msg('Packs')},{value:'graph',label:msg('Graphs')},{value:'dialect',label:msg('Dialects')}]},{label:msg('Status'),value:status,onValueChange:onStatus,options:[{value:'all',label:msg('All statuses')},{value:'draft',label:msg('Draft')},{value:'saved',label:msg('Saved')}]}]}
  sort={{label:msg('Sort library'),value:sort,onValueChange:onSort,options:[{value:'id-asc',label:msg('Name: A–Z')},{value:'id-desc',label:msg('Name: Z–A')}]}}
  filtered={filtered} onReset={reset}
  searchSuffix={({tight})=>canScope&&<DropdownMenu.Root>
   <Tooltip content={msg('Search scope: {{scope}}',{scope})}><DropdownMenu.Trigger asChild><Button variant="quiet" className={styles.scope} aria-label={msg('Search scope: {{scope}}',{scope})}><span hidden={tight}>{scope}</span><IconChevronDown/></Button></DropdownMenu.Trigger></Tooltip>
   <DropdownMenu.Portal><DropdownMenu.Content align="start" collisionPadding={16}><DropdownMenu.RadioGroup value={everywhere?'all':'folder'} onValueChange={value=>onScope(value==='all')}>{[{value:'folder',label:msg('This folder')},{value:'all',label:msg('All folders')}].map(option=><DropdownMenu.RadioItem key={option.value} value={option.value}>{option.label}</DropdownMenu.RadioItem>)}</DropdownMenu.RadioGroup></DropdownMenu.Content></DropdownMenu.Portal>
  </DropdownMenu.Root>}
  actions={({compact,tight})=><SelectionActions items={selected} clear={clearSelection} onDelete={onDelete} compact={compact} tight={tight}/>}/>
}
