import {useState} from 'react'
import {DetailsSlotContext} from '../shell/DetailsSlot'
import {QueryClient,QueryClientProvider} from '@tanstack/react-query'
import {cleanup,fireEvent,render,screen,waitFor} from '@testing-library/react'
import {afterEach,expect,it,vi} from 'vitest'
import {MappingEditor} from './MappingEditor'
import type {PackDocument} from '../mcp/types'
import type {MappingV2} from './mappingTypes'
afterEach(cleanup)
it('keeps source edits pending until valid configuration is explicitly applied',async()=>{
 const onChange=vi.fn(),onInvalid=vi.fn(),onDirty=vi.fn()
 const mapping:MappingV2={version:2,case:{facts:[],evidence:[]},sources:[{name:'records',kind:'operation',profile:'lookup',maxAge:300,arguments:{tool:'lookup',arguments:{}},read:{copy:{facts:[],evidence:[]}}}]}
 function Editor(){const [value,setValue]=useState(mapping);return <MappingEditor doc={{} as PackDocument} mapping={value} profiles={[]} disabled={false} onChange={next=>{onChange(next);setValue(next)}} {...{onInvalid,onDirty}}/>}
 render(<QueryClientProvider client={new QueryClient()}><Editor/></QueryClientProvider>)
 fireEvent.click(screen.getByRole('button',{name:'records'}))
 fireEvent.change(screen.getByLabelText('Request parameters (JSON)'),{target:{value:'{'}})
 fireEvent.change(screen.getByLabelText('Maximum source age (seconds)'),{target:{value:'600'}})
 fireEvent.click(screen.getByRole('button',{name:'Apply configuration'}))
 expect(onChange).not.toHaveBeenCalled();expect(onDirty).toHaveBeenCalled();expect(onInvalid).toHaveBeenLastCalledWith(true)
 expect(screen.getByRole('alert')).toBeTruthy()
 fireEvent.change(screen.getByLabelText('Request parameters (JSON)'),{target:{value:'{"vendor":"example"}'}})
 fireEvent.click(screen.getByRole('button',{name:'Apply configuration'}))
 expect(onChange.mock.calls[0]![0].sources[0]).toMatchObject({maxAge:600,arguments:{tool:'lookup',arguments:{vendor:'example'}}})
 await waitFor(()=>expect(onInvalid).toHaveBeenLastCalledWith(false))
})

it('retains the integration chooser and pending request when the details portal moves',async()=>{
 const dock=document.createElement('div'),drawer=document.createElement('div');document.body.append(dock,drawer)
 const query=new QueryClient({defaultOptions:{queries:{retry:false}}}),claim=()=>()=>{},reveal=vi.fn()
 const mapping:MappingV2={version:2,case:{facts:[],evidence:[]},sources:[{name:'records',kind:'operation',profile:'lookup',arguments:{tool:'lookup',arguments:{}},read:{copy:{facts:[],evidence:[]}}}]}
 const props={doc:{} as PackDocument,mapping,profiles:[],disabled:false,onChange:vi.fn(),onInvalid:vi.fn(),onDirty:vi.fn()}
 function Editor({target}:{target:HTMLElement}){const [value,setValue]=useState(mapping);return <QueryClientProvider client={query}><DetailsSlotContext.Provider value={{target,open:true,claim,reveal}}><MappingEditor {...props} mapping={value} onChange={next=>{props.onChange(next);setValue(next)}}/></DetailsSlotContext.Provider></QueryClientProvider>}
 const view=(target:HTMLElement)=><Editor target={target}/>
 const ui=render(view(dock));fireEvent.click(screen.getByRole('button',{name:'records'}))
 fireEvent.change(screen.getByLabelText('Request parameters (JSON)'),{target:{value:'{"case":"retained"}'}})
 expect((screen.getByRole('button',{name:'Integration lookup'}) as HTMLButtonElement).disabled).toBe(true)
 fireEvent.click(screen.getByRole('button',{name:'Apply configuration'}))
 fireEvent.click(screen.getByRole('button',{name:'Integration lookup'}))
 await screen.findByRole('dialog',{name:'Choose integration'})
 fireEvent.change(screen.getByRole('textbox',{name:'Search integrations…'}),{target:{value:'Local'}})
 ui.rerender(view(drawer))
 expect(screen.getByRole('dialog',{name:'Choose integration'})).toBeTruthy()
 expect((screen.getByRole('textbox',{name:'Search integrations…'}) as HTMLInputElement).value).toBe('Local')
 fireEvent.click(screen.getByRole('button',{name:'Cancel'}))
 await waitFor(()=>expect(screen.queryByRole('dialog')).toBeNull())
 expect(JSON.parse((screen.getByLabelText('Request parameters (JSON)') as HTMLTextAreaElement).value)).toEqual({case:'retained'})
 expect(dock.querySelector('textarea')).toBeNull();expect(drawer.querySelector('textarea')).toBeTruthy()
 ui.unmount();dock.remove();drawer.remove()
})

it('explains calculator binding setup on adding a source and keeps ordinary source details unchanged',async()=>{
 const {setSourceIntegration}=await import('./jobIntegrations')
 const calculator={digest:'digest',profile:{id:'fx',shape:'mcp' as const,source:'finance',class:'record' as const,authority:'gateway',publicKey:'key',adapter:{name:'mcp',version:'1',digest:'digest'},endpoint:null,tools:['convert'],calculator:{name:'fx-convert',version:'2.1.0'}}}
 const query=new QueryClient({defaultOptions:{queries:{retry:false}}})
 const props={doc:{} as PackDocument,profiles:[calculator],disabled:false,onChange:vi.fn(),onInvalid:vi.fn(),onDirty:vi.fn()}
 const draw=(mapping:MappingV2)=><QueryClientProvider client={query}><MappingEditor {...props} mapping={mapping}/></QueryClientProvider>
 const ui=render(draw({version:2,sources:[]}))
 ui.rerender(draw(setSourceIntegration({version:2},calculator)))
 expect(await screen.findByText('This source is a calculator. In Advanced mapping → Edit mapping, bind each reported input to a parameter and set a maximum age for each table it reads. Empty bindings reject answers that report inputs or tables.')).toBeTruthy()
 expect(screen.getByLabelText('Request parameters (JSON)')).toBeTruthy()
 ui.rerender(draw(setSourceIntegration({version:2})))
 expect(screen.queryByText(/This source is a calculator/)).toBeNull()
})
