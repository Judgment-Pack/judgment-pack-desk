import { cleanup, fireEvent, render, screen } from '@testing-library/react'
import { afterEach, expect, it, vi } from 'vitest'
import { MessageRenderer } from './MessageRenderer'
import { DeskConfigFixture } from '../config/DeskConfigProvider'
import { effectiveConfig } from '../config/deskConfig'
import { signedDocument } from '../documents/__fixtures__/signedDocument'
import { verifyDocument } from '../documents/client'

const mocks = vi.hoisted(() => ({ load: vi.fn() }))
vi.mock('../documents/client', async original => ({ ...await original<typeof import('../documents/client')>(), loadDocument: mocks.load }))
afterEach(() => { cleanup(); vi.clearAllMocks() })

it('keeps a verified citation open when more response text arrives', async () => {
  const { object, pin, reference } = await signedDocument()
  mocks.load.mockResolvedValue(await verifyDocument(object, pin))
  const config = effectiveConfig(undefined)
  config.config = { ...config.config, research: { ...config.config.research, gateway: pin } }
  const documents = [{ id: reference.id, name: 'notes.txt', text: '', document: reference }]
  const text = `The source says [line one](attachment:${reference.id}/${reference.digest}/page/1).`
  const view = (response: string) => <DeskConfigFixture value={config}><MessageRenderer text={response} documents={documents} /></DeskConfigFixture>
  const rendered = render(view(text))
  const citation = screen.getByRole('button', { name: 'line one' })
  fireEvent.click(citation)
  await screen.findByText('Receipt verified. This confirms byte lineage, not accuracy or authority.')
  rendered.rerender(view(`${text}\n\nMore of the answer.`))
  expect(screen.getByRole('button', { name: 'line one' })).toBe(citation)
  expect(screen.getByRole('dialog', { name: 'notes.txt' })).toBeTruthy()
  expect(screen.getByText('Extraction complete.')).toBeTruthy()
  expect(mocks.load).toHaveBeenCalledTimes(1)
})

it('preserves a reader’s code wrapping preference while the answer grows', () => {
  const text = '```json\n{"example":true}\n```'
  const rendered = render(<MessageRenderer text={text} />)
  fireEvent.click(screen.getByRole('button', { name: 'Wrap' }))
  expect(screen.getByRole('button', { name: 'Wrap' }).getAttribute('aria-pressed')).toBe('false')
  rendered.rerender(<MessageRenderer text={`${text}\n\nThe next paragraph.`} />)
  expect(screen.getByRole('button', { name: 'Wrap' }).getAttribute('aria-pressed')).toBe('false')
})
