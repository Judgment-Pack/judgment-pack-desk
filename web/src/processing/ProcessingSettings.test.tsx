import { UnsavedChangesProvider } from '../shell/UnsavedChanges'
import { respondToDiscardDialogs } from '../testing/discardDialogs'
respondToDiscardDialogs()
import { cleanup, fireEvent, render, screen, waitFor, within } from '@testing-library/react'
import { QueryClientProvider } from '@tanstack/react-query'
import { MemoryRouter } from 'react-router-dom'
import { afterEach, expect, it, vi } from 'vitest'
import { testQueryClient } from '../testing/harness'
import { DESK_CONFIG_QUERY_KEY } from '../config/queries'
import { OCRSettings } from './ProcessingSettings'
import { configureRequest, PROCESSING_KEY, type ProcessingAnswer, type ProcessingSettings, type RunningGateway } from './processing'

const mocks = vi.hoisted(() => ({ fetch: vi.fn() }))
vi.mock('../files/client', async original => ({ ...await original<typeof import('../files/client')>(), deskFetch: mocks.fetch }))
afterEach(() => { cleanup(); vi.clearAllMocks() })

const KEY = 'azure-key-5f1d0c7e9b2a4c68a1e3d7f0b9c2e4a6'
const local = { id: 'ocr-local', name: 'On this computer', kind: 'tesseract' as const, enabled: true, ready: true }
const azure = { id: 'ocr-azure', name: 'Work scans', kind: 'azure-document-intelligence' as const, enabled: true, ready: true, endpoint: 'https://work.cognitiveservices.azure.com', credentialConfigured: true }
const digest = 'sha256:' + 'a'.repeat(64)
const settings = (patch: Partial<ProcessingSettings> = {}): ProcessingSettings => ({ version: 1, mode: 'off', connection: '', connections: [local, azure], timeoutSeconds: 60, sha256: digest, state: 'ready', ...patch })
const running = (documentProcessing: boolean, restarted?: boolean): RunningGateway => ({ status: 'ready', documentProcessing, ...(restarted ? { restarted } : {}) })

type Sent = { url: string; body: string }
/** The settings already read; each later request answered by `reply`, and every request written down. */
function setup(read: ProcessingAnswer<ProcessingSettings>, reply: (url: string, body: Record<string, unknown>) => unknown = () => ({ result: read.result, localGateway: read.localGateway })) {
  const client = testQueryClient()
  client.setQueryData(PROCESSING_KEY, read)
  const sent: Sent[] = []
  mocks.fetch.mockImplementation(async (url: string, init: RequestInit = {}) => {
    const body = String(init.body ?? '')
    sent.push({ url, body })
    return Response.json(reply(url, JSON.parse(body || '{}') as Record<string, unknown>))
  })
  const invalidate = vi.spyOn(client, 'invalidateQueries')
  render(<QueryClientProvider client={client}><MemoryRouter><UnsavedChangesProvider><OCRSettings /></UnsavedChangesProvider></MemoryRouter></QueryClientProvider>)
  return { client, sent, invalidate }
}
const configured = (sent: Sent[]) => sent.filter(s => s.url === '/api/document-processing/configure').map(s => JSON.parse(s.body) as { ifMatch: string; config: ProcessingSettings })

it('offers local OCR, the three cloud processors and a program, and saves without the status’s own members', async () => {
  const { sent } = setup({ result: settings(), localGateway: running(false) })
  fireEvent.click(screen.getByRole('button', { name: 'Add processor' }))
  const dialog = screen.getByRole('dialog')
  fireEvent.change(within(dialog).getByLabelText('Name'), { target: { value: 'Google work' } })
  fireEvent.click(within(dialog).getByRole('combobox', { name: 'Processor' }))
  for (const name of ['Local OCR · Tesseract · English', 'Google Document AI', 'Azure Document Intelligence', 'Amazon Textract', 'OCR program on this computer']) expect(screen.getByRole('option', { name })).toBeTruthy()
  fireEvent.click(screen.getByRole('option', { name: 'Google Document AI' }))
  for (const [label, value] of [['Google Cloud project ID', 'my-project'], ['Location', 'eu'], ['Processor ID', 'processor1'], ['Service account JSON', '{"type":"service_account"}']] as const) {
    fireEvent.change(within(dialog).getByLabelText(label), { target: { value } })
  }
  fireEvent.click(within(dialog).getByRole('button', { name: 'Save' }))
  await waitFor(() => expect(configured(sent)).toHaveLength(1))
  const [save] = configured(sent)
  expect(save!.ifMatch).toBe(digest)
  expect(save!.config.connections[2]).toMatchObject({ name: 'Google work', kind: 'google-document-ai', project: 'my-project', location: 'eu', processor: 'processor1', credential: '{"type":"service_account"}' })
  for (const connection of save!.config.connections) { expect(connection).not.toHaveProperty('ready'); expect(connection).not.toHaveProperty('credentialConfigured') }
  expect(save!.config.connections[1]).not.toHaveProperty('credential')
})

