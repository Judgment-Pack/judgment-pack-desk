import {cleanup,fireEvent,render,screen,waitFor} from '@testing-library/react'
import {QueryClientProvider} from '@tanstack/react-query'
import {createMemoryRouter,RouterProvider} from 'react-router-dom'
import {afterEach,expect,it,vi} from 'vitest'
import {testQueryClient} from '../testing/harness'
import {UnsavedChangesProvider} from '../shell/UnsavedChanges'
import {PolicyEditor} from './PolicyPage'
import {coveragePolicy} from '../testing/policyCoverage'
vi.mock('../chat/ChatProvider',()=>({useChats:()=>({store:null,ready:false})}))
vi.mock('../shell/InspectorSlot',()=>({useInspectorControls:()=>({reveal:vi.fn()})}))
vi.mock('./PolicyAssistant',()=>({PolicyAssistant:({editing,busy}:{editing:boolean;busy:boolean})=><div data-testid="shared-policy-assistant" data-editing={editing} data-busy={busy}/>}))
vi.mock('./PolicySourceStatus',()=>({PolicySourceStatus:()=>null}))
vi.mock('./PolicyConnectedSource',()=>({PolicyConnectedSource:()=>null}))
vi.mock('./PolicyLifecycle',()=>({PolicyLifecycle:()=>null}))
vi.mock('./PolicyHistory',()=>({PolicyHistory:()=>null}))
vi.mock('./PolicyInterpretation',()=>({PolicyInterpretationAction:()=>null}))
vi.mock('./PolicyCoverage',()=>({PolicyCoverage:({editing,disabled,onEditingChange,onPendingChange}:{editing:boolean;disabled:boolean;onEditingChange:(dirty:boolean)=>void;onPendingChange:(pending:boolean)=>void})=><div><span data-testid="coverage-guards" data-editing={editing} data-disabled={disabled}/><button onClick={()=>onEditingChange(true)}>Edit coverage fixture</button><button onClick={()=>onPendingChange(true)}>Hold coverage fixture</button></div>}))
afterEach(()=>{cleanup();vi.unstubAllGlobals()})
it('keeps one shared Assistant and protects Policy writes while coverage is dirty or delivery is held',async()=>{
 vi.stubGlobal('fetch',vi.fn(async()=>Response.json({revision:'source-none',supported:false,pending:false})))
 const router=createMemoryRouter([{path:'/policies/:policyId',element:<UnsavedChangesProvider><PolicyEditor initial={coveragePolicy}/></UnsavedChangesProvider>}],{initialEntries:['/policies/'+coveragePolicy.document.id]})
 render(<QueryClientProvider client={testQueryClient()}><RouterProvider router={router}/></QueryClientProvider>)
 fireEvent.mouseDown(screen.getByRole('tab',{name:'Coverage'}),{button:0,ctrlKey:false})
 await screen.findByTestId('coverage-guards')
 expect(screen.getAllByTestId('shared-policy-assistant')).toHaveLength(1)
 fireEvent.click(screen.getByRole('button',{name:'Edit coverage fixture'}))
 await waitFor(()=>expect(screen.getByTestId('shared-policy-assistant').getAttribute('data-editing')).toBe('true'))
 fireEvent.mouseDown(screen.getByRole('tab',{name:'Draft'}),{button:0,ctrlKey:false})
 fireEvent.change(screen.getByRole('textbox',{name:'Policy text'}),{target:{value:'Changed local Policy narrative'}})
 expect(screen.getByRole('button',{name:'Save draft'})).toHaveProperty('disabled',true)
 fireEvent.mouseDown(screen.getByRole('tab',{name:'Coverage'}),{button:0,ctrlKey:false})
 expect(screen.getByTestId('coverage-guards').getAttribute('data-editing')).toBe('true')
 fireEvent.click(screen.getByRole('button',{name:'Hold coverage fixture'}))
 await waitFor(()=>expect(screen.getByTestId('shared-policy-assistant').getAttribute('data-busy')).toBe('true'))
 expect(screen.getAllByTestId('shared-policy-assistant')).toHaveLength(1)
})
