import {StrictMode} from 'react'
import {act, cleanup, fireEvent, render, screen, waitFor} from '@testing-library/react'
import {Tooltip} from 'radix-ui'
import {afterEach, beforeEach, expect, it, vi} from 'vitest'
import {DIALECT_RETAINED_COLUMN_DRAFT} from '@judgment-pack/harness/dialects'
import {RetainedRecordReview} from './RetainedRecordReview'
import {RetainedReviewController} from './retainedSelection/controller'
import {retainedSelectionClient, RetainedReviewError, type RetainedReviewView, type RetainedSelectionClient, type ExactRetainedInput} from './retainedSelection/client'
import type {RetainedSelectionSource} from './retainedSelection/wire'
import {forgetSession, giveThisPageASessionForTesting, NoSession} from '../mcp/session'
vi.mock('./retainedSelection/client', async original => ({...await original<typeof import('./retainedSelection/client')>(), retainedSelectionClient: vi.fn()}))
beforeEach(() => giveThisPageASessionForTesting('a'.repeat(48)))
afterEach(() => {cleanup(); for (const opener of openers.splice(0)) opener.remove(); vi.restoreAllMocks(); vi.resetAllMocks()})
const openers: HTMLButtonElement[] = []
const pack = 'sha256:' + 'a'.repeat(64), digest = 'sha256:' + 'b'.repeat(64)
const source: RetainedSelectionSource = {preparationId: 'prep', revision: 'saved', packDigest: pack, conversionContract: DIALECT_RETAINED_COLUMN_DRAFT, origin: 'asserted', recordIds: ['row-0', 'row-1'], requestBytes: 1}
const view: RetainedReviewView = {header: {wireVersion: 1, viewId: 'rv_' + '1'.repeat(48), preparationId: 'prep', revision: 'saved', packDigest: pack, conversionContract: DIALECT_RETAINED_COLUMN_DRAFT, origin: 'asserted', rows: 2, maxInputBytes: 37748736, selectionSha256: '2'.repeat(64)}, rows: [0, 1].map(index => ({index, id: 'row-' + index, inputBytes: 1, inputSha256: '3'.repeat(64), offset: index})), indexSha256: '4'.repeat(64), inputBytesTotal: 2}
function input(index: number): ExactRetainedInput {
 const q = (text: string) => JSON.stringify(text), tuple = JSON.stringify(DIALECT_RETAINED_COLUMN_DRAFT)
 const contract = tuple.slice(0, -1) + ',"roles":["document-validation","preview","preparation"]}'
 const custody = '{"values":{},"prepared":{},"origin":{"preparationId":"prep","recordId":"row-' + index + '","packDigest":' + q(pack) + ',"sourceId":"original","sourceDigest":' + q(pack) + ',"row":1,"importedAt":"kept","importRecipeVersion":1,"recipeVersion":1,"conversions":{},"originalValues":{}},"retained":{"contractVersion":"1","origin":"asserted","values":{},"research":null}}'
 const text = '{"facts":{"value":"private payload ' + index + '"},"source":{"conversionContract":' + contract + ',"mapping":{},"mappingDigest":' + q(digest) + ',"retainedCaseJson":' + q(custody) + '},"preparation":{"conversionContract":' + contract + ',"version":2,"verifiedAt":"2026-10-01T00:00:00Z","mappingDigest":' + q(digest) + ',"verification":"kept","cites":[],"lineage":[],"outcomes":[]}}'
 return {text, sourceRevision: 'saved', rowId: 'row-' + index, index, inputSha256: view.rows[index]!.inputSha256}
}
function deferred<T>() {let resolve!: (value: T) => void; const promise = new Promise<T>(yes => {resolve = yes}); return {promise, resolve}}
function setup(strict = false) {
 const client = {current: vi.fn<RetainedSelectionClient['current']>().mockResolvedValue(undefined), prepare: vi.fn<RetainedSelectionClient['prepare']>().mockResolvedValue(view), read: vi.fn<RetainedSelectionClient['read']>().mockImplementation(async (_view, index) => input(index)), close: vi.fn<RetainedSelectionClient['close']>().mockResolvedValue(undefined)}
 vi.mocked(retainedSelectionClient).mockResolvedValue(client as unknown as RetainedSelectionClient)
 const close = vi.fn(), opener = document.createElement('button'); opener.textContent = 'Open review'; document.body.append(opener); openers.push(opener); opener.focus()
 const element = (revision = 'saved') => <Tooltip.Provider><RetainedRecordReview source={source} currentRevision={revision} opener={{current: opener}} onClose={close}/></Tooltip.Provider>
 const rendered = render(strict ? <StrictMode>{element()}</StrictMode> : element())
 return {...rendered, client, close, opener, element}
}
async function prepare() {fireEvent.click(screen.getByRole('button', {name: 'Prepare review'})); await screen.findByRole('combobox', {name: 'Record'})}
function select(index: number) {fireEvent.click(screen.getByRole('combobox', {name: 'Record'})); fireEvent.click(screen.getByRole('option', {name: `${index + 1} · row-${index}`}))}

