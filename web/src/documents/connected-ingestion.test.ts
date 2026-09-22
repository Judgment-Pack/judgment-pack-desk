import { expect, it, vi } from 'vitest'
import { ingestSource } from './client'
import { signedDocument } from './__fixtures__/signedDocument'
import { signedConnected } from './__fixtures__/signedConnected'
const mocks=vi.hoisted(()=>({acquire:vi.fn(),fetch:vi.fn(),seal:vi.fn(),registry:vi.fn()}))
vi.mock('../research/gatewayClient',async original=>({...await original<typeof import('../research/gatewayClient')>(),acquire:mocks.acquire,seal:mocks.seal,registry:mocks.registry}))
vi.mock('../files/client',async original=>({...await original<typeof import('../files/client')>(),deskFetch:mocks.fetch}))
it('refuses the wrong source kind before saving any acquired attachment',async()=>{
 const {object}=await signedDocument('gmail'),{pin}=await signedConnected('notion')
 mocks.acquire.mockResolvedValue({text:object.proof!.response})
 mocks.fetch.mockResolvedValue(new Response(JSON.stringify({sha256:'saved'}),{status:200}))
 mocks.registry.mockResolvedValue('')
 await expect(ingestSource({resourceId:'11111111222233334444555555555555',grant:'aa'.repeat(32)},'notion',{gateway:pin,documents:{enabled:true,maxFileBytes:4<<20}} as never,new AbortController().signal,()=>{})).rejects.toThrow()
 expect(mocks.fetch).not.toHaveBeenCalled();expect(mocks.seal).not.toHaveBeenCalled()
})
