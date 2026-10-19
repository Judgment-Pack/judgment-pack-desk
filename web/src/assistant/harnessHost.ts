import { configureHarness } from '@judgment-pack/harness/contract'
import * as replyLanguage from '../i18n/assistantLanguage'
import * as locales from '../i18n/locales'
import * as session from '../mcp/session'
import * as files from '../files/client'
import * as scope from '../desks/scope'
import * as connections from './aiConnections'
import * as provider from '../mcp/McpProvider'
import * as transport from '../mcp/transport'
import * as dialects from '../dialects/client'
import * as capabilities from './endpointCapabilities'

/**
 * The Desk as the harness's host: every injection point of
 * `@judgment-pack/harness` v0.1.0 (its `host.ts`) supplied once, from the Desk module
 * the harness read it from before it moved into the package, under the same
 * name. Importing this module is what wires the host: the page's entry
 * (`main.tsx`) imports it before anything starts a run, and the test ground
 * (`testing/setup.ts`) imports it before every test.
 *
 * **Each function is read through its module when it is called**, not when
 * the host is wired, as the harness's own imports of these modules read them
 * when it lived in this tree. On the page that is the same function. Under
 * test it is whatever the test file made of the module, its own mock
 * included, and a mock that leaves a member out fails only a call that
 * reaches it. The two classes and the sentence are values, read once.
 *
 * Nothing here changes what the Desk keeps: the session and its bearer, the
 * relay, the tool gate's allow-list and approvals stay the Desk's, and the
 * harness reaches them only through these values.
 *
 * Two points are new with v0.1.0 and are answered from
 * `assistant/endpointCapabilities.ts`: `endpointCapabilities`, which answers
 * nothing beyond each endpoint's family, and `renderImageToText`, which the
 * Desk refuses for want of a render adapter.
 */
configureHarness({
  assistantLanguageInstructions: (locale) =>
    replyLanguage.assistantLanguageInstructions(locale as locales.Language | undefined),
  currentLanguage: () => locales.currentLanguage(),
  sessionBearer: () => session.sessionBearer(),
  sessionEnded: () => session.sessionEnded(),
  whenSessionEnds: (listener) => session.whenSessionEnds(listener),
  forgetSession: (reason) => session.forgetSession(reason),
  refusalCode: (answered) => session.refusalCode(answered),
  discardBody: (answered) => session.discardBody(answered),
  NoSession: session.NoSession,
  NO_SESSION_MESSAGE: session.NO_SESSION_MESSAGE,
  chassisUrl: (path, params) => files.chassisUrl(path, params),
  deskHeaders: () => scope.deskHeaders(),
  deskSocketURL: (url) => scope.deskSocketURL(url),
  aiHeaders: (id, revision) => connections.aiHeaders(id, revision),
  socketURL: () => provider.socketURL(),
  socketProtocols: (id) => provider.socketProtocols(id),
  DeskWebSocketTransport: transport.DeskWebSocketTransport,
  deskFetch: (input, init) => files.deskFetch(input, init),
  answer: <T,>(response: Response) => files.answer<T>(response),
  readFile: (path, signal) => files.readFile(path, signal),
  dialectAPI: <T,>(path?: string, method?: string, body?: unknown, revision?: string, signal?: AbortSignal) =>
    dialects.dialectAPI<T>(path, method, body, revision, signal),
  endpointCapabilities: (endpoint) => capabilities.endpointCapabilities(endpoint),
  renderImageToText: () => capabilities.renderImageToText()
})
