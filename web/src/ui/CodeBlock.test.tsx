import { cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react'
import { afterEach, expect, it, vi } from 'vitest'
import { CodeBlock } from './CodeBlock'

afterEach(() => { cleanup(); vi.unstubAllGlobals() })

it('copies full source after changing wrap without changing formatted indentation', async () => {
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


it('indents compact JSON without losing original numbers, duplicate keys or escapes', async () => {
  const writeText = vi.fn().mockResolvedValue(undefined)
  vi.stubGlobal('navigator', { clipboard: { writeText } })
  const text = String.raw`{"id":9007199254740993,"id":-0,"name":"\u0061","items":[{},true]}`
  render(<CodeBlock text={text} label="Included with your message" expandable />)
  const pre = screen.getByRole('region', { name: 'Included with your message' })
  expect(pre.textContent).toContain('\n  "id": 9007199254740993,\n  "id": -0,')
  expect(pre.textContent).toContain(String.raw`"name": "\u0061"`)
  expect(pre.textContent).toContain('"items": [\n    {},\n    true\n  ]')
  fireEvent.click(screen.getByRole('button', { name: 'Wrap' }))
  expect(pre.textContent).toContain('\n  "id": 9007199254740993,')
  fireEvent.click(screen.getByRole('button', { name: 'Copy Included with your message' }))
  await screen.findByText('Copied')
  expect(writeText).toHaveBeenCalledWith(text)
  fireEvent.click(screen.getByRole('button', { name: 'Show code' }))
  expect(screen.getByRole('dialog').querySelector('pre')?.textContent).toBe(pre.textContent)
})

it.each(['{"partial":', '{not: "JSON"}', 'const data = {"ok":true};', 'Plain text'])('retains incomplete or non-JSON source: %s', text => {
  render(<CodeBlock text={text} />)
  expect(screen.getByRole('region', { name: 'JSON' }).textContent).toBe(text)
})

it('formats newly completed JSON when a streamed source changes', () => {
  const { rerender } = render(<CodeBlock text={'{"ok":'} />)
  rerender(<CodeBlock text={'{"ok":true}'} />)
  expect(screen.getByRole('region', { name: 'JSON' }).textContent).toBe('{\n  "ok": true\n}')
})
