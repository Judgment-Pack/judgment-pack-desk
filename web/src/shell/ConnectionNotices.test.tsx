import { cleanup, fireEvent, render, screen } from '@testing-library/react'
import { MemoryRouter } from 'react-router-dom'
import { afterEach, expect, it, vi } from 'vitest'
import { McpContext, type McpConnection } from '../mcp/McpProvider'
import { connected } from '../testing/harness'
import { ConnectionStatus } from './ConnectionNotices'

afterEach(cleanup)
function renderPage(overrides: Partial<McpConnection>) {
  return render(<MemoryRouter><McpContext.Provider value={connected(overrides)}><footer><ConnectionStatus/></footer><main><input aria-label="Unfinished edit" defaultValue="Keep this"/></main></McpContext.Provider></MemoryRouter>)
}
it.each([false, true])('keeps the workspace accessible after a failed connection (previously connected: %s)', async everConnected => {
  const retry = vi.fn()
  renderPage({status:'failed',error:new Error('connection refused'),everConnected,retryNow:retry})
  expect(screen.getByRole('textbox')).toHaveProperty('value', 'Keep this')
  expect(screen.queryByRole('alert')).toBeNull()
  const trigger = screen.getByRole('button',{name:'Connection status: not connected'})
  expect(trigger.closest('footer')).toBeTruthy()
  fireEvent.click(trigger)
  expect((await screen.findByRole('dialog')).textContent).toContain('Desk is not connected')
  fireEvent.click(screen.getByRole('button',{name:'Retry connection'}))
  expect(retry).toHaveBeenCalledOnce()
})
it('puts retry details in the status strip without broadcasting attempts', async () => {
  const retry=vi.fn()
  renderPage({status:'reconnecting',error:new Error('closed'),everConnected:true,attempt:3,retryNow:retry})
  expect(screen.getByRole('status').textContent).toBe('reconnecting')
  expect(screen.queryByText(/Connection attempts/)).toBeNull()
  fireEvent.click(screen.getByRole('button',{name:'Connection status: reconnecting'}))
  const dialog=await screen.findByRole('dialog',{name:'Connection status'})
  expect(dialog.textContent).toContain('Reconnecting to Desk')
  expect(dialog.querySelector('details')?.open).toBe(false)
  fireEvent.click(screen.getByRole('button',{name:'Retry connection'}))
  expect(retry).toHaveBeenCalledOnce()
})
it('keeps missing capabilities distinct from a healthy connection', async () => {
  renderPage({status:'ready',known:false,capabilitiesError:new Error('the request timed out')})
  fireEvent.click(screen.getByRole('button',{name:'Connection status: Limited connection'}))
  expect((await screen.findByRole('dialog')).textContent).toContain('Some features are unavailable')
  expect(screen.getByText('the request timed out')).toBeTruthy()
})
it('offers retry even when there is no diagnostic reason', () => {
  renderPage({status:'failed',error:null,everConnected:false})
  fireEvent.click(screen.getByRole('button',{name:'Connection status: not connected'}))
  expect(screen.getByRole('button',{name:'Retry connection'})).toBeTruthy()
})
