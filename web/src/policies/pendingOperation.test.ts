import {afterEach,expect,it,vi} from 'vitest'
import {giveThisPageASessionForTesting} from '../mcp/session'
import {createPolicyPendingJournal} from './pendingOperation'

const scope=vi.hoisted(()=>({desk:'a'.repeat(32)}))
vi.mock('../desks/scope',()=>({get activeDeskId(){return scope.desk}}))
afterEach(()=>{sessionStorage.clear();scope.desk='a'.repeat(32)})
it('isolates same-policy journals by current Desk, session and operation family',async()=>{
 const first=await createPolicyPendingJournal('lifecycle','same-policy')
 first.store({method:'POST',path:'/same-policy/lifecycle',body:{operationId:'original-operation',action:'withdraw'}})
 expect((await createPolicyPendingJournal('lifecycle','same-policy')).read()!.body.operationId).toBe('original-operation')
 scope.desk='b'.repeat(32)
 expect((await createPolicyPendingJournal('lifecycle','same-policy')).read()).toBeUndefined()
 scope.desk='a'.repeat(32)
 expect((await createPolicyPendingJournal('source-sync','same-policy')).read()).toBeUndefined()
 expect((await createPolicyPendingJournal('source-status','same-policy')).read()).toBeUndefined()
 expect((await createPolicyPendingJournal('coverage','same-policy')).read()).toBeUndefined()
 giveThisPageASessionForTesting('a-new-administrator-session')
 const otherIdentity=await createPolicyPendingJournal('lifecycle','same-policy')
 expect(otherIdentity.read()).toBeUndefined()
 expect(otherIdentity.key).not.toContain('a-new-administrator-session')
 // An in-flight journal keeps the original frozen scope.
 expect(first.read()!.body.operationId).toBe('original-operation')
})
it('recovers coverage commands only in the current fixed Policy collection scope',async()=>{
 const journal=await createPolicyPendingJournal('coverage','same-policy'),operation={method:'POST' as const,path:'/same-policy/coverage',body:{action:'save',operationId:'coverage-operation'}}
 journal.store(operation);expect((await createPolicyPendingJournal('coverage','same-policy')).read()).toEqual(operation)
 expect(()=>journal.store({...operation,path:'/same-policy/coverage/other/check'})).toThrow()
 sessionStorage.setItem(journal.key,JSON.stringify({...operation,path:'/other-policy/coverage'}));expect(journal.read()).toBeUndefined()
})
it('does not import an unscoped legacy journal or a different endpoint',async()=>{
 sessionStorage.setItem('jpack.policy-source.pending.v1:'+window.location.host+':same-policy',JSON.stringify({method:'POST',path:'/same-policy/source-sync',body:{operationId:'unscoped'}}))
 const journal=await createPolicyPendingJournal('source-sync','same-policy')
 expect(journal.read()).toBeUndefined()
 sessionStorage.setItem(journal.key,JSON.stringify({method:'POST',path:'/other-policy/source-sync',body:{operationId:'wrong-policy'}}))
 expect(journal.read()).toBeUndefined()
})
