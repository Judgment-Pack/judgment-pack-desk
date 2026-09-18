import { QueryClientProvider } from '@tanstack/react-query'
import { cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react'
import { MemoryRouter } from 'react-router-dom'
import { afterEach, expect, it, vi } from 'vitest'
import { testQueryClient } from '../testing/harness'
import { DeskConfigFixture } from '../config/DeskConfigProvider'
import { decodeDeskConfig, DOCUMENT_DEFAULTS, effectiveConfig, type LocalGatewayStatus } from '../config/deskConfig'
import { ConnectionsSettings } from './ConnectionsSettings'
const mocks = vi.hoisted(() => ({ fetch: vi.fn() }))
vi.mock('../files/client', async original => ({ ...await original<typeof import('../files/client')>(), deskFetch: mocks.fetch }))
afterEach(() => { cleanup(); vi.clearAllMocks(); vi.restoreAllMocks() })
const gateway = { url: 'http://localhost:8787', authority: 'gateway:test', signer: { algorithm: 'ed25519', public: 'ab'.repeat(32) } }
const research = { gateway, sources: { read: { source: 'read', dialect: 'jina-reader' } } }
function setup(extra = {}, localGateway?: LocalGatewayStatus) {
  const content = JSON.stringify({ deskConfigVersion: 1, research: { ...research, ...extra } })
  const effective = effectiveConfig(undefined, undefined, undefined, { localGateway, path: '/private/desk.json', present: true, sha256: 'revision-one', text: content, decoded: decodeDeskConfig(content, 'desk') })
  return render(<MemoryRouter><QueryClientProvider client={testQueryClient()}><DeskConfigFixture value={effective}><ConnectionsSettings /></DeskConfigFixture></QueryClientProvider></MemoryRouter>)
}
function openPDF() { fireEvent.click(screen.getByRole('button', { name: 'Manage PDF processing' })) }
function save() { fireEvent.click(screen.getByRole('button', { name: 'Save changes' })) }
function response(revision = 'revision-two') { return Response.json({ path: '/private/desk.json', sha256: revision }) }

it('shows compact personal summaries and exposes only supported actions', () => {
  setup({ gateway: null })
  expect(screen.getByText('Personal · This computer')).toBeTruthy()
  expect(screen.getByRole('button', { name: 'Set up gateway' })).toBeTruthy()
  expect((screen.getByRole('button', { name: 'Manage PDF processing' }) as HTMLButtonElement).disabled).toBe(true)
  expect(screen.queryByRole('textbox')).toBeNull()
  expect(screen.getByText('Not available yet')).toBeTruthy()
  expect(screen.queryByRole('button', { name: /connect.*drive/i })).toBeNull()
})
it('saves PDF processing conditionally and preserves shared research settings', async () => {
  mocks.fetch.mockImplementation(async () => response()); setup(); openPDF()
  fireEvent.click(screen.getByLabelText('Enable PDF processing')); save()
  await screen.findByText('Saved.')
  const sent = JSON.parse(mocks.fetch.mock.calls[0]![1].body)
  expect(sent.ifMatch).toBe('revision-one')
  expect(sent.research.documents).toEqual(DOCUMENT_DEFAULTS)
  expect(sent.research.sources.read.source).toBe('read')
  expect(sent.research.gateway).toEqual(gateway)
  expect(sent.assistant).toBeUndefined()
})
it('retains custom limits and source when disabled, then reuses them when enabled', async () => {
  mocks.fetch.mockImplementation(async () => response())
  const documents = { ...DOCUMENT_DEFAULTS, source: 'my-pdfs', maxFileBytes: 100000, maxRequestBytes: 1048576, maxResponseBytes: 65536 }
  setup({ documents }); openPDF()
  fireEvent.click(screen.getByLabelText('Enable PDF processing')); save()
  await screen.findByText('Saved.')
  expect(JSON.parse(mocks.fetch.mock.calls[0]![1].body).research.documents).toEqual({ ...documents, enabled: false })
  openPDF(); fireEvent.click(screen.getByLabelText('Enable PDF processing')); save()
  await screen.findByText('Saved.')
  const sent = JSON.parse(mocks.fetch.mock.calls[1]![1].body)
  expect(sent.research.documents).toEqual(documents)
  expect(sent.ifMatch).toBe('revision-two')
})
it('keeps input on a stale write, and reload uses the newly read revision and research sources', async () => {
  mocks.fetch.mockResolvedValueOnce(Response.json({ error: 'Configuration changed', code: 'desk-config-changed' }, { status: 409 }))
  setup(); openPDF()
  fireEvent.click(screen.getByText('Advanced settings'))
  fireEvent.change(screen.getByLabelText('Document source name'), { target: { value: 'my-documents' } })
  fireEvent.click(screen.getByLabelText('Enable PDF processing')); save()
  await screen.findByRole('alert')
  expect((screen.getByLabelText('Document source name') as HTMLInputElement).value).toBe('my-documents')
  expect(screen.queryByText('Saved.')).toBeNull()
  mocks.fetch.mockResolvedValueOnce(Response.json({ path: '/private/desk.json', present: true, sha256: 'external-revision', content: JSON.stringify({ deskConfigVersion: 1, research: { ...research, sources: { read: { source: 'changed-source', dialect: 'jina-reader' } } } }) }))
  fireEvent.click(screen.getByRole('button', { name: 'Reload and discard changes' }))
  await waitFor(() => expect(screen.queryByRole('alert')).toBeNull())
  expect((screen.getByLabelText('Document source name') as HTMLInputElement).value).toBe('documents')
  fireEvent.click(screen.getByLabelText('Enable PDF processing'))
  mocks.fetch.mockResolvedValueOnce(response('after-reload')); save()
  await screen.findByText('Saved.')
  const sent = JSON.parse(mocks.fetch.mock.calls[2]![1].body)
  expect(sent.ifMatch).toBe('external-revision')
  expect(sent.research.sources.read.source).toBe('changed-source')
})
it('shows invalid limits beside the input and expands advanced settings before any write', async () => {
  setup(); openPDF()
  fireEvent.click(screen.getByLabelText('Enable PDF processing'))
  fireEvent.change(screen.getByLabelText('Gateway request limit (MiB)'), { target: { value: '0.0625' } })
  save()
  await screen.findByRole('alert')
  expect(screen.getByLabelText('Gateway request limit (MiB)').getAttribute('aria-invalid')).toBe('true')
  expect(screen.getByText('Advanced settings').closest('details')?.open).toBe(true)
  expect(screen.getByText('The request limit must fit the encoded file plus 4096 bytes of metadata.')).toBeTruthy()
  expect(mocks.fetch).not.toHaveBeenCalled()
})
it('sets up the shared gateway without enabling PDF processing', async () => {
  mocks.fetch.mockImplementation(async () => response()); setup({ gateway: null })
  fireEvent.click(screen.getByRole('button', { name: 'Set up gateway' }))
  for (const [label, value] of [['Gateway URL', gateway.url], ['Gateway identity', gateway.authority], ['Verification public key', gateway.signer.public]]) fireEvent.change(screen.getByLabelText(label!), { target: { value } })
  save(); await screen.findByText('Saved.')
  const sent = JSON.parse(mocks.fetch.mock.calls[0]![1].body)
  expect(sent.research.gateway).toEqual(gateway)
  expect(sent.research.documents).toBeUndefined()
})
it('confirms discarding changes and returns focus to the opener', async () => {
  setup(); openPDF()
  fireEvent.click(screen.getByLabelText('Enable PDF processing'))
  fireEvent.click(screen.getByRole('button', { name: 'Cancel' }))
  await screen.findByRole('dialog', { name: 'Discard changes?' })
  fireEvent.click(screen.getByRole('button', { name: 'Keep editing' }))
  expect((screen.getByLabelText('Enable PDF processing') as HTMLInputElement).checked).toBe(true)
  fireEvent.click(screen.getByRole('button', { name: 'Cancel' }))
  fireEvent.click(screen.getByRole('button', { name: 'Discard changes' }))
  await waitFor(() => expect(document.activeElement).toBe(screen.getByRole('button', { name: 'Manage PDF processing' })))
  openPDF(); expect((screen.getByLabelText('Enable PDF processing') as HTMLInputElement).checked).toBe(false)
  expect(mocks.fetch).not.toHaveBeenCalled()
})

it('shows ready local processing and saves PDF preferences without persisting a temporary gateway URL', async () => {
  mocks.fetch.mockImplementation(async () => response())
  setup({ gateway: null }, { status: 'ready', gateway: { ...gateway, signer: { ...gateway.signer, algorithm: 'ed25519' } } })
  expect(screen.getByText('Local processing')).toBeTruthy()
  expect(screen.getByText('Ready')).toBeTruthy()
  openPDF()
  expect((screen.getByLabelText('Enable PDF processing') as HTMLInputElement).checked).toBe(true)
  expect(screen.queryByLabelText('Document source name')).toBeNull()
  expect((screen.getByRole('button', { name: 'Save changes' }) as HTMLButtonElement).disabled).toBe(true)
  fireEvent.click(screen.getByLabelText('Enable PDF processing')); save()
  await screen.findByText('Saved.')
  const sent = JSON.parse(mocks.fetch.mock.calls[0]![1].body)
  expect(sent.research.gateway).toBeNull()
  expect(sent.research.documents.enabled).toBe(false)
  expect(sent.research.sources.read.source).toBe('read')
  expect(JSON.stringify(sent)).not.toContain(gateway.url)
})
it('offers an existing gateway when local components are unavailable without falsely enabling PDF uploads', () => {
  const errors = vi.spyOn(console, 'error').mockImplementation(() => {})
  setup({ gateway: null }, { status: 'unavailable', problem: 'Missing components' })
  expect(screen.getByText('Unavailable')).toBeTruthy()
  expect((screen.getByRole('button', { name: 'Manage PDF processing' }) as HTMLButtonElement).disabled).toBe(true)
  fireEvent.click(screen.getByRole('button', { name: 'Set up gateway' }))
  expect(screen.getByLabelText('Connection').textContent).toBe('Local (automatic)')
  expect(screen.queryByLabelText('Gateway URL')).toBeNull()
  expect((screen.getByRole('button', { name: 'Save changes' }) as HTMLButtonElement).disabled).toBe(true)
  expect(errors).not.toHaveBeenCalled()
})
