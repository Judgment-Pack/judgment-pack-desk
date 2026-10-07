import { QueryClientProvider } from '@tanstack/react-query'
import { act, cleanup, renderHook, waitFor } from '@testing-library/react'
import type { ReactNode } from 'react'
import { afterEach, expect, it, vi } from 'vitest'
import { testQueryClient } from '../testing/harness'
import { useProviderModels } from './providers'

const models={models:[{id:'available',name:'Available',efforts:['low','high'],defaultEffort:'low'}]}
const busy=()=>Response.json({error:'provider-busy'},{status:409})
afterEach(()=>{cleanup();vi.unstubAllGlobals()})
function setup(answer:()=>Promise<Response>|Response,cached=false){
 const client=testQueryClient()
 if(cached)client.setQueryData(['model-provider','openai','models'],models)
 const fetcher=vi.fn(async(url:string,init?:RequestInit)=>{
  expect(new URL(url,'http://localhost').pathname).toBe('/api/model-providers/openai/models')
  expect(init?.method).toBe('GET')
  return answer()
 })
 vi.stubGlobal('fetch',fetcher)
 const wrapper=({children}:{children:ReactNode})=><QueryClientProvider client={client}>{children}</QueryClientProvider>
 return {wrapper,fetcher,...renderHook(()=>useProviderModels(),{wrapper})}
}
it('keeps first discovery loading and automatically recovers from busy without a refresh',async()=>{
 let attempts=0
 const {result,fetcher}=setup(()=>++attempts<3?busy():Response.json(models))
 await waitFor(()=>expect(result.current.failureCount).toBe(1))
 expect(result.current.isPending).toBe(true)
 expect(result.current.isError).toBe(false)
 await waitFor(()=>expect(result.current.data).toEqual(models),{timeout:3500})
 expect(result.current.isError).toBe(false)
 expect(fetcher).toHaveBeenCalledTimes(3)
})
it('keeps the verified model list usable during a busy background refresh',async()=>{
 let attempts=0
 const next={models:[{...models.models[0],id:'updated'}]}
 const {result}=setup(()=>++attempts===1?busy():Response.json(next),true)
 act(()=>{void result.current.refetch()})
 await waitFor(()=>expect(result.current.failureCount).toBe(1))
 expect(result.current.isSuccess).toBe(true)
 expect(result.current.isError).toBe(false)
 expect(result.current.data).toEqual(models)
 await waitFor(()=>expect(result.current.data).toEqual(next))
})
it.each(['sign-in-required','provider-unavailable'])('does not retry or conceal a %s refusal',async error=>{
 const {result,fetcher}=setup(()=>Response.json({error},{status:409}))
 await waitFor(()=>expect(result.current.isError).toBe(true))
 expect(result.current.error).toMatchObject({code:error})
 await new Promise(resolve=>setTimeout(resolve,650))
 expect(fetcher).toHaveBeenCalledOnce()
})
it('coalesces model readers and cancels busy retries when the last reader leaves',async()=>{
 const first=setup(busy)
 const second=renderHook(()=>useProviderModels(),{wrapper:first.wrapper})
 await waitFor(()=>expect(first.result.current.failureCount).toBe(1))
 expect(first.fetcher).toHaveBeenCalledOnce()
 first.unmount();second.unmount()
 await new Promise(resolve=>setTimeout(resolve,650))
 expect(first.fetcher).toHaveBeenCalledOnce()
})
