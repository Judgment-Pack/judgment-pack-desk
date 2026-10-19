import { StrictMode } from 'react'
import { cleanup, fireEvent, render, screen, waitFor, within } from '@testing-library/react'
import { afterEach, expect, it, vi } from 'vitest'
import { MemoryRouter } from 'react-router-dom'
import { DeskNotices, DeskNoticesProvider, DeskStatusNotice } from './DeskNotices'
import { Button } from '../ui/Button'
import { PopoverClose } from '../ui/Popover'

afterEach(cleanup)
it('groups notices by severity, deduplicates panes, and removes only the unmounted owner', async () => {
  const retry=vi.fn()
  function Page({first=true,second=true}:{first?:boolean;second?:boolean}) {
    return <StrictMode><DeskNoticesProvider><main>Workspace</main>
      <DeskStatusNotice id="setup" label="Optional setup">Review optional settings</DeskStatusNotice>
      {first&&<DeskStatusNotice id="folders" label="Folders unavailable" tone="warning" actions={<Button onClick={retry}>Retry folders</Button>}>Folder read failed</DeskStatusNotice>}
      {second&&<DeskStatusNotice id="folders" label="Folders unavailable" tone="warning" actions={<Button onClick={retry}>Retry folders</Button>}>Folder read failed</DeskStatusNotice>}
      <footer><DeskNotices/></footer>
    </DeskNoticesProvider></StrictMode>
  }
  const page=render(<Page/>)
  const trigger=await screen.findByRole('button',{name:'Folders unavailable · 2 notices'})
  expect(trigger.closest('footer')).toBeTruthy()
  expect(screen.getByRole('main').textContent).toBe('Workspace')
  expect(screen.queryByText('Folder read failed')).toBeNull()
  fireEvent.click(trigger)
  const dialog=await screen.findByRole('dialog',{name:'Desk notices'})
  expect(within(dialog).getAllByText('Folder read failed')).toHaveLength(1)
  expect(dialog.textContent?.indexOf('Folders unavailable')).toBeLessThan(dialog.textContent?.indexOf('Optional setup')??0)
  fireEvent.click(within(dialog).getByRole('button',{name:'Retry folders'}))
  expect(retry).toHaveBeenCalledOnce()
  page.rerender(<Page first={false}/>)
  expect(within(screen.getByRole('dialog')).getByText('Folder read failed')).toBeTruthy()
  page.rerender(<Page first={false} second={false}/>)
  await waitFor(()=>expect(screen.queryByText('Folder read failed')).toBeNull())
  fireEvent.keyDown(screen.getByRole('dialog'),{key:'Escape'})
  await waitFor(()=>expect(document.activeElement).toBe(screen.getByRole('button',{name:'Optional setup'})))
})
it('closes recovery actions and restores focus when the final notice resolves', async () => {
  function Page({warning=true}:{warning?:boolean}) {
    return <MemoryRouter><DeskNoticesProvider>
      {warning&&<DeskStatusNotice id="service" label="Needs setup" actions={<PopoverClose><Button>Review settings</Button></PopoverClose>}>Service unavailable</DeskStatusNotice>}
      <footer><DeskNotices/><button data-connection-status>connected</button></footer>
    </DeskNoticesProvider></MemoryRouter>
  }
  const page=render(<Page/>)
  fireEvent.click(screen.getByRole('button',{name:'Needs setup'}))
  fireEvent.click(screen.getByRole('button',{name:'Review settings'}))
  await waitFor(()=>expect(screen.queryByRole('dialog')).toBeNull())
  fireEvent.click(screen.getByRole('button',{name:'Needs setup'}))
  page.rerender(<Page warning={false}/>)
  await waitFor(()=>expect(document.activeElement).toBe(screen.getByRole('button',{name:'connected'})))
  page.rerender(<Page/>)
  expect(screen.queryByRole('dialog')).toBeNull()
})