it('StrictMode idle/focus/reconnect does no work, double Prepare shares capture, and initial readiness does not mount', async () => {
 const {client} = setup(true)
 fireEvent.focus(window); fireEvent(window, new Event('online'))
 expect(retainedSelectionClient).not.toHaveBeenCalled(); expect(client.prepare).not.toHaveBeenCalled(); expect(client.read).not.toHaveBeenCalled()
 const pending = deferred<RetainedSelectionClient>(); vi.mocked(retainedSelectionClient).mockReturnValue(pending.promise)
 const button = screen.getByRole('button', {name: 'Prepare review'}); fireEvent.click(button); fireEvent.click(button)
 expect(retainedSelectionClient).toHaveBeenCalledTimes(1)
 expect(screen.queryByRole('combobox', {name: 'Record'})).toBeNull()
 await act(async () => pending.resolve(client as unknown as RetainedSelectionClient))
 await screen.findByRole('combobox', {name: 'Record'}); expect(client.prepare).toHaveBeenCalledTimes(1); expect(client.read).not.toHaveBeenCalled()
})
it('closes and disposes a late asynchronous capture without preparation', async () => {
 const {client, close} = setup(); const dispose = vi.spyOn(RetainedReviewController.prototype, 'dispose')
 const pending = deferred<RetainedSelectionClient>(); vi.mocked(retainedSelectionClient).mockReturnValue(pending.promise)
 fireEvent.click(screen.getByRole('button', {name: 'Prepare review'})); fireEvent.click(screen.getByRole('button', {name: 'Cancel'}))
 await act(async () => pending.resolve(client as unknown as RetainedSelectionClient))
 expect(close).toHaveBeenCalledTimes(1); expect(dispose).toHaveBeenCalledTimes(1); expect(client.prepare).not.toHaveBeenCalled(); expect(client.read).not.toHaveBeenCalled()
})
it('cancels preparation and unmounts pending capture without late publication', async () => {
 const current = setup(); const pending = deferred<RetainedReviewView>(); current.client.prepare.mockReturnValue(pending.promise)
 fireEvent.click(screen.getByRole('button', {name: 'Prepare review'})); await waitFor(() => expect(current.client.prepare).toHaveBeenCalledTimes(1))
 const signal = current.client.prepare.mock.calls[0]![1] as AbortSignal
 fireEvent.click(screen.getByRole('button', {name: 'Cancel'})); expect(signal.aborted).toBe(true)
 await act(async () => pending.resolve(view)); expect(current.client.close).toHaveBeenCalledWith(view); expect(screen.queryByRole('combobox', {name: 'Record'})).toBeNull()
 current.unmount()
 const next = setup(); const capture = deferred<RetainedSelectionClient>(); vi.mocked(retainedSelectionClient).mockReturnValue(capture.promise)
 fireEvent.click(screen.getByRole('button', {name: 'Prepare review'})); next.unmount()
 await act(async () => capture.resolve(next.client as unknown as RetainedSelectionClient)); expect(next.client.prepare).not.toHaveBeenCalled()
})
it('mounts only explicit rows, drops prior payload during supersession, and never restores a late row', async () => {
 const {client} = setup(); await prepare(); select(0); await screen.findByText('"private payload 0"')
 const pending = deferred<ExactRetainedInput>(); client.read.mockReturnValueOnce(pending.promise)
 fireEvent.click(screen.getByRole('button', {name: 'Next record'}))
 expect(screen.queryByText('"private payload 0"')).toBeNull()
 fireEvent.click(screen.getByRole('button', {name: 'Previous record'})); await screen.findByText('"private payload 0"')
 await act(async () => pending.resolve(input(1)))
 expect(screen.queryByText('"private payload 1"')).toBeNull(); expect(client.prepare).toHaveBeenCalledTimes(1)
 expect(client.read.mock.calls.map(call => call[1])).toEqual([0, 1, 0])
})
it.each(['review-not-ready', 'resource'] as const)('Read again after %s calls only mount of the same row', async reason => {
 const {client} = setup(); await prepare(); client.read.mockRejectedValueOnce(new RetainedReviewError(reason, 409)); select(1)
 const retry = await screen.findByRole('button', {name: 'Read again'}); expect(screen.queryByText('"private payload 1"')).toBeNull()
 fireEvent.click(retry); await screen.findByText('"private payload 1"')
 expect(client.prepare).toHaveBeenCalledTimes(1); expect(client.read.mock.calls.map(call => call[1])).toEqual([1, 1])
})
it('reader failure uses read-only retry and does not show stale partial input', async () => {
 const {client} = setup(); await prepare(); client.read.mockResolvedValueOnce({...input(0), text: '{"facts":{}}'}); select(0)
 await screen.findByText('This record could not be displayed. Read it again.')
 fireEvent.click(screen.getByRole('button', {name: 'Read again'})); await screen.findByText('"private payload 0"'); expect(client.prepare).toHaveBeenCalledTimes(1)
})
it('expired and initial failures wait for explicit new preparation; source changes block it', async () => {
 const current = setup(); current.client.prepare.mockRejectedValueOnce(new RetainedReviewError('resource'))
 fireEvent.click(screen.getByRole('button', {name: 'Prepare review'})); await screen.findByText('This review is temporarily unavailable.')
 expect(current.client.prepare).toHaveBeenCalledTimes(1)
 fireEvent.click(screen.getByRole('button', {name: 'Prepare review again'})); await screen.findByRole('combobox', {name: 'Record'})
 current.client.read.mockRejectedValueOnce(new RetainedReviewError('expired')); select(0)
 await screen.findByText('This review is no longer available. Prepare it again to continue.')
 expect(current.client.prepare).toHaveBeenCalledTimes(2)
 current.rerender(current.element('changed'))
 expect((screen.getByRole('button', {name: 'Prepare review again'}) as HTMLButtonElement).disabled).toBe(true)
 expect(screen.getByText('saved')).toBeTruthy(); expect(current.client.prepare).toHaveBeenCalledTimes(2)
})
it('session end drops mounted content and exposes no authenticated retry', async () => {
 setup(); await prepare(); select(0); await screen.findByText('"private payload 0"')
 act(() => forgetSession())
 expect(screen.queryByText('"private payload 0"')).toBeNull(); expect(screen.queryByRole('button', {name: 'Read again'})).toBeNull(); expect(screen.queryByRole('button', {name: 'Prepare review again'})).toBeNull()
})
it('Escape closes, aborts a row read, and restores focus to its opener', async () => {
 const current = setup(); await prepare(); const pending = deferred<ExactRetainedInput>(); current.client.read.mockReturnValueOnce(pending.promise); select(0)
 const signal = current.client.read.mock.calls[0]![2] as AbortSignal
 fireEvent.keyDown(document, {key: 'Escape'})
 expect(current.close).toHaveBeenCalledTimes(1); expect(signal.aborted).toBe(true)
 current.unmount(); await waitFor(() => expect(document.activeElement).toBe(current.opener))
 await act(async () => pending.resolve(input(0))); expect(screen.queryByText('"private payload 0"')).toBeNull()
})

it('capture session failure offers no authenticated preparation retry', async () => {
 setup(); vi.mocked(retainedSelectionClient).mockRejectedValueOnce(new NoSession())
 fireEvent.click(screen.getByRole('button', {name: 'Prepare review'}))
 await screen.findByText('Your session ended. Sign in again to continue.')
 expect(screen.queryByRole('button', {name: 'Prepare review'})).toBeNull(); expect(screen.queryByRole('button', {name: 'Prepare review again'})).toBeNull()
})
