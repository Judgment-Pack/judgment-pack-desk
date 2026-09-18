import { act, cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react'
import { afterEach, expect, it, vi } from 'vitest'
import { DocumentPreview } from './DocumentPreview'
import { DeskConfigFixture } from '../config/DeskConfigProvider'
import { effectiveConfig } from '../config/deskConfig'
import { signedDocument } from './__fixtures__/signedDocument'
import { verifyDocument } from './client'
import { readDocumentRecord } from './record'
import partial from './__fixtures__/partial-ocr-failed.json'
const mocks = vi.hoisted(() => ({ load: vi.fn() }))
vi.mock('./client', async original => ({ ...await original<typeof import('./client')>(), loadDocument: mocks.load }))
afterEach(() => { cleanup(); vi.clearAllMocks() })
async function setup() {
  const { object, pin, reference } = await signedDocument()
  const document = await verifyDocument(object,pin)
  const config = effectiveConfig(undefined); config.config = { ...config.config, research: { ...config.config.research, gateway: pin } }
  const onChange = vi.fn()
  return { document, config, reference, onChange }
}
it('shows explicit consent for partial pages and disables unreadable pages', async () => {
  const { document, config, reference, onChange } = await setup()
  document.record = readDocumentRecord(partial); mocks.load.mockResolvedValue(document)
  render(<DeskConfigFixture value={config}><DocumentPreview name="partial.pdf" reference={reference} disabled={false} onChange={onChange} /></DeskConfigFixture>)
  fireEvent.click(screen.getByRole('button',{name:'partial.pdf'}))
  await screen.findByText('Some pages are missing or unreadable. Only selected readable pages can be sent.')
  const consent = screen.getByLabelText('Use the selected readable pages despite the missing content.')
  expect((consent as HTMLInputElement).checked).toBe(false)
  fireEvent.click(consent)
  expect(onChange).toHaveBeenCalledWith({...reference,allowPartial:true})
  expect(screen.getAllByRole('checkbox').some(node => (node as HTMLInputElement).disabled)).toBe(true)
})
it('never shows a verified citation for a mismatched quote', async () => {
  const { document, config, reference } = await setup(); mocks.load.mockResolvedValue(document)
  render(<DeskConfigFixture value={config}><DocumentPreview name="notes.txt" reference={reference} disabled citation={{page:1,quote:'Invented'}} /></DeskConfigFixture>)
  fireEvent.click(screen.getByRole('button',{name:'Invented'}))
  await screen.findByText('This quote does not match the cited page.')
  expect(screen.queryByText(/Receipt verified/)).toBeNull()
})
it('does not show a late verification after closing the preview', async () => {
  const { document, config, reference } = await setup(); let finish!: (value: typeof document) => void
  mocks.load.mockReturnValue(new Promise(resolve => { finish = resolve }))
  render(<DeskConfigFixture value={config}><DocumentPreview name="notes.txt" reference={reference} disabled /></DeskConfigFixture>)
  fireEvent.click(screen.getByRole('button',{name:'notes.txt'})); await waitFor(()=>expect(finish).toBeDefined())
  fireEvent.keyDown(window.document,{key:'Escape'})
  await act(async()=>finish(document))
  expect(screen.queryByText(/Receipt verified/)).toBeNull()
})
