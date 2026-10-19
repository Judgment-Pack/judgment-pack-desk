import type { AITarget } from './aiConnections'
import { allowedAgentModels } from '../config/deskConfig'
import { sourceMessage } from '../i18n/source'
import type { AssistantAgentConfig, AssistantConfig, AssistantEndpointConfig, ThinkingTier } from '../config/deskConfig'
import type { AssistantSession, AgentAssistantSession } from '@judgment-pack/harness/engine'
import { bindAgentRun } from '@judgment-pack/harness/agentTransport'
import { bindModelCall } from '@judgment-pack/harness/session'
import { normalize } from '@judgment-pack/harness/thinking'
import type { RunMeter } from './runMeter'
import { capabilitiesOf } from './endpointCapabilities'

/** UI selection has no endpoint impersonation: an agent target stays an agent. */
export function selectedAssistant(slot: { engine?: string; endpoint: AssistantEndpointConfig | null; agent?: AssistantAgentConfig }) {
  return slot.engine === 'codex' ? slot.agent ? { models:allowedAgentModels(slot.agent), model:slot.agent.model, tools:slot.agent.tools } : null : slot.endpoint
}
export function bindExecution(config: Pick<AssistantConfig,'engine'|'endpoint'|'agent'> & AITarget, model: string, thinking: ThinkingTier, meter?: RunMeter): Pick<AssistantSession,'model'|'thinking'> | Pick<AgentAssistantSession,'agent'|'effort'> {
  if (!model) throw new Error(sourceMessage('Choose an Assistant model before starting.'))
  if (config.engine === 'codex') {
    if (!config.agent || !allowedAgentModels(config.agent).includes(model)) throw new Error(sourceMessage('Choose a model for the ChatGPT subscription in Assistant settings.'))
    const run = config.connectionId?bindAgentRun(model,config):bindAgentRun(model)
    return { agent:{ model, run:meter?meter.agent(run,model):run }, ...(config.agent.model === model && config.agent.effort ? { effort:config.agent.effort } : {}) }
  }
  if (!config.endpoint) throw new Error(sourceMessage('Choose an enabled Assistant model.'))
  const call = config.connectionId?bindModelCall(config.endpoint.kind,config):bindModelCall(config.endpoint.kind)
  // What this connection states about its endpoint (its probe's finding and its owner's overrides) rides on the session,
  // which is exact where two connections serve one model; the harness completes the rest from the family's default.
  const capabilities = capabilitiesOf(config.endpoint)
  return { model:{ family:config.endpoint.kind,model,call:meter?meter.call(call,model):call,...(Object.keys(capabilities).length?{capabilities}:{}) }, thinking:normalize(thinking,config.endpoint.kind) }
}
