import {Link} from 'react-router-dom'
import {msg} from '../../i18n'
import {Button} from '../../ui/Button'
import {CollectionLayout,CollectionEmpty} from '../../ui/CollectionLayout'
import {CollectionToolbar} from '../../ui/CollectionToolbar'
import {CollectionRow} from '../../ui/CollectionRow'
import {SelectionTable,SelectionHeader,SelectionCell,SelectionActions} from '../../ui/SelectionTable'
import {useCollectionSelection} from '../../ui/useCollectionSelection'
import {usePagination} from '../../ui/usePagination'
import {Pagination} from '../../ui/Pagination'
import {Menu} from '../../ui/Menu'
import {Tooltip,OverflowTooltip} from '../../ui/Tooltip'
import {IconMatrix,IconCopy,IconTrash,IconPlay,IconAssistant,IconInfo} from '../../shell/icons'
import {executable,expectedLabel,latestResult,type TestCase,type TestRun,type TestSuite} from './model'
import type {ReactNode} from 'react'
import styles from './TestsWorkspace.module.css'

export function TestsCollection({suite,digest,query,onQuery,onReset,filter,onFilter,sort,onSort,busy,canRun,header,caseHref,onRun,onResult,onDuplicate,onDelete,onExport,coverage,onCoverage,onDesign,loading,unavailable=false}: {
 suite:TestSuite;digest:string;query:string;onQuery:(value:string)=>void;filter:string;onFilter:(value:string)=>void;sort:string;onSort:(value:string)=>void
 busy:boolean;canRun:boolean;header:ReactNode;caseHref:(id:string)=>string;onRun:(cases:TestCase[])=>void;onResult:(run:TestRun)=>void;onDuplicate:(test:TestCase)=>void;onDelete:(test:TestCase,opener:HTMLElement)=>void;onExport:(cases:TestCase[])=>void
 onReset:()=>void;coverage:number;onCoverage:()=>void;onDesign:()=>void;loading:boolean;unavailable?:boolean
}) {
 const filtered=suite.cases.filter(c=>(c.id+' '+c.name+' '+c.rationale).toLocaleLowerCase().includes(query.trim().toLocaleLowerCase())&&(filter==='all'||latestResult(suite,c,digest).status===filter))
 if(sort!=='saved')filtered.sort((a,b)=>sort==='name-desc'?b.name.localeCompare(a.name):a.name.localeCompare(b.name))
 const scope=JSON.stringify([query,filter,sort]),selection=useCollectionSelection(filtered,c=>c.id),pagination=usePagination(filtered.length,scope)
 const selected=selection.selectedRows.filter(executable)
 return <CollectionLayout label={msg('Test cases')} loading={loading&&!suite.cases.length?msg('Loading tests…'):undefined} scrollKey={scope+pagination.page}
  toolbar={<>{header}<CollectionToolbar label={msg('Test controls')} searchLabel={msg('Search cases')} search={query} onSearch={onQuery}
   filters={[{label:msg('Test result filter'),value:filter,onValueChange:onFilter,options:[
    {value:'all',label:msg('All results')},{value:'passed',label:msg('Passed')},{value:'mismatch',label:msg('Mismatch')},{value:'exploratory',label:msg('Exploratory')},{value:'not-run',label:msg('Not run')},{value:'stale',label:msg('Needs rerun')},{value:'error',label:msg('Could not run')}
   ]}]}
   sort={{label:msg('Sort test cases'),value:sort,onValueChange:onSort,options:[{value:'saved',label:msg('Saved order')},{value:'name-asc',label:msg('Name: A–Z')},{value:'name-desc',label:msg('Name: Z–A')}]}}
   filtered={!!query||filter!=='all'||sort!=='saved'} onReset={onReset}
   actions={({compact,tight})=><SelectionActions selection={selection} compact={compact} tight={tight} onExport={()=>onExport(selection.selectedRows)}>
    <Menu.Item icon={<IconPlay/>} disabled={busy||!canRun||!selected.length} onSelect={()=>onRun(selected)}>{msg('Run selected cases')}</Menu.Item>
   </SelectionActions>}/></>}
  footer={!loading&&!unavailable&&<><Pagination pagination={pagination}/>{coverage>0&&<div className={styles.coverageFooter}>
   <Button variant="quiet" onClick={onCoverage}><IconInfo/>{msg('{{count}} coverage gaps',{count:coverage})}</Button>
   <Button variant="quiet" disabled={busy} onClick={onDesign}><IconAssistant/>{msg('Design missing tests')}</Button>
  </div>}</>}
 >
  {unavailable?<CollectionEmpty title={msg('Tests unavailable')}>{null}</CollectionEmpty>:!suite.cases.length?<CollectionEmpty title={msg('No saved cases yet')}><p>{msg('Create a case or ask Assistant to design one. Add an expected result to make it repeatable.')}</p></CollectionEmpty>:!filtered.length?<CollectionEmpty title={msg('No cases match these filters.')}><Button variant="quiet" onClick={onReset}>{msg('Clear filters')}</Button></CollectionEmpty>:
  <SelectionTable variant="index" selection={selection} className={styles.caseIndex} aria-label={msg('Test cases')}>
   <colgroup><col data-selection-column/><col/><col className={styles.expectedColumn}/><col className={styles.resultColumn}/><col className={styles.originColumn}/><col className={styles.actionsColumn}/></colgroup>
   <thead><tr><SelectionHeader selection={selection}/><th scope="col">{msg('Case')}</th><th scope="col">{msg('Expected')}</th><th scope="col">{msg('Last result')}</th><th scope="col">{msg('Origin')}</th><th scope="col" aria-label={msg('Actions')}/></tr></thead>
   <tbody>{filtered.slice(pagination.start,pagination.end).map(c=>{
    const result=latestResult(suite,c,digest)
    return <CollectionRow key={c.id} data-selection-selected={selection.has(c.id)||undefined}>
     <SelectionCell selection={selection} id={c.id} name={c.name}/>
     <td><OverflowTooltip selector="[data-overflow-text]" content={c.name}><Link to={caseHref(c.id)} data-row-primary data-collection-name><IconMatrix/><span data-overflow-text>{c.name}</span></Link></OverflowTooltip></td>
     <td><OverflowTooltip content={expectedLabel(c.row)}><span data-overflow-text>{expectedLabel(c.row)}</span></OverflowTooltip></td>
     <td><Button variant="inline" disabled={!result.run||busy} onClick={()=>result.run&&onResult(result.run)}><span className={styles.status} data-status={result.status}>{msg(result.label)}</span></Button></td>
     <td>{c.inputSnapshot?msg('Dialects'):c.origin==='draft'?msg('Draft'):c.origin==='ai'?msg('AI'):c.origin==='manual'?msg('Manual'):msg('Imported')}</td>
     <td><div className={styles.rowActions}>
      <Tooltip content={msg('Duplicate case')}><Button variant="quiet" size="icon" aria-label={msg('Duplicate case')} disabled={busy} onClick={()=>onDuplicate(c)}><IconCopy/></Button></Tooltip>
      <Tooltip content={msg('Delete case')}><Button variant="quiet" size="icon" aria-label={msg('Delete case')} disabled={busy} onClick={event=>onDelete(c,event.currentTarget)}><IconTrash/></Button></Tooltip>
     </div></td>
    </CollectionRow>
   })}</tbody>
  </SelectionTable>}
 </CollectionLayout>
}