it('never shows a credential again, and sends a typed one only in the save’s body', async () => {
  const { sent } = setup({ result: settings(), localGateway: running(false) })
  fireEvent.click(screen.getByRole('button', { name: 'Manage Work scans' }))
  const key = within(screen.getByRole('dialog')).getByLabelText('API key') as HTMLInputElement
  expect(key.value).toBe('')
  fireEvent.change(key, { target: { value: KEY } })
  fireEvent.click(within(screen.getByRole('dialog')).getByRole('button', { name: 'Save' }))
  await waitFor(() => expect(screen.queryByRole('dialog')).toBeNull())
  expect(sent.filter(s => s.body.includes(KEY)).map(s => s.url)).toEqual(['/api/document-processing/configure'])
  expect(sent.some(s => s.url.includes(KEY))).toBe(false)
  expect(document.body.innerHTML).not.toContain(KEY)
  fireEvent.click(screen.getByRole('button', { name: 'Manage Work scans' }))
  expect((within(screen.getByRole('dialog')).getByLabelText('API key') as HTMLInputElement).value).toBe('')
})

it('asks for the credential again when the destination it was entered for changes', () => {
  setup({ result: settings(), localGateway: running(false) })
  fireEvent.click(screen.getByRole('button', { name: 'Manage Work scans' }))
  const dialog = screen.getByRole('dialog')
  fireEvent.change(within(dialog).getByLabelText('Name'), { target: { value: 'Renamed' } })
  expect((within(dialog).getByRole('button', { name: 'Save' }) as HTMLButtonElement).disabled).toBe(false)
  fireEvent.change(within(dialog).getByLabelText('Azure endpoint'), { target: { value: 'https://other.cognitiveservices.azure.com' } })
  expect((within(dialog).getByRole('button', { name: 'Save' }) as HTMLButtonElement).disabled).toBe(true)
  expect(within(dialog).getByText('Enter the credential again: it is sent only to the destination it was entered for.')).toBeTruthy()
  fireEvent.change(within(dialog).getByLabelText('API key'), { target: { value: KEY } })
  expect((within(dialog).getByRole('button', { name: 'Save' }) as HTMLButtonElement).disabled).toBe(false)
})

it('says which plan the running gateway has, and that a save turning OCR on restarts it, before the save', async () => {
  const { sent, invalidate } = setup({ result: settings(), localGateway: running(false) }, (url, body) => url.endsWith('/configure')
    ? { result: { ...settings(), ...(body.config as object), sha256: 'sha256:' + 'b'.repeat(64) }, localGateway: running(true, true) } : undefined)
  expect(screen.getByText('The running local gateway reads no scanned pages: it was started without OCR.')).toBeTruthy()
  // A timeout alone leaves OCR as it is: no restart.
  fireEvent.change(screen.getByLabelText('Processing timeout (seconds)'), { target: { value: '90' } })
  expect(screen.queryByRole('button', { name: 'Save and restart' })).toBeNull()
  expect(screen.getByRole('button', { name: 'Save changes' })).toBeTruthy()
  fireEvent.click(screen.getByRole('combobox', { name: 'OCR mode' })); fireEvent.click(screen.getByRole('option', { name: 'When a page has no text' }))
  fireEvent.click(screen.getByRole('combobox', { name: 'OCR processor' })); fireEvent.click(screen.getByRole('option', { name: 'On this computer' }))
  expect(screen.getByText('Saving turns OCR on. Desk restarts its local gateway to take it, and reads in progress stop.')).toBeTruthy()
  expect(configured(sent)).toHaveLength(0)
  fireEvent.click(screen.getByRole('button', { name: 'Save and restart' }))
  await screen.findByText('Desk restarted its local gateway with these settings.')
  expect(configured(sent)[0]!.config).toMatchObject({ mode: 'auto', connection: 'ocr-local', timeoutSeconds: 90 })
  expect(screen.getByText('The running local gateway reads scanned pages with OCR.')).toBeTruthy()
  expect(invalidate).toHaveBeenCalledWith({ queryKey: DESK_CONFIG_QUERY_KEY })
})

