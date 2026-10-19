import {renderHook,waitFor} from '@testing-library/react'
import {expect,it,vi} from 'vitest'
import {useAutomaticAction} from './automatic'
import type {ActionGrant} from './client'
const grant:ActionGrant={id:'one',action:'pack-update',chatId:'chat',path:'pack.json',revision:'base',name:'pack',state:'ready',expiresAt:new Date(Date.now()+60000).toISOString()}
it('starts only the authorized resource once, regardless of ordinary query updates',async()=>{
 const execute=vi.fn(async()=>{}),onError=vi.fn()
 const props={grant,action:'pack-update',path:'pack.json',revision:'base',fingerprint:'candidate',ready:false,execute,onError}
 const h=renderHook(p=>useAutomaticAction(p),{initialProps:props})
 expect(execute).not.toHaveBeenCalled()
 h.rerender({...props,ready:true,action:'research'});expect(execute).not.toHaveBeenCalled()
 h.rerender({...props,ready:true,revision:'newer'});expect(execute).not.toHaveBeenCalled()
 h.rerender({...props,ready:true});await waitFor(()=>expect(execute).toHaveBeenCalledTimes(1))
 h.rerender({...props,ready:true,grant:{...grant}});expect(execute).toHaveBeenCalledTimes(1)
})
it('does not retry an uncertain dispatch or use expired/revoked allowances',async()=>{
 const execute=vi.fn(async()=>{throw Error('uncertain')}),onError=vi.fn()
 const props={grant,action:'pack-update',path:'pack.json',revision:'base',fingerprint:'candidate',ready:true,execute,onError}
 const h=renderHook(p=>useAutomaticAction(p),{initialProps:{...props,grant:{...grant,state:'revoked' as const}} as typeof props})
 expect(execute).not.toHaveBeenCalled()
 h.rerender({...props,grant:{...grant,expiresAt:'2000-01-01T00:00:00Z'}});expect(execute).not.toHaveBeenCalled()
 h.rerender(props);await waitFor(()=>expect(onError).toHaveBeenCalledTimes(1))
 h.rerender({...props,ready:false});h.rerender(props);expect(execute).toHaveBeenCalledTimes(1)
})
