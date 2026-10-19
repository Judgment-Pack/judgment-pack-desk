import {expect,it} from 'vitest'
import {previousSourceObservation} from './PolicyConnectedSource'
import type {PolicySourceObservation} from './client'
it('selects a retained baseline within the exact binding/account/resource despite repeated provider revision labels',()=>{
 const first={id:'first',bindingId:'binding-one',sourceRevision:'7',source:{provider:'google-drive',accountId:'account-one',resourceId:'resource-one'}} as PolicySourceObservation
 const anotherResource={...first,id:'other-resource',source:{...first.source,resourceId:'resource-two'}}
 const anotherBinding={...first,id:'other-binding',bindingId:'binding-two'}
 const anotherAccount={...first,id:'other-account',source:{...first.source,accountId:'account-two'}}
 const current={...first,id:'current'}
 expect(previousSourceObservation([first,anotherResource,anotherBinding,anotherAccount,current],current)?.id).toBe('first')
 expect(previousSourceObservation([anotherResource,anotherBinding,anotherAccount,current],current)).toBeUndefined()
})
