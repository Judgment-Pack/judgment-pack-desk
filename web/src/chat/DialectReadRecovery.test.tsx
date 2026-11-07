import {afterEach,describe,it,expect,vi} from 'vitest'
import {cleanup,render,screen,fireEvent} from '@testing-library/react'
import {DialectReadRecovery} from './ResponseRecovery'
import type {DialectReadRecoveryView} from '../assistant/serverRuns/types'

const retained:DialectReadRecoveryView={runId:'arun_ffffffffffffffffffffffffffffffff',runRevision:'9007199254740993',recoveryRevision:'2',reads:[{operationId:'old-read',originalOutcome:'interrupted',backingState:'present-valid',ordinal:'1',storageState:'unresolved',canInspect:true}]}
afterEach(cleanup)
describe('retained read recovery display',()=>{
 it('keeps found bytes separate from original completion and inspects only on explicit action',()=>{
  const inspect=vi.fn(),retry=vi.fn()
  render(<DialectReadRecovery view={retained} failed={false} busy={false} disabled={false} onInspect={inspect} onRefresh={retry}/>)
  expect(screen.getByText('The retained snapshot was found. The earlier read outcome is unchanged.')).toBeTruthy()
  expect(inspect).not.toHaveBeenCalled();expect(retry).not.toHaveBeenCalled()
  fireEvent.click(screen.getByRole('button',{name:'Inspect retained read'}))
  expect(inspect).toHaveBeenCalledTimes(1);expect(inspect).toHaveBeenCalledWith('old-read')
  expect(retry).not.toHaveBeenCalled()
 })
 it('preserves cached rows after unavailable status and offers only saved-data reload',()=>{
  const refresh=vi.fn()
  render(<DialectReadRecovery view={retained} failed busy={false} disabled={false} onRefresh={refresh}/>)
  expect(screen.getByText('The retained snapshot was found. The earlier read outcome is unchanged.')).toBeTruthy()
  expect(screen.getByText('Saved read status is unavailable. Retained work is unchanged.')).toBeTruthy()
  fireEvent.click(screen.getByRole('button',{name:'Reload saved read status'}))
  expect(refresh).toHaveBeenCalledOnce()
 })
 it('shows missing and corruption distinctly and withholds unavailable storage admission',()=>{
  const view:DialectReadRecoveryView={...retained,reads:[{...retained.reads[0],operationId:'missing',backingState:'absent',canInspect:false},{...retained.reads[0],operationId:'corrupt',backingState:'corrupt',canInspect:false},{...retained.reads[0],operationId:'unseen',backingState:'not-observed',reason:'budget-unresolved',ordinal:'0',canInspect:false}]}
  render(<DialectReadRecovery view={view} failed={false} busy={false} disabled={false} onInspect={vi.fn()}/>)
  expect(screen.getByText('The retained snapshot is missing. The earlier read was not restarted.')).toBeTruthy()
  expect(screen.getByText('The retained snapshot could not be verified. The earlier read was not restarted.')).toBeTruthy()
  expect(screen.queryByRole('button',{name:'Inspect retained read'})).toBeNull()
 })
})
