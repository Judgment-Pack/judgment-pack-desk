import type { CriticMode } from '@judgment-pack/harness/contract'
import { sourceMessage } from '../i18n/source'

// Saved Desk chats may retain the old boolean. Validate both original fields
// before translation, including a legacy value hidden by canonical precedence.
// v0.9.1 receives only canonical fields and refuses any retired member presence.
export function savedCriticChoice(settings: { critic?: unknown; adversarialReview?: unknown }): CriticMode | undefined {
  const { critic, adversarialReview } = settings
  if (critic !== undefined && critic !== 'off' && critic !== 'auto' && critic !== 'on') {
    throw new Error(sourceMessage('Invalid saved chat'))
  }
  if (adversarialReview !== undefined && typeof adversarialReview !== 'boolean') {
    throw new Error(sourceMessage('Invalid saved chat'))
  }
  return critic !== undefined ? critic : adversarialReview === undefined ? undefined : adversarialReview ? 'on' : 'off'
}
