import {cleanup,fireEvent,render,screen} from '@testing-library/react'
import {QueryClient,QueryClientProvider} from '@tanstack/react-query'
import {MemoryRouter} from 'react-router-dom'
import {afterEach,it,expect,vi} from 'vitest'
import {DialectDraftEditor} from './DialectDraftPage'
const fixture=vi.hoisted(()=>({source:'prep_'+'b'.repeat(32),entity:'entity-one',revision:'a'.repeat(64),last:undefined as any}))
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
vi.mock('./queries',()=>({dialectListQuery:()=>({queryKey:['preps'],queryFn:async()=>({items:[{id:fixture.source,name:'Original companies'},{id:'prep_'+'c'.repeat(32),name:'Other companies'}]})}),dialectQuery:(id:string)=>({queryKey:['prep',id],queryFn:async()=>({sha256:fixture.revision,document:{records:[{id:'record-one',research:{entity:{kind:'record',id:fixture.entity,name:'Company one'}}}]}})})}))
vi.mock('./useDialectDraft',async()=>{const {useState}=await import('react');return {useDialectDraft:(initial:any)=>{const [value,setValue]=useState({name:initial.document.name,recipe:initial.document.recipe});fixture.last=value;return {value,setValue,saved:initial,busy:false,dirty:false,error:undefined,flush:async()=>initial,clear:()=>{},reload:async()=>{},settled:async()=>{},current:()=>initial}}}})
afterEach(cleanup)
function show(){const initial:any={sha256:fixture.revision,document:{id:'draft_one',version:1,name:'Related records',recipe:{intent:'Find related records',template:{id:'related-records',name:'Related records',license:'Apache-2.0',digest:'sha256:'+fixture.revision},research:{version:1,target:'records',fields:[{name:'description',type:'string',description:'Description'}],criteria:[],limits:{records:10,searches:0,pages:20,modelCalls:40},ai:{connection:'legacy-api',revision:fixture.revision,model:'test'},companyInputs:[{preparationId:fixture.source,revision:fixture.revision,entityId:fixture.entity}]}}}};const cache=new QueryClient({defaultOptions:{queries:{retry:false}}});render(<MemoryRouter><QueryClientProvider client={cache}><DialectDraftEditor initial={initial}/></QueryClientProvider></MemoryRouter>);return cache}
it('clearing last parent preserves selector and blocks Save',async()=>{const cache=show();fireEvent.click(await screen.findByRole('checkbox',{name:'Company one'}));expect(fixture.last.recipe.research.companyInputs).toEqual([]);expect(screen.queryByRole('combobox',{name:'Company records'})).not.toBeNull();expect(fixture.last.recipe.research.requireParents).toBe(true);expect((screen.getByRole('button',{name:'Save to Dialects'}) as HTMLButtonElement).disabled).toBe(true);cache.clear()})
it('changing parent source preserves selector for replacement selection',async()=>{const cache=show();await screen.findByRole('checkbox',{name:'Company one'});fireEvent.change(screen.getByRole('combobox',{name:'Company records'}),{target:{value:'prep_'+'c'.repeat(32)}});expect(screen.queryByRole('combobox',{name:'Company records'})).not.toBeNull();expect(fixture.last.recipe.research.requireParents).toBe(true);expect((screen.getByRole('button',{name:'Save to Dialects'}) as HTMLButtonElement).disabled).toBe(true);cache.clear()})
