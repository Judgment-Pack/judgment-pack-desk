import {cleanup,fireEvent,render,screen} from '@testing-library/react'
import {QueryClient,QueryClientProvider} from '@tanstack/react-query'
import {MemoryRouter} from 'react-router-dom'
import {afterEach,it,expect,vi} from 'vitest'
import {DialectDraftEditor} from './DialectDraftPage'
const fixture=vi.hoisted(()=>({source:'prep_'+'b'.repeat(32),entity:'entity-one',revision:'a'.repeat(64),display:'Notice one',kind:'record',last:undefined as any}))
vi.mock('../config/DeskConfigProvider',()=>({useEffectiveConfig:()=>({aiConnections:{data:{connections:[]}}})}))
vi.mock('../assistant/aiConnections',()=>({deskAIConnections:()=>[],resolveAIConnection:()=>({assistant:{engine:'vercel',endpoint:{model:'test',models:['test']}},connection:{id:'legacy-api',revision:'a'.repeat(64)}})}))
vi.mock('../search/connections',()=>({useSearchConnections:()=>({available:true,loading:false,data:{connections:[]}})}))
vi.mock('../shell/UnsavedChanges',()=>({useConfirmDiscard:()=>async()=>true,useRegisteredChanges:()=>()=>{}}))
vi.mock('../chat/ChatProvider',()=>({useChats:()=>({store:undefined})}))
vi.mock('./DialectAssistant',()=>({DialectAssistant:()=>null}))
vi.mock('./RecipePackSection',()=>({RecipePackSection:()=>null}))
vi.mock('./ExchangeResearchTemplate',()=>({ExchangeResearchTemplate:()=>null}))
vi.mock('../shell/QueryStatusNotice',()=>({QueryStatusNotice:()=>null}))
vi.mock('../ui/PageLayout',()=>({PageHeader:({actions}:any)=><header>{actions}</header>,PageBody:({children}:any)=><main>{children}</main>}))
vi.mock('../ui/Tabs',()=>({Tabs:({tabs,value}:any)=>tabs.find((t:any)=>t.value===value).panel}))
vi.mock('../ui/Select',()=>({Select:({options,onValueChange,...props}:any)=><select {...props} onChange={e=>onValueChange(e.target.value)}>{options.map((o:any)=><option key={o.value} value={o.value}>{o.label}</option>)}</select>}))
vi.mock('./queries',()=>({dialectListQuery:()=>({queryKey:['preps'],queryFn:async()=>({items:[{id:fixture.source,name:'Original records'},{id:'prep_'+'c'.repeat(32),name:'Other records'}]})}),dialectQuery:(id:string)=>({queryKey:['prep',id],queryFn:async()=>({sha256:fixture.revision,document:{records:[{id:'record-one',research:{entity:{kind:fixture.kind,id:fixture.entity,name:fixture.display}}}]}})})}))
vi.mock('./useDialectDraft',async()=>{const {useState}=await import('react');return {useDialectDraft:(initial:any)=>{const [value,setValue]=useState({name:initial.document.name,recipe:initial.document.recipe});fixture.last=value;return {value,setValue,saved:initial,busy:false,dirty:false,error:undefined,flush:async()=>initial,clear:()=>{},reload:async()=>{},settled:async()=>{},current:()=>initial}}}})
afterEach(cleanup)
function show(target:'records'|'people-at-companies'='records',retainedRecipe?:any){
 fixture.display=target==='records'?'Notice one':'Company one';fixture.kind=target==='records'?'record':'company'
 const initial:any={sha256:fixture.revision,document:{id:'draft_one',version:1,name:'Related records',recipe:retainedRecipe??{intent:'Find related records',template:{id:target==='records'?'related-records':'company-people',name:'Related records',license:'Apache-2.0',digest:'sha256:'+fixture.revision},research:{version:1,target,fields:[{name:'description',type:'string',description:'Description'}],criteria:[],limits:{records:10,searches:0,pages:20,modelCalls:40},ai:{connection:'legacy-api',revision:fixture.revision,model:'test'},companyInputs:[{preparationId:fixture.source,revision:fixture.revision,entityId:fixture.entity}]}}}}
 const cache=new QueryClient({defaultOptions:{queries:{retry:false}}})
 const ui=render(<MemoryRouter><QueryClientProvider client={cache}><DialectDraftEditor initial={initial}/></QueryClientProvider></MemoryRouter>)
 return {cache,initial,unmount:ui.unmount}
}
it('clearing the last generic parent preserves the selector and blocks Save',async()=>{
 const {cache}=show()
 fireEvent.click(await screen.findByRole('checkbox',{name:'Notice one'}))
 expect(fixture.last.recipe.research.companyInputs).toEqual([])
 expect(screen.queryByRole('combobox',{name:'Source records'})).not.toBeNull()
 expect(fixture.last.recipe.research.requireParents).toBe(true)
 expect(screen.getByText('Select source records for related research.')).toBeTruthy()
 expect((screen.getByRole('button',{name:'Save to Dialects'}) as HTMLButtonElement).disabled).toBe(true)
 cache.clear()
})
it('changing generic parent source clears the selection and requires a replacement',async()=>{
 const {cache}=show()
 await screen.findByRole('checkbox',{name:'Notice one'})
 fireEvent.change(screen.getByRole('combobox',{name:'Source records'}),{target:{value:'prep_'+'c'.repeat(32)}})
 expect(screen.queryByRole('combobox',{name:'Source records'})).not.toBeNull()
 expect(fixture.last.recipe.research.companyInputs).toEqual([])
 expect(fixture.last.recipe.research.companyIds).toEqual([])
 expect(fixture.last.recipe.research.requireParents).toBe(true)
 expect((screen.getByRole('button',{name:'Save to Dialects'}) as HTMLButtonElement).disabled).toBe(true)
 cache.clear()
})
it('reloads a historical company-mode selection without rewriting its saved recipe',async()=>{
 const first=show('people-at-companies')
 const original=JSON.stringify(first.initial.document.recipe)
 expect((await screen.findByRole('checkbox',{name:'Company one'}) as HTMLInputElement).checked).toBe(true)
 expect(screen.getByRole('combobox',{name:'Company records'})).toBeTruthy()
 expect(screen.queryByRole('combobox',{name:'Source records'})).toBeNull()
 expect(JSON.stringify(fixture.last.recipe)).toBe(original)
 const retained=JSON.parse(JSON.stringify(fixture.last.recipe))
 first.unmount();first.cache.clear()
 const next=show('people-at-companies',retained)
 expect((await screen.findByRole('checkbox',{name:'Company one'}) as HTMLInputElement).checked).toBe(true)
 expect(JSON.stringify(fixture.last.recipe)).toBe(original)
 expect(fixture.last.recipe.research.companyInputs).toEqual([{preparationId:fixture.source,revision:fixture.revision,entityId:fixture.entity}])
 expect((screen.getByRole('button',{name:'Save to Dialects'}) as HTMLButtonElement).disabled).toBe(false)
 next.cache.clear()
})
