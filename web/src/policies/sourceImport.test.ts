import {expect,it} from 'vitest'
import {InvalidPolicySource,preparePolicyImport,POLICY_SOURCE_BYTE_LIMIT} from './sourceImport'
it('keeps exact UTF-8 original bytes separately from editable text',async()=>{
 const raw='\ufeff# Access\r\nCafé 中文\r\n'
 const result=await preparePolicyImport(new File([raw],'Access.md',{type:'text/markdown'}))
 expect(result.text).toBe(raw)
 expect(Array.from(atob(result.source.data),character=>character.charCodeAt(0))).toEqual(Array.from(new TextEncoder().encode(raw)))
 expect(result.source).toMatchObject({name:'Access.md',mediaType:'text/markdown'})
})
it.each([
 ['policy.pdf','text/plain',new Uint8Array([65])],
 ['policy.txt','application/octet-stream',new Uint8Array([65])],
 ['../policy.md','text/markdown',new Uint8Array([65])],
 ['policy\n.md','text/markdown',new Uint8Array([65])],
 ['policy.md','text/markdown',new Uint8Array([0xff])],
 ['policy.txt','text/plain',new Uint8Array([0])],
 ['policy.txt','text/plain',new Uint8Array(POLICY_SOURCE_BYTE_LIMIT+1)],
])('refuses unsupported, pathlike, binary or oversized %s',async(name,type,raw)=>{
 await expect(preparePolicyImport(new File([raw],name,{type}))).rejects.toBeInstanceOf(InvalidPolicySource)
})
it('infers a supported media type only for an untyped local text file',async()=>{
 expect((await preparePolicyImport(new File(['plain'],'policy.txt'))).source.mediaType).toBe('text/plain')
 expect((await preparePolicyImport(new File(['markdown'],'policy.MD'))).source.mediaType).toBe('text/markdown')
})
