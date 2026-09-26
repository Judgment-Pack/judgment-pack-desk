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
  NO_MODEL_CHOSEN,
  type AssistantEndpointConfig,
  type AssistantAgentConfig,
  type AssistantEngine,
  type ThinkingTier
} from '../config/deskConfig'

export const CHECKING_KEY = sourceMessage('Checking saved API key…')
export const UNREAD_KEY = sourceMessage('Could not check the saved API key. Retry or open Admin › Assistant for details.')

export interface AssistantSlot {
  agent?: AssistantAgentConfig
  /** A selected target, no target, or a configuration read that failed. */
  state: 'none' | 'configured' | 'unavailable'
  /** Saved API endpoint; it may be inactive when the Codex agent is selected. */
  endpoint: AssistantEndpointConfig | null
  /** Why the selected target cannot run yet, if known. */
  unusable?: string
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

export function useAssistantSlot(): AssistantSlot {
  const { config, desk } = useEffectiveConfig()
  const subscription = config.assistant.engine === 'codex'
  const key = useAssistantKey(!subscription)
  const account = useProviderStatus(subscription)
  const endpoint = config.assistant.endpoint
  // **A read that did not produce a file is not a file that says none.** A
  // refused *decode* is different and is deliberately not here: that file was
  // read, this desk will not honour it, and the defaults are what apply — which
  // is `none`, truthfully. What this covers is the read that never produced
  // one at all.
  const unread = desk?.readFailure !== undefined
  if (subscription) {
    const agent = config.assistant.agent
    const busy = account.error instanceof ProviderError && account.error.code === 'provider-busy'
    const unusable = !agent ? sourceMessage('Configure a ChatGPT subscription in Assistant settings.')
      : account.isPending ? sourceMessage('Checking the ChatGPT connection…')
      : account.error && !busy ? account.error.message
      : account.data?.account !== 'connected' ? sourceMessage('Connect your ChatGPT account in Assistant settings.')
      : !agent.model ? sourceMessage('Choose a model for the ChatGPT subscription in Assistant settings.') : undefined
    return { state:unread ? 'unavailable' : agent ? 'configured' : 'none', endpoint, agent, unusable,
      keyPresent:key.isSuccess && key.data.present, keyStatus:key.status, retryKey:()=>{ void account.refetch() },
      engine:config.assistant.engine, thinking:config.assistant.thinking }
  }
  return {
    ...(config.assistant.agent ? {agent:config.assistant.agent}:{}),
    state: unread ? 'unavailable' : endpoint === null ? 'none' : 'configured',
    endpoint,
    // The decoder's words, read off the decoder's own constant. A sentence
    // typed out here would be a second copy nothing keeps in step.
    unusable: endpoint !== null && endpoint.model === null ? NO_MODEL_CHOSEN : undefined,
    keyPresent: key.isSuccess && key.data.present,
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
