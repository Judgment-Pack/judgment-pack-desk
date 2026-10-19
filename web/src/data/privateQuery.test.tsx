import {act,cleanup,renderHook} from '@testing-library/react'
import {QueryClient,QueryClientProvider,useQuery,focusManager} from '@tanstack/react-query'
import {afterEach,expect,it,vi} from 'vitest'
import {privateQuery} from './privateQuery'
afterEach(()=>{cleanup();focusManager.setFocused(undefined);vi.useRealTimers()})
it('recovers an initially failed read, retains data during failures, and refreshes on focus',async()=>{
 vi.useFakeTimers()
 const client=new QueryClient({defaultOptions:{queries:{staleTime:Infinity,retry:false,refetchOnWindowFocus:false}}})
 const read=vi.fn().mockRejectedValue(Error('busy'))
 const {result}=renderHook(()=>useQuery({...privateQuery,queryKey:['private-snapshot'],queryFn:read}),{wrapper:({children})=><QueryClientProvider client={client}>{children}</QueryClientProvider>})
 await act(async()=>{await vi.advanceTimersByTimeAsync(10)})
 expect(result.current.isError).toBe(true)
 read.mockResolvedValue({items:['saved']})
 await act(async()=>{await vi.advanceTimersByTimeAsync(privateQuery.refetchInterval)})
 expect(result.current.data).toEqual({items:['saved']})
 read.mockRejectedValue(Error('busy again'))
 await act(async()=>{await vi.advanceTimersByTimeAsync(privateQuery.refetchInterval)})
 expect(result.current.isError).toBe(true)
 expect(result.current.data).toEqual({items:['saved']})
 read.mockResolvedValue({items:['saved','new']})
 await act(async()=>{focusManager.setFocused(false);focusManager.setFocused(true);await vi.advanceTimersByTimeAsync(10)})
 expect(result.current.data).toEqual({items:['saved','new']})
 expect(result.current.isError).toBe(false)
})
it('does not poll while the window is hidden',async()=>{
 vi.useFakeTimers();focusManager.setFocused(false)
 const client=new QueryClient(),read=vi.fn().mockResolvedValue({items:[]})
 renderHook(()=>useQuery({...privateQuery,queryKey:['hidden'],queryFn:read}),{wrapper:({children})=><QueryClientProvider client={client}>{children}</QueryClientProvider>})
 await act(async()=>{await vi.advanceTimersByTimeAsync(30_000)})
 expect(read).toHaveBeenCalledTimes(1)
})
