import {render,screen,fireEvent} from '@testing-library/react'
import {it,expect,vi} from 'vitest'
import {ExecutionProgress} from './ExecutionProgress'
import type {ExecutionCheckpoint} from '@judgment-pack/harness/execution/state'
it('shows one compact pause with explicit continuation and preserves completed progress',()=>{
 const onContinue=vi.fn(),onStop=vi.fn()
 const execution={status:'paused',used:20,limit:20,plan:[{step:'Inspect rules',status:'completed'},{step:'Verify fix',status:'pending'}],explanation:''} as ExecutionCheckpoint
 render(<ExecutionProgress execution={execution} onContinue={onContinue} onStop={onStop}/>)
 expect(screen.getByText('Paused · Request limit reached')).toBeTruthy()
 expect(screen.getByText('Tasks · 1 of 2 complete')).toBeTruthy()
 fireEvent.click(screen.getByRole('button',{name:'Continue · +10 requests'}))
 expect(onContinue).toHaveBeenCalledOnce();expect(onStop).not.toHaveBeenCalled()
})
