import { QueryClientProvider } from '@tanstack/react-query'
import { cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react'
import { afterEach, expect, it, vi } from 'vitest'
import { testQueryClient } from '../testing/harness'
import { DeskConfigFixture } from '../config/DeskConfigProvider'
import { decodeDeskConfig, effectiveConfig } from '../config/deskConfig'
import { DocumentSettings } from './DocumentSettings'
const mocks=vi.hoisted(()=>({fetch:vi.fn()}))
vi.mock('../files/client',async original=>({...await original<typeof import('../files/client')>(),deskFetch:mocks.fetch}))
afterEach(()=>{cleanup();vi.clearAllMocks()})
function setup(){
 const content=JSON.stringify({deskConfigVersion:1,research:{gateway:{url:'http://localhost:8787',authority:'gateway:test',signer:{algorithm:'ed25519',public:'ab'.repeat(32)}},sources:{read:{source:'read',dialect:'jina-reader'}}}})
 const effective=effectiveConfig(undefined,undefined,undefined,{path:'/private/desk.json',present:true,sha256:'revision-one',text:content,decoded:decodeDeskConfig(content,'desk')})
 return render(<QueryClientProvider client={testQueryClient()}><DeskConfigFixture value={effective}><DocumentSettings /></DeskConfigFixture></QueryClientProvider>)
}
it('saves Documents through the conditional config write and preserves research sources',async()=>{
 mocks.fetch.mockResolvedValue(Response.json({path:'/private/desk.json',sha256:'revision-two'}));setup()
 fireEvent.click(screen.getByLabelText('Enable document uploads through the gateway'))
 fireEvent.click(screen.getByRole('button',{name:'Save'}))
 await screen.findByText('Saved.')
 const sent=JSON.parse(mocks.fetch.mock.calls[0]![1].body)
 expect(sent.ifMatch).toBe('revision-one');expect(sent.research.documents.source).toBe('documents');expect(sent.research.sources.read.source).toBe('read');expect(sent.assistant).toBeUndefined()
})
it('keeps user input after a stale write instead of claiming success',async()=>{
 mocks.fetch.mockResolvedValue(Response.json({error:'Configuration changed',code:'desk-config-changed'},{status:409}));setup()
 fireEvent.change(screen.getByLabelText('Document source name'),{target:{value:'my-documents'}})
 fireEvent.click(screen.getByLabelText('Enable document uploads through the gateway'))
 fireEvent.click(screen.getByRole('button',{name:'Save'}))
 await screen.findByRole('alert');expect((screen.getByLabelText('Document source name') as HTMLInputElement).value).toBe('my-documents')
 expect(screen.queryByText('Saved.')).toBeNull()
})
it('rejects an encoded file bound larger than the request before any request leaves',async()=>{
 setup();fireEvent.click(screen.getByLabelText('Enable document uploads through the gateway'))
 fireEvent.change(screen.getByLabelText('Gateway request limit (bytes)'),{target:{value:'65536'}})
 fireEvent.click(screen.getByRole('button',{name:'Save'}))
 await waitFor(()=>expect(screen.getByRole('alert').textContent).toContain('encoded file'))
 expect(mocks.fetch).not.toHaveBeenCalled()
})
