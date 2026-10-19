import { useEffect } from 'react'
import { resolveAIConnection, type AITarget } from './aiConnections'
import { rememberConnection } from './connectionRecords'
import { ProviderError, useProviderStatus } from './providers'
import { selectedAssistant } from './target'
import { sourceMessage } from '../i18n/source'
/**
 * Shared configuration and readiness for all assistant consumers. Configuration
 * presence is separate from credential/model readiness: a saved target can be
 * configured without being ready to run. Only the selected authentication path
 * contributes to readiness; a retained inactive endpoint or agent does not.
 */
import { useEffectiveConfig } from '../config/DeskConfigProvider'
import { useAssistantKey } from './queries'
import {
  type AssistantEndpointConfig,
  type AssistantAgentConfig,
  type AssistantEngine,
  type ThinkingTier
} from '../config/deskConfig'

export const UNBOUND_KEY = sourceMessage('The saved API key does not match this endpoint. Save the key again in Connections > AI.')
export const CHECKING_KEY = sourceMessage('Checking saved API key…')
export const UNREAD_KEY = sourceMessage('Could not check the saved API key. Retry or open Connections > AI for details.')

export interface AssistantSlot {
  connectionId?:string
  connectionRevision?:string
  connectionName?:string
  agent?: AssistantAgentConfig
  /** A selected target, no target, or a configuration read that failed. */
  state: 'none' | 'configured' | 'unavailable'
  /** Saved API endpoint; it may be inactive when the Codex agent is selected. */
  endpoint: AssistantEndpointConfig | null
  /** Why the selected target cannot run yet, if known. */
  unusable?: string
  /** Recovery follows the failed operation, never a guessed authentication state. */
  recovery?: 'retry' | 'configure'
  retrying?: boolean
  /**
   * Whether a key is stored on this machine.
   *
   * True only after a successful read confirms presence. Consumers must read
   * keyStatus before describing false as an absent key: loading and failed
   * requests establish nothing about the credential on disk.
   */
  keyPresent: boolean
  keyStatus: 'pending' | 'error' | 'success'
  retryKey: () => void
  /** The selected execution engine and existing review-depth preference. */
  engine: AssistantEngine
  thinking: ThinkingTier
}

export function useAssistantSlot(connectionId?:string): AssistantSlot {
  const effective=useEffectiveConfig()
  const target=resolveAIConnection(effective,connectionId)
  const {desk,assistantProfile}=effective
  const config={...effective.config,assistant:target.assistant}
  const connection=target.connection
  const identity:AITarget=connection?{connectionId:connection.id,connectionRevision:connection.revision,connectionName:connection.name}:{}
  const subscription = config.assistant.engine === 'codex'
  const key = useAssistantKey(!subscription&&!target.problem,connection?.id,connection?.revision)
  const account = useProviderStatus(subscription&&!target.problem,connection?.id)
  const endpoint = config.assistant.endpoint
  // The harness asks the host about a run's connection by its name alone, once
  // the run has started; what the connection names for its critic and its
  // delegated children is read from here (`connectionModels.ts`).
  useEffect(() => { if (connection) rememberConnection(identity, target.assistant) }, [connection?.id, connection?.revision, target.assistant])
  // **A read that did not produce a file is not a file that says none.** A
  // refused *decode* is different and is deliberately not here: that file was
  // read, this desk will not honour it, and the defaults are what apply — which
  // is `none`, truthfully. What this covers is the read that never produced
  // one at all.
  const unread = desk?.readFailure !== undefined || !!assistantProfile?.problem
  if(target.problem)return {...identity,state:'unavailable',endpoint:null,unusable:target.problem,recovery:effective.aiConnections?.loading?undefined:'configure',keyPresent:false,keyStatus:'pending',retryKey:()=>{},engine:config.assistant.engine,thinking:config.assistant.thinking}
  if (subscription) {
    const agent = config.assistant.agent
    const busy = account.error instanceof ProviderError && account.error.code === 'provider-busy'
    let unusable: string | undefined
    let recovery: AssistantSlot['recovery']
    if (unread || !agent) {
      unusable = assistantProfile?.problem ?? sourceMessage('Configure a ChatGPT subscription in Connections > AI.')
      recovery = 'configure'
    } else if (account.isPending) {
      unusable = sourceMessage('Checking the ChatGPT connection…')
    } else if (account.error && !(busy && account.data?.account === 'connected')) {
      // Busy does not establish whether an account is signed in. Keep a
      // previously verified connection, otherwise wait for a successful read.
      unusable = account.error.message
      recovery = account.error instanceof ProviderError && account.error.code === 'sign-in-required' ? 'configure' : 'retry'
    } else if (account.data?.account === 'signed-out') {
      unusable = sourceMessage('Connect your ChatGPT account in Connections > AI.')
      recovery = 'configure'
    } else if (account.data?.account === 'login-pending') {
      unusable = sourceMessage('Waiting for sign-in…')
      recovery = 'configure'
    } else if (account.data?.account !== 'connected') {
      unusable = sourceMessage('The ChatGPT connection could not complete this request. Try again.')
      recovery = account.data?.runtime === 'not-installed' ? 'configure' : 'retry'
    } else if (!agent.model) {
      unusable = sourceMessage('Choose a model for the ChatGPT subscription in Assistant settings.')
      recovery = 'configure'
    }
    return { ...identity, state:unread ? 'unavailable' : agent ? 'configured' : 'none', endpoint, agent, unusable, recovery, retrying: account.isFetching,
      keyPresent:key.isSuccess && key.data.present, keyStatus:key.status, retryKey:()=>{ void account.refetch() },
      engine:config.assistant.engine, thinking:config.assistant.thinking }
  }
  const unbound = key.isSuccess && key.data.present && !key.data.bound
  return {
    ...identity,
    ...(config.assistant.agent ? {agent:config.assistant.agent}:{}),
    state: unread ? 'unavailable' : endpoint === null ? 'none' : 'configured',
    endpoint,
    // Use the server's binding verdict and point to the setting that repairs it.
    unusable: assistantProfile?.problem ?? (unbound ? UNBOUND_KEY : undefined) ?? (endpoint !== null && endpoint.model === null ? sourceMessage("An endpoint is configured and no model is chosen for it. Pick one in Admin › Assistant.") : undefined),
    recovery: unread || endpoint === null || endpoint.model === null ? 'configure'
      : key.isPending ? undefined : key.isError ? 'retry' : (!key.data.present && key.data.keyless !== true) || unbound ? 'configure' : undefined,
    retrying: key.isFetching,
    // **A key that is not needed is as good as one that is there**: the desk says so (`keyless`) for an endpoint on this computer.
    keyPresent: key.isSuccess && (key.data.present || key.data.keyless === true),
    keyStatus: key.status,
    retryKey: () => { void key.refetch() },
    engine: config.assistant.engine,
    thinking: config.assistant.thinking
  }
}

/** Shared readiness; subscription auth never depends on an API key. */
export function assistantReady(slot: AssistantSlot): boolean {
  if (slot.state !== 'configured' || !selectedAssistant(slot) || slot.unusable) return false
  return slot.engine === 'codex' || (slot.keyStatus === 'success' && slot.keyPresent)
}
