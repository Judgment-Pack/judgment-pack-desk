import { cleanup, fireEvent, render, screen } from '@testing-library/react'
import { afterEach, expect, it, vi } from 'vitest'
import { useState, useCallback, type ReactNode } from 'react'
import { WorkSummary } from './RunPresentation'
import { LinkStepDetails } from './LinkStepDetails'
import type { ChatAttachment } from './store'
const reader=vi.hoisted(()=>vi.fn())
vi.mock('../documents/SourceReader',()=>({SourceReader:(props:unknown)=>{reader(props);return <section>Retained page</section>}}))
afterEach(()=>{cleanup();vi.clearAllMocks()})
const file:ChatAttachment={id:'12345678-1234-1234-1234-123456789012',name:'NASA',text:'',document:{id:'12345678-1234-1234-1234-123456789012',digest:'sha256:'+'a'.repeat(64),pages:[1,2,3],allowPartial:false},link:{url:'https://www.nasa.gov/'}}
const row={id:'read',name:'read_link',status:'complete' as const,link:{url:'https://www.nasa.gov/#missions',documentId:file.id,digest:file.document!.digest,pages:[2]}}
it('makes Read a link open the exact retained window in the right pane',async()=>{
 function Harness(){const [node,setNode]=useState<ReactNode>(null);const read=useCallback((n:ReactNode)=>setNode(n),[]);return <><WorkSummary work={{items:[row],notices:[]}} documents={[file]} onRead={read}/><aside>{node}</aside></>}
 render(<Harness/>);fireEvent.click(screen.getByText('Work · 1 step'))
 const button=screen.getByRole('button',{name:'Read a link · www.nasa.gov · Done'})
 expect(button.querySelectorAll('svg').length).toBe(2)
 fireEvent.click(button);await screen.findByText('Retained page')
 expect(reader.mock.lastCall?.[0]).toMatchObject({requestedUrl:row.link.url,reference:{...file.document,pages:[2]}})
})
it('shows the requested URL if the exact retained document is unavailable, without substituting a neighbor',()=>{
 render(<LinkStepDetails row={row} document={{...file,document:{...file.document!,digest:'sha256:'+'b'.repeat(64)}}}/>)
 expect(screen.getByText(row.link.url)).toBeTruthy();expect(screen.getByText('No retained page is available for this step.')).toBeTruthy();expect(reader).not.toHaveBeenCalled()
})