it('warns before a save that turns OCR off, and says when the settings are not the running plan’s', () => {
  setup({ result: settings({ mode: 'auto', connection: 'ocr-local' }), localGateway: running(false) })
  expect(screen.getByText('It takes these settings when it starts again: when you next save them, or when Desk starts.')).toBeTruthy()
  cleanup()
  setup({ result: settings({ mode: 'auto', connection: 'ocr-local' }), localGateway: running(true) })
  expect(screen.queryByText('It takes these settings when it starts again: when you next save them, or when Desk starts.')).toBeNull()
  fireEvent.click(screen.getByRole('combobox', { name: 'OCR mode' })); fireEvent.click(screen.getByRole('option', { name: 'Off' }))
  expect(screen.getByText('Saving turns OCR off. Desk restarts its local gateway to take it, and reads in progress stop.')).toBeTruthy()
  expect(screen.getByRole('button', { name: 'Save and restart' })).toBeTruthy()
})

it('holds the timeout to 10–120 seconds before anything is sent', () => {
  const { sent } = setup({ result: settings(), localGateway: running(false) })
  const timeout = screen.getByLabelText('Processing timeout (seconds)')
  for (const [value, allowed] of [['9', false], ['10', true], ['120', true], ['121', false], ['60.5', false]] as const) {
    fireEvent.change(timeout, { target: { value } })
    expect((screen.getByRole('button', { name: 'Save changes' }) as HTMLButtonElement).disabled).toBe(!allowed)
  }
  expect(sent).toHaveLength(0)
})

it('keeps the input when the settings changed elsewhere', async () => {
  const { client } = setup({ result: settings(), localGateway: running(false) }, () => ({ error: 'processing-changed', localGateway: running(false) }))
  fireEvent.click(screen.getByRole('button', { name: 'Manage On this computer' }))
  fireEvent.change(within(screen.getByRole('dialog')).getByLabelText('Name'), { target: { value: 'New name' } })
  fireEvent.click(within(screen.getByRole('dialog')).getByRole('button', { name: 'Save' }))
  await within(screen.getByRole('dialog')).findByText('The document processing settings changed elsewhere. Reload them before saving.')
  expect((within(screen.getByRole('dialog')).getByLabelText('Name') as HTMLInputElement).value).toBe('New name')
  expect(client.getQueryData<ProcessingAnswer<ProcessingSettings>>(PROCESSING_KEY)?.result.sha256).toBe(digest)
})

it('refuses a test PDF past 4 MiB before sending it', async () => {
  const { sent } = setup({ result: settings({ mode: 'auto', connection: 'ocr-local' }), localGateway: running(true) })
  const big = new File([new Uint8Array(4 * 1024 * 1024 + 1)], 'scan.pdf', { type: 'application/pdf' })
  fireEvent.change(screen.getByLabelText('Test PDF file'), { target: { files: [big] } })
  await screen.findByText('Choose a PDF of at most 4 MiB.')
  expect(sent.filter(s => s.url.endsWith('/test'))).toHaveLength(0)
})

it('shows a test’s pages and its codes', async () => {
  setup({ result: settings({ mode: 'auto', connection: 'ocr-local' }), localGateway: running(true) }, () => ({ result: {
    processing: { status: 'partial', errors: [{ code: 'ocr-incomplete', page: null }] }, extraction: 'ocr', pageCount: 2,
    pages: [{ number: 1, status: 'ok', extraction: 'ocr', text: 'Scanned source text' }] }, localGateway: running(true) }))
  const pdf = new File([new TextEncoder().encode('%PDF-1.7')], 'scan.pdf', { type: 'application/pdf' })
  fireEvent.change(screen.getByLabelText('Test PDF file'), { target: { files: [pdf] } })
  await screen.findByText('Scanned source text')
  expect(screen.getByText('ocr-incomplete')).toBeTruthy()
  expect(screen.getByText('Result: partial · 2 pages')).toBeTruthy()
})

it('leaves status-only members and a blank credential out of a save', () => {
  const request = configureRequest(settings(), { mode: 'auto', connection: 'ocr-azure', timeoutSeconds: undefined, connections: [{ ...azure, credential: '' }, { ...local, credential: KEY }] })
  expect(request).toEqual({ ifMatch: digest, config: { version: 1, mode: 'auto', connection: 'ocr-azure', timeoutSeconds: 120, connections: [
    { id: 'ocr-azure', name: 'Work scans', kind: 'azure-document-intelligence', enabled: true, endpoint: 'https://work.cognitiveservices.azure.com' },
    { id: 'ocr-local', name: 'On this computer', kind: 'tesseract', enabled: true, credential: KEY }] } })
})
