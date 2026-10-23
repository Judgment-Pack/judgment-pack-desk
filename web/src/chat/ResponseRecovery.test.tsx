import {act,cleanup,fireEvent,render,screen} from '@testing-library/react'
import {afterEach,expect,it,vi} from 'vitest'
import {ResponseRecovery} from './ResponseRecovery'
import {failureError,failureRecovery,normalizeFailure} from '@judgment-pack/harness/failures'
import {checkpoint,decodeCheckpoint} from './checkpoint'
import {INITIAL_STATE} from '../research/run'
afterEach(cleanup)
it('keeps the failure next to an explicit, guarded recovery action',()=>{
 const retry=vi.fn(),detail='Codex could not recover the connection after automatic retries.'
 const view=render(<ResponseRecovery detail={detail} disabled onRetry={retry}/>)
 expect(screen.getByRole('alert').textContent).toBe(detail)
 fireEvent.click(screen.getByRole('button',{name:'Retry response'}));expect(retry).not.toHaveBeenCalled()
 view.rerender(<ResponseRecovery detail={detail} disabled={false} onRetry={retry}/>)
 fireEvent.click(screen.getByRole('button',{name:'Retry response'}));expect(retry).toHaveBeenCalledOnce()
 expect(screen.getByText(/Completed steps and partial responses are kept/)).toBeTruthy()
})

it('renders safe diagnostics and waits for Retry-After before enabling explicit retry',()=>{
 vi.useFakeTimers();vi.setSystemTime(new Date('2026-10-14T12:00:00Z'))
 try{
  const retry=vi.fn()
  render(<ResponseRecovery detail="unsafe provider body" failure={{version:1,code:'rate-limited',source:'model',outcome:'refused',status:429,requestId:'req_fixture',retryAt:'2026-10-14T12:00:02Z'}} disabled={false} onRetry={retry}/>)
  expect(screen.getByRole('alert').textContent).toContain('limiting requests')
  expect(screen.queryByText('unsafe provider body')).toBeNull()
  fireEvent.click(screen.getByRole('button',{name:'Retry response'}));expect(retry).not.toHaveBeenCalled()
  act(()=>vi.advanceTimersByTime(2010))
  fireEvent.click(screen.getByRole('button',{name:'Retry response'}));expect(retry).toHaveBeenCalledOnce()
 }finally{vi.useRealTimers()}
})

it('offers one explicit retry with its added allowance after the last request failed',()=>{
 const retry=vi.fn()
 render(<ResponseRecovery detail="Stopped" disabled={false} additionalRequests={10} onRetry={retry}/>)
 expect(screen.queryByRole('button',{name:'Retry response'})).toBeNull()
 expect(screen.getByText('The request allowance is used up. Retrying adds 10 requests.')).toBeTruthy()
 expect(retry).not.toHaveBeenCalled()
 fireEvent.click(screen.getByRole('button',{name:'Retry · +10 requests'}))
 expect(retry).toHaveBeenCalledOnce()
})

it('explains a repeated-work stop separately from an invalid model response',()=>{
 render(<ResponseRecovery detail="Unused" failure={{version:1,code:'repeated-work',source:'desk',outcome:'unknown'}} disabled={false} onRetry={()=>{}}/>)
 expect(screen.getByRole('article',{name:'Stopped repeating work'})).toBeTruthy()
 expect(screen.getByRole('alert').textContent).toContain('kept repeating completed work')
 expect(screen.getByText('Review the completed and refused steps before retrying, or send a different instruction.')).toBeTruthy()
 expect(screen.queryByText('Response interrupted')).toBeNull()
})

it('retains the released project pace refusal through checkpoint and safe recovery rendering',()=>{
 const failure=normalizeFailure(failureError('pace exceeded','desk','not-started'))
 expect(failureRecovery(failure)).toBe('wait')
 const saved=decodeCheckpoint(JSON.parse(JSON.stringify(checkpoint({...INITIAL_STATE,failure},[]))))
 expect(saved.state.failure).toEqual(failure)
 const retry=vi.fn()
 render(<ResponseRecovery detail="unsafe provider body" failure={saved.state.failure} disabled={false} onRetry={retry}/>)
 expect(screen.getByRole('alert').textContent).toBe('The project period budget is exhausted. Wait for the next period or change the project policy.')
 expect(screen.queryByText('unsafe provider body')).toBeNull()
 expect(retry).not.toHaveBeenCalled()
})
