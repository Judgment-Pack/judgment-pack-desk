import { cleanup, fireEvent, render, screen } from '@testing-library/react'
import { MemoryRouter } from 'react-router-dom'
import { afterEach, expect, it, vi } from 'vitest'
import { McpContext, type McpConnection } from '../mcp/McpProvider'
import { connected } from '../testing/harness'
import { BlockedNotice, ConnectionStatus, useBlockingError } from './ConnectionNotices'

afterEach(cleanup)
function Page() {
  const blocking = useBlockingError()
  return <><footer><ConnectionStatus/></footer><main>{blocking ? <BlockedNotice error={blocking}/> : <p>the routes</p>}</main></>
}
function renderPage(overrides: Partial<McpConnection>) {
  return render(<MemoryRouter><McpContext.Provider value={connected(overrides)}><Page/></McpContext.Provider></MemoryRouter>)
}
it('shows a persistent actionable error when the first connection fails', () => {
  renderPage({status:'failed',error:new Error('no session token'),everConnected:false})
  expect(screen.getByRole('alert').textContent).toContain('Not connected to the runtime')
  expect(screen.getByRole('alert').textContent).toContain('no session token')
  expect(screen.queryByText('the routes')).toBeNull()
})
it('keeps the workspace in place and puts retry details in the status strip', async () => {
  const retry=vi.fn()
  renderPage({status:'reconnecting',error:new Error('closed'),everConnected:true,attempt:3,retryNow:retry})
  expect(screen.getByRole('main').textContent).toBe('the routes')
  expect(screen.getByRole('status').textContent).toBe('reconnecting')
  expect(screen.queryByText(/Connection attempts/)).toBeNull()
  fireEvent.click(screen.getByRole('button',{name:'Connection status: reconnecting'}))
  const dialog=await screen.findByRole('dialog',{name:'Connection status'})
  expect(dialog.textContent).toContain('Reconnecting to Desk')
  expect(dialog.querySelector('details')?.open).toBe(false)
  fireEvent.click(screen.getByRole('button',{name:'Retry connection'}))
  expect(retry).toHaveBeenCalledOnce()
})
it('renders the routes when the connection is blocked but has no reason', () => {
  renderPage({status:'failed',error:null,everConnected:false})
  expect(screen.queryByRole('alert')).toBeNull()
  expect(screen.getByText('the routes')).toBeTruthy()
})
it('keeps missing capabilities distinct from a healthy connection', async () => {
  renderPage({status:'ready',known:false,capabilitiesError:new Error('the request timed out')})
  fireEvent.click(screen.getByRole('button',{name:'Connection status: Limited connection'}))
  expect((await screen.findByRole('dialog')).textContent).toContain('Some features are unavailable')
  expect(screen.getByText('the request timed out')).toBeTruthy()
})
it('offers retry on the initial failure without broadcasting attempt counts', () => {
  renderPage({status:'reconnecting',error:new Error('closed'),everConnected:false,attempt:2})
  expect(screen.getByRole('alert').textContent).toContain('reconnecting automatically')
  expect(screen.getByRole('button',{name:'Retry connection'})).toBeTruthy()
  expect(screen.queryByText(/attempt 2/)).toBeNull()
})
