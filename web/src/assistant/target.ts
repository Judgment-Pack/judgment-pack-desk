import type { AITarget } from './aiConnections'
import { allowedAgentModels } from '../config/deskConfig'
import { sourceMessage } from '../i18n/source'
import type { AssistantAgentConfig, AssistantConfig, AssistantEndpointConfig, ThinkingTier } from '../config/deskConfig'
import type { AssistantSession, AgentAssistantSession } from '@judgment-pack/harness/engine'
import { bindAgentRun } from '@judgment-pack/harness/agentTransport'
import { bindModelCall } from '@judgment-pack/harness/session'
import { normalize } from '@judgment-pack/harness/thinking'

/** UI selection has no endpoint impersonation: an agent target stays an agent. */
export function selectedAssistant(slot: { engine?: string; endpoint: AssistantEndpointConfig | null; agent?: AssistantAgentConfig }) {
  return slot.engine === 'codex' ? slot.agent ? { models:allowedAgentModels(slot.agent), model:slot.agent.model, tools:slot.agent.tools } : null : slot.endpoint
}
export function bindExecution(config: Pick<AssistantConfig,'engine'|'endpoint'|'agent'> & AITarget, model: string, thinking: ThinkingTier): Pick<AssistantSession,'model'|'thinking'> | Pick<AgentAssistantSession,'agent'|'effort'> {
  if (!model) throw new Error(sourceMessage('Choose an Assistant model before starting.'))
  if (config.engine === 'codex') {
    if (!config.agent || !allowedAgentModels(config.agent).includes(model)) throw new Error(sourceMessage('Choose a model for the ChatGPT subscription in Assistant settings.'))
    return { agent:{ model, run:config.connectionId?bindAgentRun(model,config):bindAgentRun(model) }, ...(config.agent.model === model && config.agent.effort ? { effort:config.agent.effort } : {}) }
  }
  if (!config.endpoint) throw new Error(sourceMessage('Choose an enabled Assistant model.'))
  return { model:{ family:config.endpoint.kind,model,call:config.connectionId?bindModelCall(config.endpoint.kind,config):bindModelCall(config.endpoint.kind) }, thinking:normalize(thinking,config.endpoint.kind) }
}
