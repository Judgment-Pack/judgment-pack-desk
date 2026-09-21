import { cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react'
import { afterEach, expect, it, vi } from 'vitest'
import { DeskConfigFixture } from '../config/DeskConfigProvider'
import { effectiveConfig } from '../config/deskConfig'
import { signedDocument } from './__fixtures__/signedDocument'
import { verifyDocument } from './client'
import { CitationPreview, SourceReader } from './SourceReader'
const mocks = vi.hoisted(() => ({ load: vi.fn() }))
vi.mock('./client', async original => ({ ...await original<typeof import('./client')>(), loadDocument: mocks.load }))
afterEach(() => { cleanup(); vi.clearAllMocks() })
async function fixture() {
  const { object, pin, reference } = await signedDocument()
  const value = await verifyDocument(object, pin)
  const config = effectiveConfig(undefined)
  config.config = { ...config.config, research: { ...config.config.research, gateway: pin } }
  return { value, config, reference }
}
it('loads only on click, highlights normalized quotes and opens a read-only full reader', async () => {
  const { value, config, reference } = await fixture()
  mocks.load.mockResolvedValue(value)
  const onRead = vi.fn()
  render(<DeskConfigFixture value={config}><CitationPreview name="notes.txt" reference={reference} citation={{ page: 1, quote: 'line one line two' }} number={1} onRead={onRead} /></DeskConfigFixture>)
  expect(mocks.load).not.toHaveBeenCalled()
  fireEvent.click(screen.getByRole('button', { name: /View source 1/ }))
  await screen.findByText('Quote found on page 1')
  expect(screen.queryByRole('checkbox')).toBeNull()
  expect(screen.getByText('line one line two', { selector: 'mark' })).toBeTruthy()
  fireEvent.click(screen.getByRole('button', { name: 'Open source text' }))
  expect(onRead).toHaveBeenCalledOnce()
  await waitFor(() => expect(screen.queryByRole('dialog')).toBeNull())
})
it.each([['made up', [1]], ['line one', [2]]] as const)('does not offer a reader for a mismatched quote or unselected page', async (quote, pages) => {
  const { value, config, reference } = await fixture()
  mocks.load.mockResolvedValue(value)
  render(<DeskConfigFixture value={config}><CitationPreview name="notes.txt" reference={{ ...reference, pages: [...pages] }} citation={{ page: 1, quote }} number={1} onRead={vi.fn()} /></DeskConfigFixture>)
  fireEvent.click(screen.getByRole('button', { name: /View source 1/ }))
  await screen.findByRole('alert')
  expect(screen.queryByText('Quote found on page 1')).toBeNull()
  expect(screen.queryByRole('button', { name: 'Open source text' })).toBeNull()
})
it('aborts on close and ignores late document bytes', async () => {
  const { value, config, reference } = await fixture()
  let resolve!: (value: unknown) => void
  mocks.load.mockImplementation(() => new Promise(done => { resolve = done }))
  render(<DeskConfigFixture value={config}><CitationPreview name="notes.txt" reference={reference} citation={{ page: 1, quote: 'line one' }} number={1} onRead={vi.fn()} /></DeskConfigFixture>)
  fireEvent.click(screen.getByRole('button', { name: /View source 1/ }))
  const signal = mocks.load.mock.calls[0]![2] as AbortSignal
  fireEvent.click(screen.getByRole('button', { name: 'Close' }))
  expect(signal.aborted).toBe(true)
  resolve(value)
  await waitFor(() => expect(screen.queryByRole('dialog')).toBeNull())
  expect(screen.queryByText('Quote found on page 1')).toBeNull()
})
it('keeps verification failures visible and retries without showing unverified source text', async () => {
  const { config, reference, value } = await fixture()
  mocks.load.mockRejectedValueOnce(new Error('Verification refused')).mockResolvedValueOnce(value)
  render(<DeskConfigFixture value={config}><SourceReader name="notes.txt" reference={reference} /></DeskConfigFixture>)
  await screen.findByText('Verification refused')
  expect(screen.queryByText(/line one/)).toBeNull()
  fireEvent.click(screen.getByRole('button', { name: 'Retry' }))
  expect(await screen.findByText('line one line two')).toBeTruthy()
  expect(screen.queryByRole('checkbox')).toBeNull()
  expect(screen.getByRole('button', { name: 'Download original' })).toBeTruthy()
})
