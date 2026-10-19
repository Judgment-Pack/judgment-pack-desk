import { cleanup, render, screen } from '@testing-library/react'
import { afterEach, beforeEach, expect, it, vi } from 'vitest'
import { MemoryRouter } from 'react-router-dom'
import { Tooltip } from 'radix-ui'
import { emptySuite } from './model'
import { ChatTestProposals } from './ChatTestProposals'
const fixture = vi.hoisted(()=>({suites:{} as Record<string,unknown>,drafts:[] as {id:string}[]}))
vi.mock('./store',()=>({useTestStorage:()=>({query:{data:{content:{suites:fixture.suites}}}})}))
vi.mock('../../chat/ChatProvider',()=>({useChats:()=>({packDrafts:fixture.drafts})}))
const proposal = {id:'p',chatId:'chat',at:'2026-10-13T12:00:00Z',state:'ready',sources:[],attempts:[{document:{matrixVersion:'3',cases:[{id:'fit',facts:{},focus:'Fit',expectedDisposition:{kind:'outcome',outcomeId:'yes',reasons:[],handoff:{state:'none'}}}]},unknowns:[],findings:[]}]}
beforeEach(()=>{fixture.suites={pack:{...emptySuite(),proposals:[proposal]}};fixture.drafts=[]})
afterEach(cleanup)
const show=(history=false)=>render(<MemoryRouter><Tooltip.Provider><ChatTestProposals chatId="chat" history={history}/></Tooltip.Provider></MemoryRouter>)
it('links a retained proposal to review without rendering a second chat',()=>{
 const view=show();expect(screen.getByRole('link',{name:'Review cases'}).getAttribute('href')).toBe('/packs/pack/matrix?chat=chat&testProposal=p')
 expect(view.container.querySelector('textarea')).toBeNull()
})
it('keeps saved proposals in Related work but removes the pending notice',()=>{
 fixture.suites={pack:{...emptySuite(),proposals:[{...proposal,savedCases:{fit:'snapshot'}}]}}
 const view=show();expect(screen.queryByRole('link',{name:'Review cases'})).toBeNull()
 view.unmount();show(true);expect(screen.getByRole('link',{name:'Review cases'})).toBeTruthy()
})
it('does not expose another session’s proposals',()=>{
 fixture.suites={pack:{...emptySuite(),proposals:[{...proposal,chatId:'other'}]}}
 show();expect(screen.queryByRole('link',{name:'Review cases'})).toBeNull()
})
it('deduplicates proposals recovered from a draft into its finalized pack',()=>{
 fixture.drafts=[{id:'draft-one'}];fixture.suites={pack:{...emptySuite(),proposals:[proposal]},'draft-one':{...emptySuite(),proposals:[proposal]}}
 show(true);expect(screen.getAllByRole('link',{name:'Review cases'})).toHaveLength(1)
 expect(screen.getByRole('link',{name:'Review cases'}).getAttribute('href')).toContain('/packs/pack/matrix')
})
it('retains historical proposal links and uses the draft route for unfinalized work',()=>{
 fixture.drafts=[{id:'draft-one'}];fixture.suites={'draft-one':{...emptySuite(),proposals:[proposal,{...proposal,id:'p2',at:'2026-10-13T12:01:00Z'}]}}
 show(true);expect(screen.getAllByRole('link',{name:'Review cases'})).toHaveLength(2)
 expect(screen.getByText('History').closest('details')?.open).toBe(false)
 expect(screen.getAllByRole('link',{name:'Review cases'})[0].getAttribute('href')).toContain('/packs/drafts/draft-one?')
})
