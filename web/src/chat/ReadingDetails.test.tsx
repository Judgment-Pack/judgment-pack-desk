import { cleanup, fireEvent, render, screen } from '@testing-library/react'
import { afterEach, expect, it, vi } from 'vitest'
import { MessageDetails } from './ReadingDetails'
afterEach(() => { cleanup(); vi.unstubAllGlobals() })
it('shows the saved user message and copies the exact submitted snapshot, including JSON escapes', async () => {
  const input = 'Explain this.\n' + JSON.stringify({ pages: [{ text: 'line one\nline two', page: 1 }] })
  const writeText = vi.fn().mockResolvedValue(undefined)
  vi.stubGlobal('navigator', { clipboard: { writeText } })
  render(<MessageDetails text={'Explain this.\n\nAttached: notes.pdf'} input={input} />)
  expect(screen.getByText(/Attached: notes.pdf/)).toBeTruthy()
  fireEvent.click(screen.getByText('Exact sent context'))
  fireEvent.click(screen.getByRole('button', { name: 'Copy Sent context' }))
  await screen.findByText('Copied')
  expect(writeText).toHaveBeenCalledWith(input)
})
