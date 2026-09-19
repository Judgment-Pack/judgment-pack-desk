import { MemoryRouter } from 'react-router-dom'
import { useRef, useState } from 'react'
import { QueryClientProvider } from '@tanstack/react-query'
import { cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react'
import { afterEach, expect, it, vi } from 'vitest'
import { DeskConfigFixture } from '../config/DeskConfigProvider'
import { effectiveConfig } from '../config/deskConfig'
import { testQueryClient } from '../testing/harness'
import { PersonalConnections } from './PersonalConnections'

vi.mock('../files/client', async original => ({ ...await original<typeof import('../files/client')>(), deskFetch: async (url: string) => Response.json({
  version: 1, state: url.includes('/gmail/') ? 'connected' : 'setup-required', account: { id: 'synthetic', email: 'person@example.test' }
}) }))
afterEach(cleanup)
function Harness() {
  const [open, setOpen] = useState(true), opener = useRef<HTMLButtonElement>(null)
  return <><button ref={opener}>Account</button><PersonalConnections open={open} onOpenChange={setOpen} openerRef={opener} /></>
}
it('closes one connection layer at a time and restores the correct opener', async () => {
  const config = effectiveConfig(undefined)
  config.desk = { present: false, path: '/synthetic/desk.json', problems: [], localGateway: { status: 'ready', gateway: { url: 'http://127.0.0.1:8888', authority: 'gateway:desk-local', signer: { algorithm: 'ed25519', public: 'ab'.repeat(32) } } } }
  render(<MemoryRouter><QueryClientProvider client={testQueryClient()}><DeskConfigFixture value={config}><Harness /></DeskConfigFixture></QueryClientProvider></MemoryRouter>)
  const manage = await screen.findByRole('button', { name: 'Manage' })
  for (let attempt = 0; attempt < 3; attempt++) {
    fireEvent.click(manage)
    await screen.findByRole('dialog', { name: 'Gmail' })
    fireEvent.keyDown(document, { key: 'Escape' })
    await waitFor(() => expect(screen.queryByRole('dialog', { name: 'Gmail' })).toBeNull())
    expect(screen.getByRole('dialog', { name: 'My connections' })).toBeTruthy()
    await waitFor(() => expect(document.activeElement).toBe(manage))
  }
  fireEvent.click(screen.getByRole('button', { name: 'Done' }))
  await waitFor(() => expect(document.activeElement).toBe(screen.getByRole('button', { name: 'Account' })))
})
