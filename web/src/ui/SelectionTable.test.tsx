import { createRef, useImperativeHandle } from 'react'
import { act, cleanup, fireEvent, render, screen, within } from '@testing-library/react'
import { Tooltip } from 'radix-ui'
import { afterEach, expect, it, vi } from 'vitest'
import { useCollectionSelection } from './useCollectionSelection'
import { SelectionTable, SelectionHeader, SelectionCell, SelectionActions } from './SelectionTable'
import { CollectionRow } from './CollectionRow'
import { exportSelectedRows } from './selectionExport'

afterEach(()=>{cleanup();vi.restoreAllMocks()})
const rows=[{id:'a',name:'Alpha'},{id:'b',name:'Beta'},{id:'c',name:'Gamma'}]
type API=ReturnType<typeof useCollectionSelection<(typeof rows)[number]>>
function Fixture({items=rows,page=0,scope='one',complete=true,handle,open=()=>{}}:{items?:typeof rows;page?:number;scope?:string;complete?:boolean;handle?:React.Ref<API>;open?:()=>void}){
 const selection=useCollectionSelection(items,row=>row.id,scope)
 useImperativeHandle(handle,()=>selection)
 return <Tooltip.Provider><input aria-label="Search"/><SelectionActions selection={selection} onExport={()=>exportSelectedRows('fixture',selection.selectedRows)}/>
 <SelectionTable selection={selection}><thead><tr><SelectionHeader selection={selection} complete={complete}/><th>Name</th></tr></thead><tbody>
 {items.slice(page,page+2).map(row=><CollectionRow key={row.id}><SelectionCell selection={selection} id={row.id} name={row.name}/><td><button data-row-primary onClick={open}>{row.name}</button></td></CollectionRow>)}
 </tbody></SelectionTable></Tooltip.Provider>
}
const box=(name:string)=>screen.getByRole('checkbox',{name}) as HTMLInputElement
it('selects matching rows across pages, shows mixed state, and toggles all off',()=>{
 const handle=createRef<API>(),ui=render(<Fixture handle={handle}/>)
 fireEvent.click(box('Select Alpha'));expect(box('Select all matches').indeterminate).toBe(true)
 fireEvent.click(box('Select all matches'));expect(handle.current!.selectedRows).toEqual(rows)
 ui.rerender(<Fixture page={2} handle={handle}/>);expect(box('Select Gamma').checked).toBe(true)
 fireEvent.click(box('Clear selection'));expect(handle.current!.count).toBe(0)
 expect(screen.queryByRole('group',{name:'Selected item actions'})).toBeNull()
})
it('prunes filtered or removed rows, retains only matching choices and resets for another owner',()=>{
 const handle=createRef<API>(),ui=render(<Fixture handle={handle}/>)
 fireEvent.click(box('Select all matches'))
 ui.rerender(<Fixture handle={handle} items={rows.slice(1)}/>);expect(handle.current!.selectedRows).toEqual(rows.slice(1))
 ui.rerender(<Fixture handle={handle}/>);expect(handle.current!.has('a')).toBe(false);expect(handle.current!.mixed).toBe(true)
 ui.rerender(<Fixture handle={handle} scope="two"/>);expect(handle.current!.count).toBe(0)
})
it('labels incomplete inventories and does not silently select subsequently loaded rows',()=>{
 const handle=createRef<API>(),ui=render(<Fixture handle={handle} items={rows.slice(0,1)} complete={false}/>)
 fireEvent.click(box('Select all loaded matches'))
 ui.rerender(<Fixture handle={handle} complete={false}/>);expect(handle.current!.count).toBe(1);expect(box('Select all loaded matches').indeterminate).toBe(true)
 ui.rerender(<Fixture handle={handle} items={[]}/>);expect(box('Select all matches').disabled).toBe(true);expect(handle.current!.count).toBe(0)
})
it('keeps checkbox and whole selection cell independent from opening the row; supports shift ranges',()=>{
 const handle=createRef<API>(),open=vi.fn(),ui=render(<Fixture handle={handle} open={open}/>)
 fireEvent.click(box('Select Alpha'));fireEvent.click(box('Select Alpha').closest('td')!);expect(open).not.toHaveBeenCalled()
 ui.rerender(<Fixture handle={handle} open={open} page={1}/>)
 fireEvent.click(box('Select Gamma'),{shiftKey:true});expect(handle.current!.selectedRows).toEqual(rows)
 fireEvent.click(screen.getByRole('button',{name:'Gamma'}).closest('td')!);expect(open).toHaveBeenCalledOnce()
 expect(handle.current!.count).toBe(3)
})
it('scopes select-all and Escape to the table without intercepting search text selection',()=>{
 const handle=createRef<API>();render(<Fixture handle={handle}/>)
 fireEvent.keyDown(screen.getByRole('textbox',{name:'Search'}),{key:'a',ctrlKey:true});expect(handle.current!.count).toBe(0)
 fireEvent.keyDown(box('Select Alpha'),{key:'a',ctrlKey:true});expect(handle.current!.count).toBe(3)
 fireEvent.keyDown(box('Select Alpha'),{key:'a',ctrlKey:true});expect(handle.current!.count).toBe(3)
 fireEvent.keyDown(box('Select Alpha'),{key:'Escape'});expect(handle.current!.count).toBe(0)
})
it('exports only the selected rows without treating the extract as a verification record',async()=>{
 let blob:Blob|undefined
 vi.spyOn(URL,'createObjectURL').mockImplementation(value=>{blob=value as Blob;return 'blob:fixture'})
 vi.spyOn(HTMLAnchorElement.prototype,'click').mockImplementation(()=>{})
 const handle=createRef<API>();render(<Fixture handle={handle}/>)
 fireEvent.click(box('Select Beta'))
 fireEvent.keyDown(screen.getByRole('button',{name:'1 selected'}),{key:'Enter'})
 fireEvent.click(await screen.findByRole('menuitem',{name:'Export selected rows'}))
 const text=await new Promise<string>(resolve=>{const reader=new FileReader();reader.onload=()=>resolve(String(reader.result));reader.readAsText(blob!)})
 const value=JSON.parse(text);expect(value.format).toBe('desk-table-selection');expect(value.rows).toEqual([rows[1]])
 expect(handle.current!.count).toBe(1)
 act(()=>handle.current!.clear());expect(within(screen.getByRole('table')).getAllByRole('checkbox').every(el=>!(el as HTMLInputElement).checked)).toBe(true)
})
