/**
 * The page's binding of the authoring run to this desk: the assistant slot
 * for the engine, the runtime connection for checks, the research
 * configuration for the gateway, and the Console for activity.
 *
 * **The turn port is the run hook's start, made repeatable.** `useAssistantRun`
 * owns one run per start and the Create flow needs one; this run needs a
 * research turn, a reviewer turn and as many repair and conversation turns as
 * the budget allows, each its own engine session on its own gated connection,
 * so the same primitives — the session bearer, `openAssistantConnection`,
 * `loadEngine`, `runAssistantSession`, `bindModelCall`, `normalize` — are
 * composed here per turn. Nothing new reaches an engine: the same session
 * contract, with the desk's host tools in their slot.
 */
import { useMemo, useRef, useSyncExternalStore } from 'react'
import { loadEngine } from '../assistant/engines'
import type { AssistantEvent, CallTool, McpToolResult } from '../assistant/engine'
import { usePickedModel } from '../assistant/pickedModel'
import { bindModelCall, openAssistantConnection, runAssistantSession } from '../assistant/session'
import { normalize } from '../assistant/thinking'
import { useAssistantSlot } from '../assistant/useAssistantSlot'
import { useEffectiveConfig } from '../config/DeskConfigProvider'
import { useFileListing } from '../files/queries'
import { useMcp } from '../mcp/McpProvider'
import { AUTHOR_PACK_PROMPT, TEST_PACK_PROMPT, usePromptNames, usePromptText } from '../mcp/prompts'
import { sessionBearer } from '../mcp/session'
import { recordActivity } from '../shell/consoleLog'
import { acquire, newResearchSession, registry, seal } from './gatewayClient'
import { Ledger, type SourceRecord } from './ledger'
import { AuthoringRun, INITIAL_STATE, type RunState, type TurnRequest } from './run'
import { researchTools } from './tools'

export const MAX_REVISIONS = 4

export interface ResearchRunBinding {
  run: AuthoringRun | null
  state: RunState
  ledger: Ledger | null
  sources: readonly SourceRecord[]
  /** Why a run cannot start, or '' where it can. */
  blocked: string
  /** The model this run would use, where one is enabled. */
  model: string
  researchConfigured: boolean
}

const EMPTY_MODELS: readonly string[] = []
const NO_SOURCES: readonly SourceRecord[] = []

function callToolThrough(client: NonNullable<ReturnType<typeof useMcp>['client']>): CallTool {
  return async (name, args) => (await client.callTool({ name, arguments: args })) as McpToolResult
}

