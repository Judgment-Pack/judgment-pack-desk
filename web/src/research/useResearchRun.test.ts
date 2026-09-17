import { describe, expect, it } from 'vitest'
import { researchBlockedReason } from './useResearchRun'

const ready = {
  slot: { state: 'ready', endpoint: 'https://example.invalid', keyStatus: 'stored', keyPresent: true },
  modelPicked: true,
  advertised: true,
  authorPromptRead: true,
  research: { gateway: { url: 'http://127.0.0.1:8787' }, sources: { search: {}, read: {} } },
  mcp: { status: 'ready', client: {}, validateSupported: true, expectationValidationSupported: true }
}

describe('why research cannot start', () => {
  it('says nothing when everything a run needs is present', () => {
    expect(researchBlockedReason(ready)).toBe('')
  })

  it('names the runtime build when the admission check is not served', () => {
    // Without this, a run starts, spends a research turn and a reviewer turn on
    // the gateway and the model, and only then fails closed on a tool the
    // runtime never had.
    const reason = researchBlockedReason({ ...ready, mcp: { ...ready.mcp, expectationValidationSupported: false } })
    expect(reason).toContain('experimental_validate_expectations')
  })

  it.each([
    [{ slot: { ...ready.slot, keyPresent: false } }, 'No API key'],
    [{ modelPicked: false }, 'Choose an enabled model'],
    [{ advertised: false }, 'does not offer the authoring prompt'],
    [{ research: { gateway: null, sources: ready.research.sources } }, 'No research gateway'],
    [{ mcp: { ...ready.mcp, validateSupported: false } }, 'does not serve validate'],
    [{ mcp: { ...ready.mcp, status: 'connecting' } }, 'connection is not ready']
  ])('names what is missing: %#', (override, expected) => {
    expect(researchBlockedReason({ ...ready, ...override })).toContain(expected)
  })
})
it('lets ordinary chat answer when authoring and runtime checks are unavailable', () => {
  expect(researchBlockedReason({ ...ready, mode: 'draft', advertised: false, authorPromptRead: false,
    mcp: { status: 'failed', client: null, validateSupported: false, expectationValidationSupported: false } })).toBe('')
  expect(researchBlockedReason({ ...ready, mode: 'draft', slot: { ...ready.slot, keyPresent: false } })).toContain('No API key')
})
