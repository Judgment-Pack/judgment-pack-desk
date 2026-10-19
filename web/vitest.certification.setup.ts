// The ground the harness certification runs on (see vitest.certification.config.mts).
//
// The Desk's test ground (`src/testing/setup.ts`) wires the harness to the Desk
// in a `beforeEach`. The certification suite also prepares its scenarios in
// `beforeAll`, which runs before any `beforeEach`, so the Desk's wiring is
// imported here at load time as well: every injection point the suite reads is
// the one `src/assistant/harnessHost.ts` supplies, from the first hook on, and
// the page has its session in hand (the Desk's `beforeEach` gives it too late
// for a `beforeAll`).
import { afterEach, expect } from 'vitest'
import { giveThisPageASessionForTesting } from './src/mcp/session'
import './src/assistant/harnessHost'
import { isProviderOptionsKeyWarning, recordSdkWarnings, takeSdkWarnings } from '@judgment-pack/harness/testing/sdkWarnings'

// The AI SDK's deprecation of a provider-options key fails the test that caused it,
// as it does in the package's own ground.
giveThisPageASessionForTesting('a-session-this-test-was-given')
recordSdkWarnings()
afterEach(() => {
  expect(takeSdkWarnings().filter(isProviderOptionsKeyWarning)).toEqual([])
})
