import {cleanup,render,screen,within,fireEvent,waitFor} from '@testing-library/react'
import {QueryClientProvider} from '@tanstack/react-query'
import {afterEach,expect,it,vi} from 'vitest'
import {testQueryClient} from '../testing/harness'
import {PolicyHistory,previousPolicyRevision} from './PolicyHistory'
import {resolvePolicyComparison,type PolicyComparison as Comparison} from './comparison'
vi.mock('./comparison',()=>({resolvePolicyComparison:vi.fn()}))
vi.mock('../ui/CodeDiff',()=>({CodeDiff:()=> <div>Saved text comparison</div>}))
import type {PolicyDocument} from './client'

afterEach(cleanup)
const at='2026-10-17T12:00:00Z',actor={kind:'local',subject:'local-admin'},workflow={independentReviewRequired:false}
function fixture():PolicyDocument{
 const content={sha256:'digest-one',bytes:100}
 return {id:'policy-one',managementMode:'desk-managed',createdAt:at,updatedAt:at,candidate:{title:'Policy',narrative:'Text',artifactVersion:'1.0.0',digest:'digest-one',revisionId:'revision-one',content},workflow,workflowDigest:'workflow-one',reviewState:'approved',currentReviewId:'review-one',revisions:[{id:'revision-one',sequence:1,at,actor,digest:'digest-one',title:'Policy',artifactVersion:'1.0.0',content,workflow,workflowDigest:'workflow-one'}],reviews:[{id:'review-one',sequence:2,candidateRevisionId:'revision-one',at,actor,candidateDigest:'digest-one',workflowDigest:'workflow-one',decision:'approve'}],publications:[{id:'publication-one',sequence:3,candidateRevisionId:'revision-one',at,actor,artifactVersion:'1.0.0',candidateDigest:'digest-one',workflowDigest:'workflow-one',reviewId:'review-one',content}]}
}
const order=()=>within(screen.getByRole('table',{name:'Policy history'})).getAllByRole('rowheader').map(row=>row.textContent)
it('shows actual commit order for tied timestamps, including a later draft and review',()=>{
 const document=fixture(),client=testQueryClient()
 const view=render(<QueryClientProvider client={client}><PolicyHistory document={document}/></QueryClientProvider>)
 expect(order()).toEqual(['Published','Approved','Draft saved'])
 const later=structuredClone(document)
 later.revisions.push({...document.revisions[0],id:'revision-two',sequence:4,artifactVersion:'1.1.0',digest:'digest-two'})
 later.reviews.push({...document.reviews[0],id:'review-two',sequence:5,candidateRevisionId:'revision-two',candidateDigest:'digest-two',decision:'request-changes'})
 later.reviews.push({...document.reviews[0],id:'review-three',sequence:6,candidateRevisionId:'revision-two',candidateDigest:'digest-two'})
 view.rerender(<QueryClientProvider client={client}><PolicyHistory document={later}/></QueryClientProvider>)
 expect(order()).toEqual(['Approved','Changes requested','Draft saved','Published','Approved','Draft saved'])
})
it('keeps commit order when the wall clock moves backwards',()=>{
 const document=fixture()
 document.publications[0].at='2026-10-17T11:59:58Z'
 document.reviews[0].at='2026-10-17T11:59:59Z'
 render(<QueryClientProvider client={testQueryClient()}><PolicyHistory document={document}/></QueryClientProvider>)
 expect(order()).toEqual(['Published','Approved','Draft saved'])
})

it('passes the chosen old publication identity to Design pack even after a later saved revision',()=>{
 const document=fixture(),cache=testQueryClient(),old=document.revisions[0]!
 document.revisions.push({...old,id:'revision-new',sequence:4,artifactVersion:'2.0.0'})
 document.candidate={...document.candidate,revisionId:'revision-new',artifactVersion:'2.0.0'}
 cache.setQueryData(['policies',document.id,'revision',old.id],{candidate:{...document.candidate,revisionId:old.id,artifactVersion:'1.0.0'},workflow,workflowDigest:'workflow-one',at,actor})
 const designAction=vi.fn(()=> <span>Exact design action</span>)
 render(<QueryClientProvider client={cache}><PolicyHistory document={document} designAction={designAction}/></QueryClientProvider>)
 fireEvent.click(screen.getByRole('button',{name:'Inspect Published'}))
 expect(designAction).toHaveBeenLastCalledWith({policyId:document.id,publicationId:'publication-one'})
 expect(screen.getByText('Exact design action')).toBeTruthy()
})

it('compares the prior retained candidate rather than a repeated review of the same revision, and freezes that pair',async()=>{
 const document=fixture(),old=document.revisions[0]!
 document.revisions.push({...old,id:'revision-two',sequence:4,digest:'digest-two'})
 document.candidate={...document.candidate,revisionId:'revision-two'}
 document.reviews.push({...document.reviews[0]!,id:'review-two',sequence:5,candidateRevisionId:'revision-two'},{...document.reviews[0]!,id:'review-three',sequence:6,candidateRevisionId:'revision-two'})
 expect(previousPolicyRevision(document,'revision-two')?.id).toBe('revision-one')
 const side={identity:{content:{sha256:'hash',bytes:1}},text:'Text',metadata:{kind:'narrative' as const,completeness:'complete' as const}}
 vi.mocked(resolvePolicyComparison).mockImplementation(async(policyId,request)=>({policyId,before:{...side,selector:request.before},after:{...side,selector:request.after},differences:[],limitations:[]} as Comparison))
 const client=testQueryClient(),view=render(<QueryClientProvider client={client}><PolicyHistory document={document}/></QueryClientProvider>)
 fireEvent.click(screen.getAllByRole('button',{name:'Compare Approved with previous saved revision'})[0]!)
 await screen.findByText('Saved text comparison')
 expect(resolvePolicyComparison).toHaveBeenCalledWith(document.id,{before:{kind:'revision',revisionId:'revision-one'},after:{kind:'revision',revisionId:'revision-two'}},expect.any(AbortSignal))
 const newer=structuredClone(document);newer.candidate.revisionId='revision-three';newer.revisions.push({...old,id:'revision-three',sequence:7})
 view.rerender(<QueryClientProvider client={client}><PolicyHistory document={newer}/></QueryClientProvider>)
 expect(screen.getByText(/keeps the exact selected pair/)).toBeTruthy()
 await waitFor(()=>expect(resolvePolicyComparison).toHaveBeenCalledTimes(1))
})

it('distinguishes exact versions with repeated title and version labels',()=>{
 const document=fixture(),old=document.revisions[0]!
 document.revisions=[{...old,id:'revision_'+'1'.repeat(48)},{...old,id:'revision_'+'2'.repeat(48),sequence:4}]
 document.publications=[]
 render(<QueryClientProvider client={testQueryClient()}><PolicyHistory document={document}/></QueryClientProvider>)
 fireEvent.click(screen.getByText('Compare exact saved versions'))
 fireEvent.pointerDown(screen.getByRole('combobox',{name:'Before saved version'}),{button:0,ctrlKey:false,pointerType:'mouse'})
 const labels=screen.getAllByRole('option').map(option=>option.textContent)
 expect(labels).toHaveLength(2);expect(new Set(labels).size).toBe(2)
 expect(labels.some(label=>label?.includes('#1')&&label.includes('11111111'))).toBe(true)
 expect(labels.some(label=>label?.includes('#4')&&label.includes('22222222'))).toBe(true)
})
