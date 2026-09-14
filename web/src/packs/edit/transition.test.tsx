import { cleanup, fireEvent, screen, waitFor, within } from '@testing-library/react'
import { readFileSync } from 'node:fs'
import { join } from 'node:path'
import { afterEach, describe, expect, it, vi } from 'vitest'
import { chassis, drawPack, forgetSlot, served, PACK_DIGEST } from './editHarness'

const original = readFileSync(join(import.meta.dirname, '../__fixtures__/full.pack.json'), 'utf8')
const changed = original.replace('Vendor onboarding', 'Revised onboarding')
const route = '/packs/vendor-onboarding'

afterEach(() => { cleanup(); forgetSlot(); vi.unstubAllGlobals() })

async function editBytes() {
  const area = await screen.findByLabelText<HTMLTextAreaElement>("The document's bytes")
  await waitFor(() => expect(area.readOnly).toBe(false))
  fireEvent.change(area, { target: { value: changed } })
  return area
}

async function leave() {
  fireEvent.click(screen.getByRole('button', { name: 'Back to pack' }))
  return within(await screen.findByRole('dialog', { name: 'Save changes before leaving?' }))
}

describe('view and edit transitions', () => {
  it('pins save and return outside the scrolling body and removes saved-pack navigation while editing', async () => {
    chassis({ content: original, sha256: PACK_DIGEST })
    const { router } = drawPack(served(original), { path: `${route}?view=logic&layout=list&at=%2Frules%2F1` })
    fireEvent.click(await screen.findByRole('button', { name: 'Edit' }))
    const back = await screen.findByRole('button', { name: 'Back to pack' })
    for (const name of ['Back to pack', 'Save', 'Test draft']) {
      const control = screen.getByRole('button', { name })
      expect(control.closest('[data-page-header]')).not.toBeNull()
      expect(control.closest('[data-page-scroll]')).toBeNull()
    }
    expect(screen.queryByRole('link', { name: 'Test pack' })).toBeNull()
    expect(screen.queryByRole('navigation', { name: 'Pack sections' })).toBeNull()
    expect(document.activeElement).toBe(back)
    fireEvent.click(back)
    await waitFor(() => expect(router.state.location.search).toBe('?view=logic&layout=list&at=%2Frules%2F1'))
    expect(screen.queryByRole('dialog')).toBeNull()
    await waitFor(() => expect(document.activeElement).toBe(screen.getByRole('button', { name: 'Edit' })))
  })

  it('keeps edits on cancel and discards only when explicitly returning without saving', async () => {
    const log = chassis({ content: original, sha256: PACK_DIGEST })
    const { router } = drawPack(served(original), { path: `${route}?view=overview&edit=1&shape=json` })
    const area = await editBytes()
    const dialog = await leave()
    fireEvent.keyDown(dialog.getByRole('button', { name: 'Keep editing' }), { key: 's', ctrlKey: true })
    expect(log.writes).toHaveLength(0)
    fireEvent.click(dialog.getByRole('button', { name: 'Keep editing' }))
    expect(area.value).toBe(changed)
    expect(log.writes).toHaveLength(0)
    await waitFor(() => expect(document.activeElement).toBe(screen.getByRole('button', { name: 'Back to pack' })))
    fireEvent.click((await leave()).getByRole('button', { name: 'Discard and return' }))
    await waitFor(() => expect(router.state.location.search).toBe('?view=overview'))
    expect(log.writes).toHaveLength(0)
    fireEvent.click(screen.getByRole('button', { name: 'Edit' }))
    expect(await screen.findByDisplayValue('Vendor onboarding')).toBeTruthy()
  })

  it('returns only after the exact submitted draft has been saved and read back', async () => {
    const log = chassis({ content: original, sha256: PACK_DIGEST, holdWrite: true })
    const { router } = drawPack(served(original), { path: `${route}?view=logic&edit=1&shape=json` })
    await editBytes()
    fireEvent.click((await leave()).getByRole('button', { name: 'Save and return' }))
    await waitFor(() => expect(log.writes).toHaveLength(1))
    expect(log.writes[0].content).toBe(changed)
    expect(router.state.location.search).toContain('edit=1')
    expect(screen.getByRole('button', { name: 'Back to pack' }).hasAttribute('disabled')).toBe(true)
    log.releaseWrite()
    await waitFor(() => expect(router.state.location.search).toBe('?view=logic'))
    expect(screen.getByRole('heading', { level: 1 }).textContent).toContain('Revised onboarding')
  })

  it.each([
    { failWrite: { status: 500, error: 'Disk full' } },
    { staleWith: { sha256: 'f'.repeat(64) } },
    { landsAs: (value: string) => `${value}\n` }
  ])('retains the draft when save fails or cannot be verified: %j', async options => {
    const log = chassis({ content: original, sha256: PACK_DIGEST, ...options })
    const { router } = drawPack(served(original), { path: `${route}?edit=1&shape=json` })
    const area = await editBytes()
    fireEvent.click((await leave()).getByRole('button', { name: 'Save and return' }))
    await waitFor(() => expect(log.writes).toHaveLength(1))
    await waitFor(() => expect(screen.getByRole('button', { name: 'Save' }).hasAttribute('disabled')).toBe(false))
    expect(router.state.location.search).toContain('edit=1')
    expect(area.value).toBe(changed)
    const header = area.closest('[data-layout="page"]')!.querySelector('[data-page-header]')!
    expect(header.textContent).toMatch(/Could not save|Save conflict|Save could not be verified/)
  })

  it('keeps editing if additional work arrives while Save and return is running', async () => {
    const log = chassis({ content: original, sha256: PACK_DIGEST, holdWrite: true })
    const { router } = drawPack(served(original), { path: `${route}?edit=1&shape=json` })
    const area = await editBytes()
    fireEvent.click((await leave()).getByRole('button', { name: 'Save and return' }))
    await waitFor(() => expect(log.writes).toHaveLength(1))
    fireEvent.change(area, { target: { value: `${changed}\n` } })
    log.releaseWrite()
    await screen.findByText(/Saved, and verified/)
    expect(router.state.location.search).toContain('edit=1')
    expect(area.value).toBe(`${changed}\n`)
    expect(screen.getByText('Editing · Unsaved changes')).toBeTruthy()
  })

  it('does not silently drop unfinished operand text on Save and return', async () => {
    const doc = JSON.parse(original)
    doc.rules[0].when = { op: 'fact', path: '/case/tag', operator: 'equals', value: 'green' }
    const source = JSON.stringify(doc)
    const log = chassis({ content: source, sha256: PACK_DIGEST })
    drawPack(served(source), { path: `${route}?edit=1` })
    const operand = await screen.findByDisplayValue('"green"')
    fireEvent.change(operand, { target: { value: '{"unfinished"' } })
    const dialog = await leave()
    expect(dialog.getByRole('button', { name: 'Save and return' }).hasAttribute('disabled')).toBe(true)
    expect(dialog.getByText(/Finish or discard unfinished fields/)).toBeTruthy()
    fireEvent.keyDown(dialog.getByRole('button', { name: 'Keep editing' }), { key: 'Escape' })
    await waitFor(() => expect(screen.queryByRole('dialog')).toBeNull())
    expect((operand as HTMLInputElement).value).toBe('{"unfinished"')
    expect(log.writes).toHaveLength(0)
  })
})
