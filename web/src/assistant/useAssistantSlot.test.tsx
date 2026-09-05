/**
 * The capability surface chunk 2 will read.
 *
 * **Nothing renders a tab, and this suite asserts that too.** The hook exists
 * so that "is there an assistant on this desk" has one answer rather than a
 * configuration read and a key read reassembled at each call site — and the
 * value of pinning it now, before anything consumes it, is that the states it
 * offers are decided here rather than by whatever the first consumer found
 * convenient.
 */
import { QueryClientProvider } from '@tanstack/react-query'
import { cleanup, render, screen, waitFor } from '@testing-library/react'
import { readFileSync, readdirSync } from 'node:fs'
import { join } from 'node:path'
import { afterEach, describe, expect, it, vi } from 'vitest'
import { DeskConfigFixture } from '../config/DeskConfigProvider'
import { decodeDeskConfig, effectiveConfig, type EffectiveConfig } from '../config/deskConfig'
import { testQueryClient } from '../testing/harness'
import { useAssistantSlot } from './useAssistantSlot'

afterEach(() => {
  cleanup()
  vi.unstubAllGlobals()
})

const ENDPOINT = {
  url: 'https://api.example.invalid/v1',
  kind: 'openai-compatible',
  model: 'a-model',
  tools: ['validate']
}

function withEndpoint(endpoint: unknown): EffectiveConfig {
  return effectiveConfig(undefined, undefined, undefined, {
    path: '/home/someone/.config/jpack-desk/desk.json',
    present: true,
    decoded: decodeDeskConfig(
      JSON.stringify({ deskConfigVersion: 1, assistant: { endpoint } }),
      'desk'
    )
  })
}

/** The hook's reading, painted so a test can read it back. */
function Reading() {
  const slot = useAssistantSlot()
  return (
    <output>
      {slot.state}|{slot.endpoint?.model ?? 'no endpoint'}|{slot.keyPresent ? 'key' : 'no key'}
    </output>
  )
}

function renderSlot(value: EffectiveConfig, keyPresent: boolean) {
  vi.stubGlobal('fetch', async () => ({
    ok: true,
    status: 200,
    statusText: '',
    text: async () =>
      JSON.stringify({ present: keyPresent, fingerprint: keyPresent ? 'sk-a…wxyz' : '' })
  }))
  return render(
    <QueryClientProvider client={testQueryClient()}>
      <DeskConfigFixture value={value}>
        <Reading />
      </DeskConfigFixture>
    </QueryClientProvider>
  )
}

describe('useAssistantSlot', () => {
  it('reads none where no endpoint is configured', async () => {
    renderSlot(effectiveConfig(undefined), false)
    await waitFor(() => expect(screen.getByRole('status').textContent).toBe('none|no endpoint|no key'))
  })

  it('reads configured, and hands over the endpoint itself', async () => {
    renderSlot(withEndpoint(ENDPOINT), true)
    await waitFor(() =>
      expect(screen.getByRole('status').textContent).toBe('configured|a-model|key')
    )
  })

  it('separates a configured endpoint from a stored key', async () => {
    // **Not folded together.** "An endpoint with no key yet" is a real state
    // that a consumer has to be able to report, and collapsing it into `none`
    // would have the future pane say there is no assistant configured when
    // there is one and it is a paste away from working.
    renderSlot(withEndpoint(ENDPOINT), false)
    await waitFor(() =>
      expect(screen.getByRole('status').textContent).toBe('configured|a-model|no key')
    )
  })

  it('says no key while the read has not answered, rather than guessing', () => {
    vi.stubGlobal('fetch', () => new Promise(() => {}))
    render(
      <QueryClientProvider client={testQueryClient()}>
        <DeskConfigFixture value={withEndpoint(ENDPOINT)}>
          <Reading />
        </DeskConfigFixture>
      </QueryClientProvider>
    )
    // The honest reading: the page has not been told there is a key. Nothing
    // gates on this — the desk refuses a probe with no key by name, which is
    // where that decision belongs.
    expect(screen.getByRole('status').textContent).toBe('configured|a-model|no key')
  })

  it('is read by nothing that renders a tab', () => {
    // Chunk 1 ships the slot and no pane. A tab appearing here would be a
    // feature nobody reviewed arriving with the configuration for it.
    const src = join(import.meta.dirname, '..')
    const offenders: string[] = []
    const walk = (relative: string) => {
      for (const entry of readdirSync(join(src, relative), { withFileTypes: true })) {
        const path = relative === '' ? entry.name : `${relative}/${entry.name}`
        if (entry.isDirectory()) {
          walk(path)
          continue
        }
        if (!entry.name.endsWith('.ts') && !entry.name.endsWith('.tsx')) continue
        if (path.includes('.test.')) continue
        if (path === 'assistant/useAssistantSlot.ts') continue
        if (readFileSync(join(src, path), 'utf8').includes('useAssistantSlot')) {
          offenders.push(path)
        }
      }
    }
    walk('')
    expect(offenders, 'the slot has a consumer already').toEqual([])
  })
})
