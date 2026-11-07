import {cleanup,fireEvent,render,screen,waitFor,within} from '@testing-library/react'
import {afterEach,beforeEach,expect,it,vi} from 'vitest'
import * as wire from '../../chat/wire'
import {SourceApprovalControl} from './SourceApprovalControl'
import {forgetSession,giveThisPageASessionForTesting} from '../../mcp/session'

const scope={},hash='a'.repeat(64),reviewId='0123456789abcdef0123456789abcdef',approvalId='abcdef0123456789abcdef0123456789'
const at='2026-01-02T03:04:05Z'
// Handwritten current declaration schemas from assistant_{search,link,website}_tools.go,
// assistant_dialect_research_tools.go and input_preparations.go; not captured output.
const schemas=[
 {type:'object',properties:{query:{type:'string',maxLength:2000,description:'A concise public search query. Do not include private attachments or secrets.'}},required:['query'],additionalProperties:false},
 {type:'object',properties:{url:{type:'string',maxLength:4096},offset:{type:'integer',minimum:0,maximum:9007199254740991},find:{type:'string',maxLength:200}},required:['url'],additionalProperties:false},
 {type:'object',properties:{url:{type:'string',maxLength:4096}},required:['url'],additionalProperties:false},
 {type:'object',properties:{id:{type:'string',pattern:'^prep_[a-f0-9]{32}$'},revision:{type:'string',pattern:'^[a-f0-9]{64}$'}},required:['id','revision'],additionalProperties:false},
]
// Handwritten complete Desk singleton catalog; never captured from a run.
function catalog(approved=false){
 return {version:1,sources:[
  ['desk-search','search_sources','discover','read-external','read','research','none'],
  ['desk-link','read_link','read','read-external','read','read','none'],
  ['desk-website','explore_website','discover','read-external','read','research','none'],
  ['desk-research-workflow','request_dialect_research','act','act','act','','required'],
 ].map(([adapterId,name,operation,effect,effectClass,delegation,approval],index)=>{
  const ready=index!==1
  return {snapshot:{adapterId,adapterVersion:'1',kind:'custom',trust:'first-party',configurationDigest:hash,toolListDigest:hash,
   completeDeclarations:[{name,description:'<b>Complete declaration</b>',inputSchema:schemas[index],taxonomy:{operation,effect},effectClass,delegation,approval}],
   configuration:{target:index===0?'https://source.example.test':'',targetDigest:'',authorityDigest:'',publicKeyDigest:'',deskDigest:hash,preferenceDigest:'',connection:'',revision:'',provider:'',bundleDigest:'',verifierDigest:'',gatewayBuildDigest:'',ownerDigest:hash,catalogDigest:'',catalogOwnerDigest:''},
   status:ready?'review-required':'setup-required',...(ready?{identity:{kind:'connection',label:'Desk research '+(index===3?'workflow ':'HTTP ')+hash},identityDigest:hash}:{})},
   status:ready?approved&&index===0?'approved':'review-required':'setup-required',
   ...(approved&&index===0?{approval:{id:approvalId,revision:1,approvedAt:at}}:ready&&!approved?{reviewId:index===0?reviewId:String(index).repeat(32),reviewRevision:1}:{})}
 })}
}
const json=(value:unknown,status=200)=>new Response(JSON.stringify(value),{status,headers:{'Content-Type':'application/json'}})
const row=(name='desk-search')=>within(screen.getByRole('region',{name}))
async function review(){fireEvent.click(screen.getByRole('button',{name:'Source approvals'}));fireEvent.click(screen.getByRole('button',{name:'Review sources'}));await screen.findByRole('region',{name:'desk-search'})}
beforeEach(()=>giveThisPageASessionForTesting('source-ui-session'))
afterEach(()=>{cleanup();vi.restoreAllMocks();vi.unstubAllGlobals()})
it('observes only on explicit review and shows complete escaped catalog with distinct workflow consent',async()=>{
 const fetch=vi.fn().mockResolvedValue(json(catalog()));vi.stubGlobal('fetch',fetch)
 const view=render(<SourceApprovalControl chatId="chat-one" scope={scope}/>);expect(fetch).not.toHaveBeenCalled()
 fireEvent.click(screen.getByRole('button',{name:'Source approvals'}));expect(fetch).not.toHaveBeenCalled()
 fireEvent.click(screen.getByRole('button',{name:'Review sources'}));await screen.findByRole('region',{name:'desk-search'})
 expect(fetch).toHaveBeenCalledOnce();expect(fetch.mock.calls[0][0]).toBe('/api/assistant-source-approvals/chat-one')
 expect(screen.getByText('Research workflow source consent is separate from its action approval and compute allowance.')).toBeTruthy()
 expect(row('desk-research-workflow').getAllByText('act')).toHaveLength(2);expect(row('desk-research-workflow').getByText('required')).toBeTruthy();expect(row('desk-research-workflow').getByText('Not declared')).toBeTruthy()
 expect(row('desk-link').queryByRole('button',{name:'Approve source'})).toBeNull()
 for(const name of ['desk-search','desk-link','desk-website','desk-research-workflow']){
  const details=row(name).getByLabelText('Complete source snapshot')
  expect(details.textContent).toContain('"completeDeclarations"');expect(details.textContent).toContain('"inputSchema"');expect(details.textContent).toContain('"ownerDigest"');expect(details.textContent).toContain('<b>Complete declaration</b>')
  expect(details.querySelector('b')).toBeNull()
 }
 expect(row().queryByRole('link')).toBeNull()
 view.rerender(<SourceApprovalControl chatId="chat-one" scope={scope}/>);window.dispatchEvent(new Event('focus'))
 fireEvent.click(screen.getByRole('button',{name:'Close'}));fireEvent.click(screen.getByRole('button',{name:'Source approvals'}));expect(fetch).toHaveBeenCalledOnce()
})
it('sends one explicit closed approval, then exact revoke receipt and requires explicit refresh',async()=>{
 const fetch=vi.fn().mockResolvedValueOnce(json(catalog())).mockResolvedValueOnce(json(catalog(true))).mockResolvedValueOnce(json({version:1,status:'revoked',approval:{id:approvalId,revision:2,approvedAt:at,revokedAt:'2026-01-02T03:05:05Z'}}))
 vi.stubGlobal('fetch',fetch);render(<SourceApprovalControl chatId="chat-one" scope={scope}/>);await review()
 const approve=row().getByRole('button',{name:'Approve source'});fireEvent.click(approve);fireEvent.click(approve)
 await screen.findByRole('button',{name:'Revoke source approval'})
 expect(fetch).toHaveBeenCalledTimes(2);expect(JSON.parse(fetch.mock.calls[1][1].body)).toEqual({reviewId,reviewRevision:1})
 expect(row('desk-website').queryByRole('button',{name:'Approve source'})).toBeNull()
 const revoke=screen.getByRole('button',{name:'Revoke source approval'});fireEvent.click(revoke);fireEvent.click(revoke)
 await screen.findByRole('region',{name:'Source approval receipt'})
 expect(fetch).toHaveBeenCalledTimes(3);expect(JSON.parse(fetch.mock.calls[2][1].body)).toEqual({approvalId,approvalRevision:1})
 expect(screen.getByRole('button',{name:'Revoke source approval'})).toHaveProperty('disabled',true)
 expect(screen.getByText('Source review is stale. Refresh before deciding.')).toBeTruthy()
})
it.each(['uncertain','refused','invalid receipt'])('requires explicit refresh after %s without replay',async mode=>{
 const fetch=vi.fn().mockResolvedValueOnce(json(catalog()))
 if(mode==='uncertain')fetch.mockRejectedValueOnce(Error('secret-provider-body'))
 else fetch.mockResolvedValueOnce(mode==='refused'?json({},409):json({}))
 fetch.mockResolvedValueOnce(json(catalog()));vi.stubGlobal('fetch',fetch)
 render(<SourceApprovalControl chatId="chat-one" scope={scope}/>);await review();fireEvent.click(row().getByRole('button',{name:'Approve source'}))
 await screen.findByRole('alert');expect(screen.queryByText('secret-provider-body')).toBeNull()
 const approve=row().getByRole('button',{name:'Approve source'});expect(approve).toHaveProperty('disabled',true);fireEvent.click(approve);expect(fetch).toHaveBeenCalledTimes(2)
 fireEvent.click(screen.getByRole('button',{name:'Refresh source review'}))
 await waitFor(()=>expect(row().getByRole('button',{name:'Approve source'})).toHaveProperty('disabled',false))
 expect(fetch).toHaveBeenCalledTimes(3);expect(fetch.mock.calls.filter(([,options])=>options?.method==='POST')).toHaveLength(1)
})
it('serializes review and discards a late observation when the same chat changes store',async()=>{
 let release!:(response:Response)=>void
 const fetch=vi.fn().mockImplementation(()=>new Promise<Response>(resolve=>{release=resolve}));vi.stubGlobal('fetch',fetch)
 const view=render(<SourceApprovalControl chatId="chat-one" scope={scope}/>)
 fireEvent.click(screen.getByRole('button',{name:'Source approvals'}));const button=screen.getByRole('button',{name:'Review sources'});fireEvent.click(button);fireEvent.click(button)
 await waitFor(()=>expect(fetch).toHaveBeenCalledOnce())
 view.rerender(<SourceApprovalControl chatId="chat-one" scope={{}}/>);release(json(catalog()))
 fireEvent.click(screen.getByRole('button',{name:'Source approvals'}));expect(screen.queryByRole('region',{name:'desk-search'})).toBeNull();expect(fetch).toHaveBeenCalledOnce()
})
it('clears acknowledged review on session end and never reuses it after signing in again',async()=>{
 const fetch=vi.fn().mockResolvedValue(json(catalog()));vi.stubGlobal('fetch',fetch)
 render(<SourceApprovalControl chatId="chat-one" scope={scope}/>);await review()
 forgetSession();await waitFor(()=>expect(screen.queryByRole('region',{name:'desk-search'})).toBeNull())
 giveThisPageASessionForTesting('new-source-session');expect(screen.queryByRole('button',{name:'Approve source'})).toBeNull();expect(fetch).toHaveBeenCalledOnce()
})

it('retains stale display and disables every decision if complete formatting fails',async()=>{
 const fetch=vi.fn().mockResolvedValue(json(catalog()));vi.stubGlobal('fetch',fetch)
 render(<SourceApprovalControl chatId="chat-one" scope={scope}/>);await review()
 vi.spyOn(wire,'wireJSON').mockImplementationOnce(()=>{throw new RangeError('formatter stack unavailable')})
 fireEvent.click(screen.getByRole('button',{name:'Refresh source review'}));await screen.findByRole('alert')
 expect(row().getByLabelText('Complete source snapshot').textContent).toContain('"inputSchema"')
 for(const button of screen.getAllByRole('button',{name:'Approve source'}))expect(button).toHaveProperty('disabled',true)
 expect(fetch).toHaveBeenCalledTimes(2);expect(fetch.mock.calls.every(([,options])=>options?.method==='GET')).toBe(true)
 expect(screen.queryByText('formatter stack unavailable')).toBeNull()
})
