import {afterEach,expect,it,vi} from 'vitest'
import {cleanup,fireEvent,render,screen,waitFor} from '@testing-library/react'
import {deskFetch} from '../files/client'
import {RunSigningNotice,signingFailureMessage,VerifySavedRecord} from './RunSigning'
import type {Run} from './client'
vi.mock('../files/client',async original=>({...await original<typeof import('../files/client')>(),deskFetch:vi.fn()}))
afterEach(()=>{cleanup();vi.clearAllMocks()})
it('separates the saved decision from six specific signing failures',()=>{
 const cases=[
 ["The record's signature sidecar could not be read.",'signature could not be read'],
 ["The record's signature sidecar names a key that is not the expected one.",'unexpected key'],
 ["The record's signature sidecar does not verify under the expected key.",'signature could not be verified'],
 ["The Runtime signed nothing: the key at the signing key's path is not the expected key.",'signing key changed'],
 ["The Runtime signed nothing, and the key at the signing key's path could not be read as a key.",'signing key could not be read'],
 ["The record carries no signature.",'without a signature.']]
 for(const [reason,part] of cases)expect(signingFailureMessage(reason!)).toContain(part!)
 render(<RunSigningNotice run={{state:'completed',signing:{status:'failed',reason:cases[3]![0]!}} as Run}/> )
 expect(screen.getByText('Signing failed')).toBeTruthy();fireEvent.click(screen.getByText('View details'))
 expect(screen.getByText('The decision was saved without a signature because the signing key changed.')).toBeTruthy()
})
it('verifies only on explicit request and shows the limited scope',async()=>{
 vi.mocked(deskFetch).mockResolvedValue(new Response(JSON.stringify({status:'verified',checkedAt:new Date().toISOString()})))
 render(<VerifySavedRecord runId="run_test"/>);expect(deskFetch).not.toHaveBeenCalled()
 fireEvent.click(screen.getByRole('button',{name:'Verify saved record'}))
 await waitFor(()=>expect(deskFetch).toHaveBeenCalledExactlyOnceWith('/api/audit/runs/run_test/signature',expect.objectContaining({method:'POST',signal:expect.any(AbortSignal)})))
 expect(await screen.findByText('Signature verified · Checked just now')).toBeTruthy()
 fireEvent.click(screen.getByText('View details'))
 expect(screen.getByText(/They do not establish source accuracy or policy correctness/)).toBeTruthy()
})
it('never labels an unavailable or invalid signature as verified',async()=>{
 vi.mocked(deskFetch).mockResolvedValue(new Response(JSON.stringify({status:'invalid',checkedAt:new Date().toISOString()})))
 render(<VerifySavedRecord runId="run_test"/>);fireEvent.click(screen.getByRole('button',{name:'Verify saved record'}))
 expect((await screen.findAllByText('Signature not verified')).length).toBeGreaterThan(0)
 expect(screen.queryByText('Signature verified · Checked just now')).toBeNull()
})

it('keeps a valid signature separate from missing or failed release references', async () => {
  for (const [references, text] of [
    ['verified', 'Verified against saved references'],
    ['unavailable', 'Not checked — saved references unavailable'],
    ['invalid', 'Release or inputs did not match saved references'],
    ['check-unavailable', 'The release and input checks could not be completed']
  ]) {
    vi.mocked(deskFetch).mockResolvedValue(Response.json({ status: 'verified', references, checkedAt: new Date().toISOString() }))
    const ui = render(<VerifySavedRecord runId="run_test" />)
    fireEvent.click(screen.getByRole('button', { name: 'Verify saved record' }))
    await screen.findByText(references === 'invalid' || references === 'check-unavailable' ? 'Record checks did not pass' : 'Signature verified · Checked just now')
    fireEvent.click(screen.getByText('View details'))
    expect(screen.getByText(text!)).toBeTruthy()
    expect(screen.getByText('Verified')).toBeTruthy()
    expect(screen.getByText('Not re-run')).toBeTruthy()
    ui.unmount()
  }
})

it('cancels an old verification and clears results when another run opens', async () => {
  let finish!: (response: Response) => void
  vi.mocked(deskFetch).mockImplementation(() => new Promise(resolve => { finish = resolve }))
  const ui = render(<VerifySavedRecord runId="run_first" />)
  fireEvent.click(screen.getByRole('button', { name: 'Verify saved record' }))
  const options = vi.mocked(deskFetch).mock.calls[0]![1]!
  ui.rerender(<VerifySavedRecord runId="run_second" />)
  expect(options.signal!.aborted).toBe(true)
  finish(Response.json({ status: 'verified', references: 'verified', checkedAt: new Date().toISOString() }))
  await waitFor(() => expect(screen.getByRole('button', { name: 'Verify saved record' }).hasAttribute('disabled')).toBe(false))
  expect(screen.queryByText('Signature verified · Checked just now')).toBeNull()
})


it('does not confuse a failed request with missing historical references', async () => {
  vi.mocked(deskFetch).mockRejectedValue(new Error('Connection lost'))
  render(<VerifySavedRecord runId="run_test" />)
  fireEvent.click(screen.getByRole('button', { name: 'Verify saved record' }))
  await screen.findAllByText('The signature could not be checked.')
  fireEvent.click(screen.getByText('View details'))
  expect(screen.getByText('The release and input checks could not be completed')).toBeTruthy()
  expect(screen.queryByText('Not checked — saved references unavailable')).toBeNull()
})
