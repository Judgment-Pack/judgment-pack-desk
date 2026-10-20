import {render,screen,fireEvent,cleanup} from '@testing-library/react'
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
it('shows what the response has done so far under the tasks, and by itself where it is paused with none',()=>{
 cleanup()
 const report={requests:6,unit:'model-requests' as const,tokens:{inputTokens:900,outputTokens:60},timing:{omitted:0,steps:[{model:'a-model',requestBytes:1024,firstByteMs:400,durationMs:2000,outcome:'complete' as const,status:200}]}}
 const planned={status:'running',used:6,limit:20,plan:[{step:'Inspect rules',status:'in_progress'}],explanation:''} as ExecutionCheckpoint
 const view=render(<ExecutionProgress execution={planned} report={report}/>)
 fireEvent.click(screen.getByText('Tasks · 0 of 1 complete'))
 expect(screen.getByText(/Model requests: 6 · tokens reported by the provider: in 900, out 60/)).toBeTruthy()
 expect(screen.getByText(/Requests took 2\.0 s in all · first byte 0\.4 s on average/)).toBeTruthy()
 view.unmount()
 render(<ExecutionProgress execution={{status:'paused',used:20,limit:20,plan:[],explanation:''} as unknown as ExecutionCheckpoint} report={report}/>)
 expect(screen.getByText('Paused · Request limit reached')).toBeTruthy()
 expect(screen.getByRole('region',{name:'Run report'})).toBeTruthy()
})
