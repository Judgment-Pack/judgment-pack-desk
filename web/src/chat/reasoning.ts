import { CODEX_EFFORTS, type AssistantAgentConfig } from '../config/deskConfig'
import type { CodexEffort } from '../assistant/agent'

/** A chat override belongs to the selected model; null explicitly uses its default. */
export interface ChatReasoning { model: string; effort: CodexEffort | null }
export function validChatReasoning(value: unknown): value is ChatReasoning {
  if (!value || typeof value !== 'object' || Array.isArray(value)) return false
  const setting = value as Partial<ChatReasoning>
  return typeof setting.model === 'string' && setting.model.length > 0 && setting.model.length <= 128
    && !/[\r\n\0]/.test(setting.model)
    && (setting.effort === null || CODEX_EFFORTS.includes(setting.effort as CodexEffort))
}
export function chatReasoningAgent(agent: AssistantAgentConfig | undefined, model: string, setting?: ChatReasoning): AssistantAgentConfig | undefined {
  if (!agent) return undefined
  // Admin's effort belongs to its default model. Another model starts at its
  // own native default unless this chat explicitly chose an effort for it.
  const target = agent.model === model ? agent : { ...agent, model, effort: undefined }
  if (!setting || setting.model !== model) return target
  return { ...target, effort: setting.effort ?? undefined }
}
