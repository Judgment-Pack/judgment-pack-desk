import { afterEach, expect, it, vi } from 'vitest'
import { authorizeDrive, connectionCall } from './client'
const fetch = vi.hoisted(() => vi.fn())
vi.mock('../files/client', async original => ({...await original<typeof import('../files/client')>(), deskFetch: fetch}))
afterEach(() => {vi.restoreAllMocks();vi.clearAllMocks();vi.useRealTimers()})
it('never navigates the authorization tab to a provider-supplied foreign URL', async () => {
 const tab = {opener: {},location: {href: 'about:blank'},close: vi.fn()}
 vi.spyOn(window,'open').mockReturnValue(tab as unknown as Window)
 fetch.mockResolvedValue(Response.json({id:'aa'.repeat(32),state:'pending',url:'https://evil.example/steal'}))
 await expect(authorizeDrive('pick', new AbortController().signal)).rejects.toThrow()
 expect(tab.location.href).toBe('about:blank');expect(tab.opener).toBeNull();expect(tab.close).toHaveBeenCalled()
})
it('reports popup blocking without starting an authorization', async () => {
 vi.spyOn(window,'open').mockReturnValue(null)
 await expect(authorizeDrive('pick', new AbortController().signal)).rejects.toThrow('Allow pop-ups')
 expect(fetch).not.toHaveBeenCalled()
})
it('maps gateway refusal codes to UI text instead of rendering raw provider errors',async () => {
 fetch.mockResolvedValue(Response.json({error:'wrong-account'}))
 await expect(connectionCall('pick')).rejects.toThrow('Choose the Google account')
})
it('routes Gmail controls to their provider namespace',async () => {
 fetch.mockResolvedValue(Response.json({messages:[]}))
 await connectionCall('search',{query:'subject:policy'},undefined,'gmail')
 expect(fetch).toHaveBeenCalledWith('/api/connections/gmail/search',expect.objectContaining({body:JSON.stringify({query:'subject:policy'})}))
})

// Protocol classifications survive localization so the UI can offer recovery.
it.each(['wrong-account','blocked-by-policy','authorization-in-progress','too-many-selections','file-too-large','source-changed','source-incomplete','registration-expired'])('gives actionable Notion copy for %s', async code => {
 const {connectionError}=await import('./client')
 expect(connectionError(code,'notion')).not.toContain('Try again.')
})
it('preserves reconnect classification independently from display text',async()=>{
 const {connectionFailure}=await import('./client')
 const error=connectionFailure(new Error('adapter failed: reconnect-required'),'notion')
 expect(error.reconnectRequired).toBe(true);expect(error.code).toBe('reconnect-required')
})

it('uses the declared endpoint for a new provider without a named authorization handler', async () => {
 vi.useFakeTimers()
 const tab={opener:{},location:{href:'about:blank'},close:vi.fn()}
 vi.spyOn(window,'open').mockReturnValue(tab as unknown as Window)
 fetch.mockResolvedValueOnce(Response.json({id:'aa'.repeat(32),state:'pending',url:'https://accounts.example.com/authorize?state=fixture'})).mockResolvedValueOnce(Response.json({state:'complete'}))
 const result=authorizeDrive('connect',new AbortController().signal,'fixture-files',['https://accounts.example.com/authorize'])
 await vi.advanceTimersByTimeAsync(1100)
 await expect(result).resolves.toEqual([])
 expect(tab.location.href).toBe('https://accounts.example.com/authorize?state=fixture')
 expect(tab.opener).toBeNull();expect(tab.close).toHaveBeenCalled()
})
it.each(['https://accounts.example.com/wrong','https://other.example.com/authorize','https://user:secret@accounts.example.com/authorize','https://accounts.example.com/authorize#token'])('refuses undeclared generic sign-in URL %s', async url=>{
 const tab={opener:{},location:{href:'about:blank'},close:vi.fn()}
 vi.spyOn(window,'open').mockReturnValue(tab as unknown as Window)
 fetch.mockResolvedValue(Response.json({id:'aa'.repeat(32),state:'pending',url}))
 await expect(authorizeDrive('connect',new AbortController().signal,'fixture-files',['https://accounts.example.com/authorize'])).rejects.toThrow()
 expect(tab.location.href).toBe('about:blank')
})
