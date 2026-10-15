import { cleanup, fireEvent, render, screen, waitFor, within } from '@testing-library/react'
import { afterEach, expect, it, vi } from 'vitest'
import { DeskNotices, DeskNoticesProvider } from './DeskNotices'
import { QueryStatusNotice } from './QueryStatusNotice'

afterEach(cleanup)
it('keeps background failures out of page content and retains scoped details and read-only recovery', async () => {
  const refresh=vi.fn(), run=vi.fn()
  function Page({error=Error('provider-busy'),retrying=false}:{error?:Error|null;retrying?:boolean}) {
    return <DeskNoticesProvider><main><p>Saved records</p><button onClick={run}>Start research</button>
      <QueryStatusNotice id="research:one" label="Research status unavailable" context="Suppliers" error={error} retrying={retrying} onRetry={refresh}>Last known status; the run may still be active.</QueryStatusNotice>
    </main><footer><DeskNotices/><button data-connection-status>connected</button></footer></DeskNoticesProvider>
  }
  const view=render(<Page/>)
  expect(within(screen.getByRole('main')).queryByText(/Last known/)).toBeNull()
  const trigger=screen.getByRole('button',{name:'Research status unavailable'})
  expect(trigger.closest('footer')).toBeTruthy()
  fireEvent.click(trigger)
  const panel=screen.getByRole('dialog',{name:'Research status unavailable · Suppliers'})
  expect(within(panel).getByText(/Last known/)).toBeTruthy()
  expect(within(panel).getByText('provider-busy').closest('details')?.open).toBe(false)
  fireEvent.click(within(panel).getByRole('button',{name:'Retry'}))
  expect(refresh).toHaveBeenCalledOnce();expect(run).not.toHaveBeenCalled()
  view.rerender(<Page retrying/>)
  expect((within(panel).getByRole('button',{name:'Retrying…'}) as HTMLButtonElement).disabled).toBe(true)
  view.rerender(<Page error={null}/>)
  await waitFor(()=>expect(screen.queryByRole('dialog')).toBeNull())
  expect(screen.queryByRole('button',{name:'Research status unavailable'})).toBeNull()
  expect(screen.getByText('Saved records')).toBeTruthy()
})
it('does not publish a notice for a healthy read',()=>{
 render(<DeskNoticesProvider><QueryStatusNotice id="ok" label="Unavailable" error={null} onRetry={vi.fn()}/><DeskNotices/></DeskNoticesProvider>)
 expect(screen.queryByRole('button')).toBeNull()
})
