import {publishVerifiedWrite} from '../files/verifiedWrites'
import {digestOf} from '../research/checkCandidate'
import {applyProposal} from '../assistant/acceptProposal'
import {buffered} from '../packs/edit/writes'
import {readFile,writeFile,type FileContent} from '../files/client'
import {sourceMessage} from '../i18n/source'
import type {Candidate} from '../research/run'
import {proposalProblems,samePack,type PackProposalBasis,type PackProposalCheck} from './packProposal'
export class ProposalSaveBlocked extends Error {}
/** Explicit host action: fresh checks, optimistic write, verified read-back, never replay. */
export async function savePackProposal({candidate,basis,reviewedSuiteKey,check,guard,read=readFile,write=writeFile}:{
 candidate:Candidate;basis:PackProposalBasis;reviewedSuiteKey:string;
 check:()=>Promise<PackProposalCheck>;guard:()=>void;
 read?:typeof readFile;write?:typeof writeFile;
}):Promise<FileContent> {
 guard()
 const before=await read(basis.path)
 guard()
 if(samePack(candidate.text,before.content)){publishVerifiedWrite({beforeSha256:await digestOf(basis.text),file:before});return before}
 if(before.content!==basis.text)throw new ProposalSaveBlocked(sourceMessage('The pack changed since this request. Ask for a proposal against the current version.'))
 if(before.readOnlyReason)throw new ProposalSaveBlocked(sourceMessage('This pack is read-only.'))
 const result=await check()
 guard()
 if(result.documentDigest!==candidate.digest || result.suiteKey!==reviewedSuiteKey)throw new ProposalSaveBlocked(sourceMessage('The saved test suite changed. Check the proposal and review the results again.'))
 const problems=proposalProblems(result)
 if(!result.valid || problems.mismatches.length)throw new ProposalSaveBlocked(sourceMessage('Resolve the validation findings and test mismatches before saving this proposal.'))
 const submitted=applyProposal(buffered(before.content),candidate.document).text
 if(!samePack(submitted,candidate.text))throw new ProposalSaveBlocked(sourceMessage('The proposal could not be applied completely. No write was attempted.'))
 guard()
 await write({path:basis.path,content:submitted,baseSha256:before.sha256})
 const saved=await read(basis.path)
 if(saved.content!==submitted)throw new ProposalSaveBlocked(sourceMessage('The save could not be verified. Refresh the pack before trying again.'))
 publishVerifiedWrite({beforeSha256:before.sha256,file:saved})
 return saved
}
