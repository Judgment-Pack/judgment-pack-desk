import {useState} from 'react'
import {act,cleanup,fireEvent,render,screen} from '@testing-library/react'
import {afterEach,expect,it,vi} from 'vitest'
import {Tooltip} from 'radix-ui'
import {CollectionToolbar} from './CollectionToolbar'
afterEach(()=>{cleanup();vi.restoreAllMocks();vi.unstubAllGlobals()})
function Example(){
 const [search,setSearch]=useState(''),[state,setState]=useState('all'),[review,setReview]=useState(false)
 return <Tooltip.Provider><CollectionToolbar label="Controls" searchLabel="Search inputs" search={search} onSearch={setSearch} filters={[{label:'State',value:state,onValueChange:setState,options:[{value:'all',label:'All states'},{value:'ready',label:'Ready'}]}]} toggles={[{label:'Needs attention',checked:review,onCheckedChange:setReview}]} filtered={!!search||state!=='all'||review} onReset={()=>{setSearch('');setState('all');setReview(false)}}/><output>{state}:{review?'attention':'all'}</output></Tooltip.Provider>
}
it('filters and clears from the wide control row without changing the search label',async()=>{
 render(<Example/>);const search=screen.getByRole('searchbox',{name:'Search inputs'})
 expect(search.getAttribute('placeholder')).toBe('Search…')
 fireEvent.change(search,{target:{value:'supplier'}})
 fireEvent.keyDown(screen.getByRole('button',{name:'State'}),{key:'Enter'})
 fireEvent.click(await screen.findByRole('menuitemradio',{name:'Ready'}))
 expect(screen.getByText('ready:all')).toBeTruthy()
 fireEvent.click(screen.getByRole('button',{name:'Clear filters'}))
 expect((search as HTMLInputElement).value).toBe('');expect(screen.getByText('all:all')).toBeTruthy()
})
it('retains the search and filter values when controls collapse and keeps toggles and reset reachable',async()=>{
 let width=1000,resize=()=>{}
 vi.spyOn(HTMLElement.prototype,'getBoundingClientRect').mockImplementation(()=>({width,height:32,x:0,y:0,top:0,left:0,right:width,bottom:32,toJSON(){}}))
 vi.stubGlobal('ResizeObserver',class{constructor(callback:()=>void){resize=callback}observe(){}disconnect(){}})
 render(<Example/>);const search=screen.getByRole('searchbox',{name:'Search inputs'})
 fireEvent.change(search,{target:{value:'supplier'}})
 act(()=>{width=380;resize()})
 expect((search as HTMLInputElement).value).toBe('supplier')
 fireEvent.keyDown(screen.getByRole('button',{name:'View options'}),{key:'Enter'})
 fireEvent.click(await screen.findByRole('menuitemradio',{name:'Ready'}))
 fireEvent.click(screen.getByRole('menuitemcheckbox',{name:'Needs attention'}))
 expect(screen.getByText('ready:attention')).toBeTruthy()
 fireEvent.click(screen.getByRole('menuitem',{name:'Clear filters'}))
 expect((search as HTMLInputElement).value).toBe('');expect(screen.getByText('all:all')).toBeTruthy()
})
