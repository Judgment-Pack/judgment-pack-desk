import { cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react'
import { afterEach, expect, it, vi } from 'vitest'
import { CodeBlock } from './CodeBlock'

afterEach(() => { cleanup(); vi.unstubAllGlobals() })

it('copies full source after changing wrap without changing the displayed bytes', async () => {
  const writeText = vi.fn().mockResolvedValue(undefined)
  vi.stubGlobal('navigator', { clipboard: { writeText } })
  const text = JSON.stringify({ description: 'long '.repeat(100), value: ['5000', 5000, false, null] }, null, 2)
  render(<CodeBlock text={text} />)
  const pre = screen.getByLabelText('JSON')
  expect(pre.textContent).toBe(text)
  const toggle = screen.getByRole('button', { name: 'Wrap' })
  expect(toggle.getAttribute('aria-pressed')).toBe('true')
  fireEvent.click(toggle)
  expect(toggle.getAttribute('aria-pressed')).toBe('false')
  expect(pre.textContent).toBe(text)
  fireEvent.click(screen.getByRole('button', { name: 'Copy JSON' }))
  await screen.findByText('Copied')
  expect(writeText).toHaveBeenCalledWith(text)
})

it('offers selectable text when clipboard access fails and clears feedback for another value', async () => {
  vi.stubGlobal('navigator', { clipboard: { writeText: vi.fn().mockRejectedValue(new Error('denied')) } })
  const { rerender } = render(<CodeBlock text='"first"' />)
  fireEvent.click(screen.getByRole('button', { name: 'Copy JSON' }))
  await screen.findByText('Select the text below to copy it.')
  expect(screen.queryByText('Copied')).toBeNull()
  rerender(<CodeBlock text='"second"' />)
  await waitFor(() => expect(screen.queryByRole('status')).toBeNull())
  expect(screen.getByLabelText('JSON').textContent).toBe('"second"')
})
