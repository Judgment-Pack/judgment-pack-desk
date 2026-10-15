import { cleanup, fireEvent, render, screen } from '@testing-library/react'
import { afterEach, expect, it, vi } from 'vitest'
import { useState, useCallback, type ReactNode } from 'react'
import { WorkSummary } from './RunPresentation'
import { LinkStepDetails } from './LinkStepDetails'
import type { ChatAttachment } from './store'
import { readResponseHistory, workItems } from './responseHistory'
const reader=vi.hoisted(()=>vi.fn())
vi.mock('../documents/SourceReader',()=>({SourceReader:(props:unknown)=>{reader(props);return <section>Retained page</section>}}))
afterEach(()=>{cleanup();vi.clearAllMocks()})
const file:ChatAttachment={id:'12345678-1234-1234-1234-123456789012',name:'NASA',text:'',document:{id:'12345678-1234-1234-1234-123456789012',digest:'sha256:'+'a'.repeat(64),pages:[1,2,3],allowPartial:false},link:{url:'https://www.nasa.gov/'}}
const row={id:'read',name:'read_link',status:'complete' as const,link:{url:'https://www.nasa.gov/#missions',documentId:file.id,digest:file.document!.digest,pages:[2]}}
it.each(['https://www.nasa.gov/#missions','www.nasa.gov/#missions'])('opens the exact retained window for %s',async(url)=>{
 const requested={...row,link:{...row.link,url}}
 function Harness(){const [node,setNode]=useState<ReactNode>(null);const read=useCallback((n:ReactNode)=>setNode(n),[]);return <><WorkSummary work={{items:[requested],notices:[]}} documents={[file]} onRead={read}/><aside>{node}</aside></>}
 render(<Harness/>);fireEvent.click(screen.getByText('Work · 1 step'))
 const button=screen.getByRole('button',{name:'Read a link · www.nasa.gov · Done'})
 expect(button.querySelectorAll('svg').length).toBe(2)
 fireEvent.click(button);await screen.findByText('Retained page')
 expect(reader.mock.lastCall?.[0]).toMatchObject({requestedUrl:url,reference:{...file.document,pages:[2]}})
})
it('shows the requested URL if the exact retained document is unavailable, without substituting a neighbor',()=>{
 render(<LinkStepDetails row={row} document={{...file,document:{...file.document!,digest:'sha256:'+'b'.repeat(64)}}}/>)
 expect(screen.getByText(row.link.url)).toBeTruthy();expect(screen.getByText('No retained page is available for this step.')).toBeTruthy();expect(reader).not.toHaveBeenCalled()
})

// Tool arguments and saved step labels preserve the person's original spelling.
// That spelling may be a bare domain even though acquisition uses HTTPS.
it.each(['working','failed','interrupted','complete'] as const)('renders and inspects a saved bare-domain read that is %s',async(status)=>{
 const events=[{type:'tool_call' as const,callId:'bare',name:'read_link',args:{url:'judgmentpack.org'}}]
 const items=workItems(status==='failed'||status==='complete'?[...events,{type:'tool_result',callId:'bare',name:'read_link',text:'raw tool detail',isError:status==='failed'}]:events,status==='working')
 const [response]=readResponseHistory(JSON.parse(JSON.stringify([{id:'saved',documents:[],websites:[],sourceIds:[],work:{items,notices:[]}}])))
 function Harness(){const [node,setNode]=useState<ReactNode>(null);const read=useCallback((n:ReactNode)=>setNode(n),[]);return <><WorkSummary work={response!.work} onRead={read}/><aside>{node}</aside></>}
 render(<Harness/>);fireEvent.click(screen.getByText('Work · 1 step'))
 const label=status==='working'?'Working…':status==='complete'?'Done':status==='failed'?'Failed':'Interrupted'
 fireEvent.click(screen.getByRole('button',{name:`Read a link · judgmentpack.org · ${label}`}))
 await screen.findByText('judgmentpack.org')
 expect(reader).not.toHaveBeenCalled()
 expect(screen.queryByText('raw tool detail')).toBeNull()
 if(status!=='working')expect(screen.getByText('No retained page is available for this step.')).toBeTruthy()
})

it('keeps work inspectable when a link label cannot be parsed',()=>{
 render(<WorkSummary work={{items:[{id:'invalid',name:'read_link',status:'failed',link:{url:'https://['}}],notices:[]}} onRead={()=>{}}/>)
 fireEvent.click(screen.getByText('Work · 1 step'))
 expect(screen.getByRole('button',{name:'Read a link · Link · Failed'})).toBeTruthy()
})
it('explains the origin limit before a collapsed technical URL',()=>{
 render(<LinkStepDetails row={{id:'failed',name:'read_link',status:'failed',link:{url:'https://example.org/redirect/opaque'},linkFailure:'web-redirect-origin'}}/>)
 expect(screen.getByText('The link redirects to another website, which this reader does not allow.')).toBeTruthy()
 const summary=screen.getByText('Requested URL').closest('details')!
 expect(summary.open).toBe(false)
 fireEvent.click(screen.getByText('Requested URL'))
 const link=screen.getByRole('link',{name:'https://example.org/redirect/opaque'})
 expect(link.getAttribute('href')).toBe('https://example.org/redirect/opaque')
 expect(link.getAttribute('target')).toBe('_blank')
 expect(link.getAttribute('rel')).toContain('noreferrer')
 expect(reader).not.toHaveBeenCalled()
})
