import {expect,it,vi} from 'vitest'
import {savePackProposal,ProposalSaveBlocked} from './savePackProposal'
import {digestOf} from '../research/checkCandidate'
import type {Candidate} from '../research/run'
const before={path:'p.json',sha256:'old',content:'{ "id": "p", "version": "0.1.0" }',bytes:40}
const basis={packId:'p',path:'p.json',text:before.content}
const document={id:'p',version:'0.1.1'},text=JSON.stringify(document)
const candidate:Candidate={revision:1,producedBy:'conversation',document,text,digest:await digestOf(text)}
const result={documentDigest:candidate.digest,suiteKey:'suite',at:new Date().toISOString(),valid:true,diagnostics:[],totalCases:0}
function harness(){let file=before;const read=vi.fn(async()=>file),write=vi.fn(async args=>{file={...file,content:args.content,sha256:'new'};return file});return {candidate,basis,reviewedSuiteKey:'suite',read,write,guard:vi.fn(),check:vi.fn(async()=>result)}}
it('saves directly with the observed revision and verifies the real read-back',async()=>{
 const h=harness(),saved=await savePackProposal(h)
 expect(JSON.parse(saved.content)).toEqual(document)
 expect(saved.content).toContain('"id": "p"')
 expect(h.check).toHaveBeenCalledOnce();expect(h.write).toHaveBeenCalledOnce()
 expect(h.write.mock.calls[0]![0]).toMatchObject({path:'p.json',baseSha256:'old'})
 expect(h.write.mock.calls[0]![0]).not.toHaveProperty('override');expect(h.read).toHaveBeenCalledTimes(2)
})
it('blocks changed files, changed suites, mismatches and editor changes before dispatch',async()=>{
 const stale=harness();stale.read.mockResolvedValue({...before,content:'{"id":"other"}'})
 await expect(savePackProposal(stale)).rejects.toThrow('pack changed');expect(stale.write).not.toHaveBeenCalled()
 const suite=harness();suite.check.mockResolvedValue({...result,suiteKey:'changed'})
 await expect(savePackProposal(suite)).rejects.toThrow('suite changed');expect(suite.write).not.toHaveBeenCalled()
 const mismatch=harness();mismatch.check.mockResolvedValue({...result,valid:false})
 await expect(savePackProposal(mismatch)).rejects.toThrow('mismatches');expect(mismatch.write).not.toHaveBeenCalled()
 const dirty=harness();dirty.guard.mockImplementation(()=>{throw new ProposalSaveBlocked('Dirty editor')})
 await expect(savePackProposal(dirty)).rejects.toThrow('Dirty editor');expect(dirty.write).not.toHaveBeenCalled()
})
it('does not replay uncertain writes or trust an echoed successful write',async()=>{
 const uncertain=harness();uncertain.write.mockRejectedValue(Error('connection lost'))
 await expect(savePackProposal(uncertain)).rejects.toThrow('connection lost');expect(uncertain.write).toHaveBeenCalledOnce()
 const echo=harness();echo.write.mockImplementation(async()=>({...before,content:text}))
 await expect(savePackProposal(echo)).rejects.toThrow('could not be verified');expect(echo.write).toHaveBeenCalledOnce()
})
it('an already saved proposal is idempotent',async()=>{const h=harness();h.read.mockResolvedValue({...before,content:text});await savePackProposal(h);expect(h.check).not.toHaveBeenCalled();expect(h.write).not.toHaveBeenCalled()})
