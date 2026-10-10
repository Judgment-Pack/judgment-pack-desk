import { cleanup, render, screen } from '@testing-library/react'
import { afterEach, expect, it } from 'vitest'
import { MemoryRouter } from 'react-router-dom'
import { GraphArtifacts, GraphDraftNotice } from './GraphArtifacts'
import type { GraphDraft } from './drafts'

afterEach(cleanup)
const pending:GraphDraft={id:'flow',draftId:'draft-1',createdAt:'2026-10-10T12:00:00Z',path:'flow.graph.json',content:'{}'}
it('opens saved graphs and leaves pending drafts recoverable from the conversation',()=>{
  render(<MemoryRouter><GraphArtifacts chatId="chat-1" drafts={[pending,{...pending,draftId:'draft-2',id:'saved-flow',saved:true}]}/></MemoryRouter>)
  expect(screen.getByRole('link',{name:'Open graph'}).getAttribute('href')).toBe('/graphs/saved-flow')
  expect(screen.getByRole('link',{name:'Review graph'}).getAttribute('href')).toBe('/graphs?chat=chat-1&draft=draft-1')
})
it('keeps only the latest pending graph in the thread',()=>{
  render(<MemoryRouter><GraphDraftNotice chatId="chat-1" drafts={[pending,{...pending,draftId:'draft-2'},{...pending,draftId:'draft-3',saved:true}]}/></MemoryRouter>)
  const links=screen.getAllByRole('link',{name:'Review graph'})
  expect(links).toHaveLength(1)
  expect(links[0].getAttribute('href')).toBe('/graphs?chat=chat-1&draft=draft-2')
})
