import { act, cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react'
import { Tooltip } from 'radix-ui'
import { afterEach, beforeEach, expect, it, vi } from 'vitest'
import { SourceInputFields } from './SourceInputFields'
import { jobsAPI } from './client'
import { localSnapshot } from './sourceInputs'
import { authorizeDrive } from '../connections/client'
import { ingestDrive } from '../documents/client'
import { DeskConfigFixture } from '../config/DeskConfigProvider'
import { effectiveConfig } from '../config/deskConfig'
import type { PackDocument } from '../mcp/types'
import { signedInput } from './__fixtures__/signedInput'
vi.mock('./client', () => ({ jobsAPI: vi.fn() }))
vi.mock('../connections/client', async original => ({ ...await original<object>(), authorizeDrive: vi.fn() }))
vi.mock('../documents/client', async original => ({ ...await original<object>(), ingestDrive: vi.fn() }))
vi.mock('./sourceInputs', async original => ({ ...await original<object>(), localSnapshot: vi.fn() }))
const doc = { rules: [{ condition: { op: 'fact', path: '/request/type', value: 'data-access' } }], evidenceRequirements: [{ id: 'receipt', description: 'Receipt' }] } as unknown as PackDocument
const mapping = { version: 1 as const, provider: 'local-file' as const, facts: [{ target: '/request/type', source: '/request/type' }], evidence: [] }
let snapshot: Awaited<ReturnType<typeof localSnapshot>>
beforeEach(async () => { snapshot = (await signedInput()).object; delete snapshot.proof; snapshot.selectedAt = '2026-09-25T12:00:00Z'; vi.mocked(localSnapshot).mockResolvedValue(snapshot); vi.mocked(jobsAPI).mockImplementation(async (_path, body) => ({ input: body, factsText: '{"request":{"type":"data-access"}}', evidenceText: '' })) })
afterEach(() => { cleanup(); vi.resetAllMocks() })
function view(onChange = vi.fn(), fixed = false) { return { onChange, ...render(<Tooltip.Provider><SourceInputFields doc={doc} provider="local-file" fixed={fixed ? mapping : undefined} disabled={false} onChange={onChange} /></Tooltip.Provider>) } }
async function choose() { fireEvent.change(screen.getByLabelText('Choose JSON file'), { target: { files: [new File(['{}'], 'input.json')] } }); await screen.findByText('input.json') }
it('requires an explicit mapping preview and clears approval on mapping or file changes', async () => {
 const { onChange } = view(); await choose(); expect(jobsAPI).not.toHaveBeenCalled()
 fireEvent.click(screen.getByRole('button', { name: 'Preview mapping' })); await screen.findByText('Mapped inputs')
 expect(onChange).toHaveBeenLastCalledWith(expect.objectContaining({ snapshot, mapping }))
 expect(jobsAPI).toHaveBeenCalledWith('inputs/preview', { source: { snapshot, mapping } }, undefined, expect.any(AbortSignal))
 fireEvent.change(screen.getByLabelText('request · type'), { target: { value: '/different' } })
 expect(onChange).toHaveBeenLastCalledWith(undefined); expect(screen.queryByText('Mapped inputs')).toBeNull()
 await choose(); expect(onChange).toHaveBeenLastCalledWith(undefined)
})
it('a frozen mapping cannot be changed and starts without a sample file', async () => {
 view(vi.fn(), true); expect(screen.queryByRole('button', { name: 'Preview mapping' })).toBeNull(); await choose()
 expect((screen.getByLabelText('request · type') as HTMLInputElement).closest('fieldset')?.disabled).toBe(true)
 expect(screen.getByText(/Select a new file for every run/)).toBeTruthy()
})
it('ignores a late preview after unmount', async () => {
 let resolve!: (value: unknown) => void
 vi.mocked(jobsAPI).mockReturnValue(new Promise(r => { resolve = r }))
 const { onChange, unmount } = view(); await choose(); fireEvent.click(screen.getByRole('button', { name: 'Preview mapping' }))
 await waitFor(() => expect(jobsAPI).toHaveBeenCalled()); unmount()
 await act(async () => resolve({ input: { source: { snapshot, mapping } }, factsText: '{}' }))
 expect(onChange).not.toHaveBeenCalledWith(expect.objectContaining({ snapshot }))
})
it('reports a refused mapping and never enables dependent submission', async () => {
 vi.mocked(jobsAPI).mockRejectedValue(Error('Mapped evidence must use availability words'))
 const { onChange } = view(); await choose(); fireEvent.click(screen.getByRole('button', { name: 'Preview mapping' }))
 expect((await screen.findByRole('alert')).textContent).toContain('Mapped evidence')
 expect(onChange).toHaveBeenLastCalledWith(undefined)
})
it('opens the explicit Drive picker, verifies the receipt and submits only retained source inputs', async () => {
 const { object, pin } = await signedInput(), config = effectiveConfig(undefined), onChange = vi.fn()
 config.config.research = { ...config.config.research, gateway: pin, documents: { enabled: true, source: 'documents', maxFileBytes: 200000, maxRequestBytes: 1000000, maxResponseBytes: 1000000 } }
 vi.mocked(authorizeDrive).mockResolvedValue([object.proof!.drive!]); vi.mocked(ingestDrive).mockResolvedValue({ document: { object } } as Awaited<ReturnType<typeof ingestDrive>>)
 render(<DeskConfigFixture value={config}><Tooltip.Provider><SourceInputFields doc={doc} provider="google-drive" disabled={false} onChange={onChange} /></Tooltip.Provider></DeskConfigFixture>)
 fireEvent.click(screen.getByRole('button', { name: 'Choose from Google Drive' })); await screen.findByText('input.json')
 expect(authorizeDrive).toHaveBeenCalledWith('pick', expect.any(AbortSignal))
 fireEvent.click(screen.getByRole('button', { name: 'Preview mapping' })); await screen.findByText('Mapped inputs')
 expect(jobsAPI).toHaveBeenLastCalledWith('inputs/preview', { source: { snapshot: object, mapping: { ...mapping, provider: 'google-drive' } } }, undefined, expect.any(AbortSignal))
})
