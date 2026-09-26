import { cleanup, fireEvent, render, screen } from '@testing-library/react'
import { afterEach, expect, it, vi } from 'vitest'
import type { Candidate } from '../research/run'
import { DraftReference } from './DraftReference'
import { SentAttachments } from './MessageSources'
const candidate = {revision:1,document:{title:'Policy'}} as Candidate
afterEach(cleanup)
it('replaces the action with a quiet indicator when the draft is displayed', () => {
  const onOpen=vi.fn(), view=render(<DraftReference candidate={candidate} open={false} latest onOpen={onOpen}/>)
  fireEvent.click(screen.getByRole('button',{name:'Open draft'}));expect(onOpen).toHaveBeenCalledOnce()
  view.rerender(<DraftReference candidate={candidate} open latest onOpen={onOpen}/>)
  expect(screen.queryByRole('button')).toBeNull();expect(screen.getByText('Open')).toBeTruthy()
  expect(screen.getByText('Revision 1')).toBeTruthy()
  view.rerender(<DraftReference candidate={candidate} open={false} latest={false} onOpen={onOpen}/>)
  expect(screen.getByRole('button',{name:'Open latest draft'})).toBeTruthy()
})
it('sent text-file chips open the reader without pending-upload removal controls', () => {
  const read=vi.fn();render(<SentAttachments files={[{id:'text',name:'Policy.txt',text:'Source text'}]} onRead={read}/>)
  fireEvent.click(screen.getByRole('button',{name:'Policy.txt'}));expect(read).toHaveBeenCalledOnce()
  expect(screen.queryByRole('button',{name:/Remove/})).toBeNull()
})
