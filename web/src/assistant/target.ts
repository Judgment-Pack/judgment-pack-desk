import {allowedAgentModels,type AssistantAgentConfig,type AssistantEndpointConfig} from '../config/deskConfig'

/** UI selection has no endpoint impersonation: an agent target stays an agent. */
export function selectedAssistant(slot: { engine?: string; endpoint: AssistantEndpointConfig | null; agent?: AssistantAgentConfig }) {
  return slot.engine === 'codex' ? slot.agent ? { models:allowedAgentModels(slot.agent), model:slot.agent.model, tools:slot.agent.tools } : null : slot.endpoint
}
