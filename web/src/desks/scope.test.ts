import { afterEach, beforeEach, expect, it, vi } from 'vitest'
// The test ground has already loaded this module once, to wire the assistant harness (testing/setup.ts); each case reads it afresh.
beforeEach(()=>vi.resetModules())
afterEach(()=>{sessionStorage.clear();vi.resetModules()})
it('freezes the desk for HTTP, sockets, and events until reload',async()=>{
 const id='a'.repeat(32);sessionStorage.setItem('jpack.active-desk',id)
 const scope=await import('./scope');sessionStorage.setItem('jpack.active-desk','b'.repeat(32))
 expect(scope.deskHeaders()).toEqual({'X-Jpack-Desk':id})
 expect(scope.deskSocketURL('ws://localhost/ws')).toBe(`ws://localhost/ws?desk=${id}`)
 expect(scope.deskEventPath('trigger')).toBe(`/api/desks/${id}/job-events/trigger`)
})
it('does not forward invalid stored identifiers',async()=>{
 sessionStorage.setItem('jpack.active-desk','../outside');const scope=await import('./scope')
 expect(scope.deskHeaders()).toEqual({});expect(scope.deskSocketURL('ws://localhost/ws')).toBe('ws://localhost/ws')
})
it('distinguishes a fresh visit from an explicit owner selection without changing the live document scope',async()=>{
 const scope=await import('./scope')
 expect(scope.readDeskSelection()).toBeUndefined()
 scope.selectDesk('')
 expect(scope.readDeskSelection()).toBe('')
 scope.selectDesk('c'.repeat(32))
 expect(scope.readDeskSelection()).toBe('c'.repeat(32))
 expect(scope.activeDeskId).toBe('')
 expect(()=>scope.selectDesk('../bad')).toThrow('Invalid desk')
})
