import {cleanup,render,screen,within,fireEvent} from '@testing-library/react'
import {QueryClientProvider} from '@tanstack/react-query'
import {afterEach,expect,it,vi} from 'vitest'
import {testQueryClient} from '../testing/harness'
import {PolicyHistory} from './PolicyHistory'
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
