import { afterEach, expect, it, vi } from 'vitest'
import { ProviderError, providerRequest } from './providers'

afterEach(() => vi.unstubAllGlobals())

it.each([
  ['account-lookup-failed', 408, 'Desk could not reach ChatGPT to check the account. Check your internet connection and try again.'],
  ['storage-full', 507, "Disk is full. Free space on the drive holding Desk's configuration and try again."],
  ['operation-canceled', 408, 'The ChatGPT connection request was stopped. Try again when ready.'],
] as const)('presents bounded %s guidance for status, models and setup', async (code, status, message) => {
  vi.stubGlobal('fetch', vi.fn(async () => new Response(JSON.stringify({error:code, message:'PRIVATE_SENTINEL'}), {status})))
  for (const action of ['status','models','login']) {
    await expect(providerRequest(action, action === 'login' ? {method:'browser'} : undefined)).rejects.toMatchObject({code, message})
  }
  expect(new ProviderError(code).message).not.toContain('PRIVATE_SENTINEL')
})