export function useResearchRun(): ResearchRunBinding {
  const slot = useAssistantSlot()
  const listing = useFileListing()
  const picked = usePickedModel(slot.endpoint?.models ?? EMPTY_MODELS, slot.endpoint?.model ?? null, listing.data?.root)
  const prompts = usePromptNames()
  const advertised = (prompts.data ?? []).includes(AUTHOR_PACK_PROMPT)
  const authorPrompt = usePromptText(AUTHOR_PACK_PROMPT, advertised)
  const advertisesTest = (prompts.data ?? []).includes(TEST_PACK_PROMPT)
  const testPrompt = usePromptText(TEST_PACK_PROMPT, slot.thinking !== 'off' && advertisesTest)
  const { config } = useEffectiveConfig()
  const research = config.research
  const mcp = useMcp()

  const blocked =
    slot.state === 'unavailable'
      ? 'The desk-level configuration could not be read, so no assistant is available.'
      : slot.endpoint === null
        ? 'No assistant endpoint is configured. Configure one in Admin › Assistant.'
        : slot.keyStatus === 'pending'
          ? 'Checking the saved API key…'
          : !slot.keyPresent
            ? 'No API key is stored for the assistant. Save one in Admin › Assistant.'
            : picked.model === ''
              ? 'Choose an enabled model in Admin › Assistant.'
              : !advertised
                ? 'This runtime does not offer the authoring prompt.'
                : authorPrompt.data === undefined
                  ? 'Reading the authoring prompt…'
                  : research.gateway === null
                    ? 'No research gateway is configured. Add a research section to the desk-level desk.json.'
                    : research.sources.search === null && research.sources.read === null
                      ? 'The research section names no search or read source.'
                      : mcp.status !== 'ready' || mcp.client === null
                        ? 'The runtime connection is not ready.'
                        : !mcp.validateSupported
                          ? 'This runtime does not serve validate, so a draft cannot be checked.'
                          : ''

  const ledgerRef = useRef<Ledger | null>(null)
  if (ledgerRef.current === null) ledgerRef.current = new Ledger(newResearchSession())
  const ledger = ledgerRef.current

  // The settings a turn reads, as of the moment it starts.
  const settings = useRef({ slot, picked, authorPrompt: authorPrompt.data?.text ?? '', testPrompt: testPrompt.data?.text ?? '', research, mcp })
  settings.current = { slot, picked, authorPrompt: authorPrompt.data?.text ?? '', testPrompt: testPrompt.data?.text ?? '', research, mcp }

  const run = useMemo(() => {
    const log = (text: string) => recordActivity(`research: ${text}`)
    const spent = { searches: 0, reads: 0, bytes: 0, startedAt: Date.now() }
    const current = settings.current
    const turn = async (request: TurnRequest, signal: AbortSignal, onEvent: (event: AssistantEvent) => void) => {
      const { slot, picked, testPrompt } = settings.current
      const endpoint = slot.endpoint
      if (endpoint === null) throw new Error('no assistant endpoint is configured')
      const sessionId = await sessionBearer()
      const opened = openAssistantConnection({ allowed: endpoint.tools, onEvent, sessionId, signal })
      try {
        const ready = await opened.ready
        const engine = await loadEngine(slot.engine)
        await runAssistantSession(
          engine,
          {
            prompt: request.prompt,
            testPrompt: request.reviewer ? '' : testPrompt,
            tools: ready.tools,
            callTool: ready.callTool,
            hostTools: request.hostTools,
            model: { family: endpoint.kind, model: picked.model, call: bindModelCall(endpoint.kind) },
            thinking: normalize(request.reviewer ? 'off' : slot.thinking, endpoint.kind),
            signal
          },
          onEvent
        )
      } finally {
        await opened.close()
      }
    }
    return new AuthoringRun({
      turn,
      callTool: async (name, args) => {
        const client = settings.current.mcp.client
        if (client === null) throw new Error('the runtime connection is not ready')
        // The desk's own connection: rehearsal is written here, by the desk,
        // on every evaluation it asks for a candidate.
        return callToolThrough(client)(name, name === 'experimental_evaluate' ? { ...args, rehearsal: true } : args)
      },
      ledger,
      researchTools: researchTools({
        config: current.research,
        ledger,
        budget: current.research.limits,
        spent,
        acquire,
        log
      }),
      seal: async (session, signal) => {
        await seal(session, signal)
      },
      registry: (signal) => registry(signal),
      gateway:
        current.research.gateway === null
          ? null
          : { authority: current.research.gateway.authority, publicKeyHex: current.research.gateway.signer.public },
      newSession: newResearchSession,
      get authorPrompt() {
        return settings.current.authorPrompt
      },
      maxRevisions: MAX_REVISIONS,
      log
    })
    // One run per page mount: the ledger and the budget are the run's.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [ledger])

  const state = useSyncExternalStore(run.subscribe, run.getSnapshot, () => INITIAL_STATE)
  const sources = useSyncExternalStore(ledger.subscribe, ledger.getSnapshot, () => NO_SOURCES)
  return {
    run,
    state,
    ledger,
    sources,
    blocked,
    model: picked.model,
    researchConfigured: research.gateway !== null
  }
}
