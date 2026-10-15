import {act,cleanup,renderHook} from '@testing-library/react'
import {QueryClient,QueryClientProvider,useQuery} from '@tanstack/react-query'
import {afterEach,expect,it,vi} from 'vitest'
import {dialectAPI} from './client'
import {dialectListQuery,dialectQuery} from './queries'
vi.mock('./client',async original=>({...await original<typeof import('./client')>(),dialectAPI:vi.fn()}))
afterEach(()=>{cleanup();vi.useRealTimers();vi.resetAllMocks()})
it('refreshes a visible private collection and preserves rows during a failed refresh',async()=>{
 vi.useFakeTimers()
 const client=new QueryClient({defaultOptions:{queries:{staleTime:Infinity,retry:false,refetchOnWindowFocus:false}}})
 const first={items:[{id:'one',name:'Saved recipe',records:0,updatedAt:'2026-10-14'}]}
 let fail=false,value=first
 vi.mocked(dialectAPI).mockImplementation(async()=>{if(fail)throw Error('Unavailable');return value as never})
 const {result}=renderHook(()=>{const query=useQuery(dialectListQuery());return {data:query.data,isError:query.isError}},{wrapper:({children})=><QueryClientProvider client={client}>{children}</QueryClientProvider>})
 await act(async()=>{await vi.advanceTimersByTimeAsync(10)})
 expect(result.current.data).toEqual(first)
 value={items:[{...first.items[0],records:4}]}
 await act(async()=>{await vi.advanceTimersByTimeAsync(5010)})
 expect(result.current.data?.items[0].records).toBe(4)
 fail=true
 await act(async()=>{await vi.advanceTimersByTimeAsync(5010)})
 expect(result.current.isError).toBe(true)
 expect(result.current.data?.items[0].records).toBe(4)
 fail=false
 await act(async()=>{await vi.advanceTimersByTimeAsync(5010)})
 expect(result.current.isError).toBe(false)
})
it('fetches current private document content instead of accepting the infinite document cache',async()=>{
 const client=new QueryClient({defaultOptions:{queries:{staleTime:Infinity,retry:false}}})
 client.setQueryData(['input-preparations','one'],{sha256:'before',document:{records:[]}})
 vi.mocked(dialectAPI).mockResolvedValue({sha256:'after',document:{records:[{id:'saved'}]}})
 const updated=await client.fetchQuery(dialectQuery('one'))
 expect(updated.sha256).toBe('after')
 expect(vi.mocked(dialectAPI).mock.calls[0][4]).toBeInstanceOf(AbortSignal)
})
it('recovers a detail page after an initial busy read without navigation',async()=>{
 vi.useFakeTimers()
 const client=new QueryClient({defaultOptions:{queries:{staleTime:Infinity,retry:false}}})
 vi.mocked(dialectAPI).mockRejectedValueOnce(Error('Input preparations are busy.')).mockResolvedValue({sha256:'saved',document:{records:[]}})
 const {result}=renderHook(()=>useQuery(dialectQuery('one')),{wrapper:({children})=><QueryClientProvider client={client}>{children}</QueryClientProvider>})
 await act(async()=>{await vi.advanceTimersByTimeAsync(10)})
 expect(result.current.isError).toBe(true)
 await act(async()=>{await vi.advanceTimersByTimeAsync(10_000)})
 expect(result.current.data?.sha256).toBe('saved')
 expect(result.current.isError).toBe(false)
})
