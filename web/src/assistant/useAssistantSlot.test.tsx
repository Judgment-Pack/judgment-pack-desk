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

/** One reading component, over a configuration, with the key read stubbed. */
function renderSettings(value: EffectiveConfig, Component: () => React.JSX.Element) {
  vi.stubGlobal('fetch', async () => ({
    ok: true,
    status: 200,
    statusText: '',
    text: async () => JSON.stringify({ present: false, fingerprint: '' })
  }))
  return render(
    <QueryClientProvider client={testQueryClient()}>
      <DeskConfigFixture value={value}>
        <Component />
      </DeskConfigFixture>
    </QueryClientProvider>
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

  it('reports the engine and the tier, defaulted where the file says nothing', async () => {
    // **Defaulted rather than optional.** "The file said nothing" and "the
    // file said vercel" describe the same desk, and a consumer that had to
    // tell them apart would be a consumer inventing a fourth state.
    function Settings() {
      const slot = useAssistantSlot()
      return (
        <output>
          {slot.engine}|{slot.thinking}
        </output>
      )
    }
    const render1 = renderSettings(effectiveConfig(undefined), Settings)
    await waitFor(() => expect(screen.getByRole('status').textContent).toBe('vercel|off'))
    render1.unmount()

    renderSettings(
      effectiveConfig(undefined, undefined, undefined, {
        path: '/home/someone/.config/jpack-desk/desk.json',
        present: true,
        decoded: decodeDeskConfig(
          JSON.stringify({
            deskConfigVersion: 1,
            assistant: { endpoint: null, engine: 'vercel', thinking: 'ultra' }
          }),
          'desk'
        )
      }),
      Settings
    )
    await waitFor(() => expect(screen.getByRole('status').textContent).toBe('vercel|ultra'))
  })

  it('is read only by the surfaces that render an assistant', () => {
    // It used to be *no* consumer: the slot shipped a chunk before anything
    // rendered an assistant, and this asserted the absence. Then it was one,
    // the pane. It is two now — the pane and the Create dialog's Describe
    // section — and the claim being held is the same one in its next form.
    //
    // **What is held is the list, not the count.** "One hook, so that the
    // question has a single answer" is a claim about there being one *reading*
    // of the configuration and the key, and two components calling this hook
    // is that hook doing its job. What would break it is a third place
    // assembling its own answer out of `config.assistant` and the key read —
    // so every reader is enumerated here, and a new one is a line somebody
    // had to write on purpose rather than a habit that spread.
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
    expect(offenders, 'the slot is read somewhere new').toEqual([
      'assistant/AssistantPane.tsx',
      // Admin joined the list on purpose. It is where a reader goes to find
      // out *why* an assistant is not running, so it was the one surface still
      // asserting an absence — "none — no endpoint configured" — over a file
      // this desk had just said it could not read. One reading, three readers.
      'assistant/AssistantSection.tsx',
      'shell/DescribeIt.tsx'
    ])
  })
})
