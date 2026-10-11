import { cleanup, fireEvent, render, screen, waitFor, within } from '@testing-library/react'
import { afterEach, expect, it } from 'vitest'
import { GraphOptions } from './GraphOptions'
import type { GraphDocumentMeta } from '../mcp/types'

afterEach(cleanup)
const meta:GraphDocumentMeta={status:'valid',id:'configured-flow',graphId:'declared-flow',graphVersion:'1.2.3',formatVersion:'1',path:'graphs/flow.graph.json',bytes:100,sha256:'a'.repeat(64)}
it('opens metadata in a dialog with restored focus',async()=>{
 render(<GraphOptions id="configured-flow" meta={meta}/>)
 const trigger=screen.getByRole('button',{name:'Graph options'})
 fireEvent.keyDown(trigger,{key:'Enter'})
 expect((await screen.findAllByRole('menuitem')).map(item=>item.textContent)).toEqual(['View JSON','Graph details…'])
 expect(screen.queryByText('graphs/flow.graph.json')).toBeNull()
 fireEvent.click(await screen.findByRole('menuitem',{name:'Graph details…'}))
 const dialog=await screen.findByRole('dialog',{name:'Graph details'})
 expect(screen.queryByRole('menu')).toBeNull()
 for(const value of ['configured-flow','declared-flow','1.2.3','graphs/flow.graph.json'])expect(within(dialog).getByText(value)).toBeTruthy()
 fireEvent.click(within(dialog).getByRole('button',{name:'Close'}))
 await waitFor(()=>expect(document.activeElement).toBe(trigger))
})
it('does not open an empty details dialog before metadata arrives',async()=>{
 render(<GraphOptions id="loading"/>)
 fireEvent.keyDown(screen.getByRole('button',{name:'Graph options'}),{key:'Enter'})
 const details=await screen.findByRole('menuitem',{name:'Graph details…'})
 expect(details.getAttribute('data-disabled')).not.toBeNull()
 fireEvent.click(details);expect(screen.queryByRole('dialog')).toBeNull()
})

it('opens exact raw JSON in the shared wide dialog and returns keyboard focus',async()=>{
 const raw='{ \"nodes\": {\r\n  \"unfinished\": '
 render(<GraphOptions id="graph" meta={meta} sourceText={raw}/>)
 const trigger=screen.getByRole('button',{name:'Graph options'})
 fireEvent.keyDown(trigger,{key:'Enter'})
 fireEvent.click(await screen.findByRole('menuitem',{name:'View JSON'}))
 const dialog=await screen.findByRole('dialog',{name:'Graph JSON'})
 expect(dialog.getAttribute('data-size')).toBe('wide')
 expect(within(dialog).getByRole('region',{name:'Graph JSON'}).textContent).toBe(raw)
 expect(within(dialog).getByRole('region',{name:'Graph JSON'}).getAttribute('data-wrap')).toBe('false')
 fireEvent.keyDown(dialog,{key:'Escape'})
 await waitFor(()=>expect(document.activeElement).toBe(trigger))
})
