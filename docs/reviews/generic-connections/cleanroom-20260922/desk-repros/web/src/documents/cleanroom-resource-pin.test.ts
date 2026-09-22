import { afterEach, expect, it, vi } from 'vitest'
import { verifyDocument } from './client'
import { signedResource } from './__fixtures__/signedResource'

afterEach(()=>vi.restoreAllMocks())

it('reopens the retained generic resource without provider/catalog access',async()=>{
 const {object,pin}=await signedResource()
 const initial=await verifyDocument(object,pin)
 const fetch=vi.spyOn(globalThis,'fetch').mockRejectedValue(new Error('Network must stay unused'))
 const retained=JSON.parse(JSON.stringify(object))
 expect((await verifyDocument(retained,{...pin,url:'http://127.0.0.1:1'},initial.digest)).record.content.pages[0]?.text).toContain('Fixture policy.')
 expect(fetch).not.toHaveBeenCalled()
})

it.each(['authority','public-key'] as const)('refuses retained generic resource when current %s changes',async which=>{
 const {object,pin}=await signedResource()
 const initial=await verifyDocument(object,pin)
 const current=structuredClone(pin)
 if(which==='authority') current.authority='gateway:new-current-pin'
 else current.signer.public='00'.repeat(32)
 await expect(verifyDocument(JSON.parse(JSON.stringify(object)),current,initial.digest)).rejects.toThrow('Document verification failed')
})
