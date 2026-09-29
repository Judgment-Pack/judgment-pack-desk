import { afterEach, expect, it, vi } from 'vitest'
import { createElement, type ReactNode } from 'react'
import { act, cleanup, renderHook, waitFor } from '@testing-library/react'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { FileRequestError, readFile, writeFile } from '../files/client'
import { loadPresentation, savePresentation, useDecisionAppearance } from './decisionAppearanceStorage'
import { emptyPresentation, PRESENTATION_FILE } from './decisionAppearance'
vi.mock('../files/client',async original=>({...await original<typeof import('../files/client')>(),readFile:vi.fn(),writeFile:vi.fn()}))
vi.mock('../shell/useDirtyGuard',()=>({useDirtyGuard:vi.fn()}))
afterEach(()=>{cleanup();vi.resetAllMocks()})
it('reads precise absence without writing or inferring meanings',async()=>{
 vi.mocked(readFile).mockRejectedValue(new FileRequestError(404,'Missing','chassis','not-found'))
 expect(await loadPresentation()).toEqual({document:emptyPresentation(),digest:''})
 expect(writeFile).not.toHaveBeenCalled()
})
it('does not treat errors, malformed data or oversized files as absence',async()=>{
 vi.mocked(readFile).mockRejectedValueOnce(new FileRequestError(403,'Denied','chassis','forbidden'))
 await expect(loadPresentation()).rejects.toThrow()
 for(const [content,bytes] of [['broken',6],['{"version":2,"packs":{}}',25],['{"version":1,"packs":{}}',500001]] as const){
  vi.mocked(readFile).mockResolvedValueOnce({path:PRESENTATION_FILE,sha256:'old',content,bytes})
  await expect(loadPresentation()).rejects.toThrow()
 }
 expect(writeFile).not.toHaveBeenCalled()
})
it('persists ID-based settings with optimistic concurrency and preserves other packs',async()=>{
 const before={document:{version:1 as const,packs:{other:{outcomes:{one:{meaning:'review' as const,color:'amber' as const}}}}},digest:'observed'}
 vi.mocked(writeFile).mockImplementation(async input=>({path:input.path,bytes:input.content.length,sha256:'new',content:input.content}))
 const result=await savePresentation(before,'policy',{outcome:{color:'blue',meaning:'categorical'}})
 expect(result.document.packs.other).toEqual(before.document.packs.other)
 expect(result.document.packs.policy!.outcomes.outcome!.meaning).toBe('categorical')
 expect(writeFile).toHaveBeenCalledWith(expect.objectContaining({path:PRESENTATION_FILE,baseSha256:'observed'}))
 expect(vi.mocked(writeFile).mock.calls[0]![0]).not.toHaveProperty('override')
})
it('refuses stale writes and unverified readbacks',async()=>{
 const before={document:emptyPresentation(),digest:'observed'}
 vi.mocked(writeFile).mockRejectedValueOnce(new FileRequestError(409,'Conflict','chassis','stale'))
 await expect(savePresentation(before,'policy',{})).rejects.toThrow(/another window/)
 expect(writeFile).toHaveBeenCalledOnce()
 vi.mocked(writeFile).mockResolvedValueOnce({path:PRESENTATION_FILE,bytes:2,sha256:'new',content:'{}'})
 await expect(savePresentation(before,'policy',{})).rejects.toThrow(/verified/)
})

it('keeps presentation data separate from the Project files cache and invalidates its bytes after saving',async()=>{
 const client=new QueryClient({defaultOptions:{queries:{retry:false},mutations:{retry:false}}})
 const content=JSON.stringify(emptyPresentation())
 const file={path:PRESENTATION_FILE,bytes:content.length,sha256:'before',content}
 client.setQueryData(['desk-file',PRESENTATION_FILE],file)
 vi.mocked(readFile).mockResolvedValue(file)
 vi.mocked(writeFile).mockImplementation(async input=>({...file,sha256:'after',content:input.content}))
 const wrapper=({children}:{children:ReactNode})=>createElement(QueryClientProvider,{client},children)
 const {result}=renderHook(()=>useDecisionAppearance('policy'),{wrapper})
 await waitFor(()=>expect(result.current.ready).toBe(true))
 expect(result.current.saved).toBeUndefined()
 act(()=>result.current.change('refund',{color:'rose',meaning:'hold'},{}))
 await waitFor(()=>expect(result.current.saved?.refund?.meaning).toBe('hold'))
 expect(client.getQueryData(['desk-file',PRESENTATION_FILE])).toEqual(file)
 expect(client.getQueryState(['desk-file',PRESENTATION_FILE])?.isInvalidated).toBe(true)
 client.clear()
})
