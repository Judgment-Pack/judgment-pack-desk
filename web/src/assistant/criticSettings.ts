import type { CriticMode } from '@judgment-pack/harness/contract'

// Saved Desk chats may retain the old boolean. Translate only at the host
// boundary: v0.9.1 refuses the retired member even when its value is undefined.
export function savedCriticChoice(settings: { critic?: CriticMode; adversarialReview?: boolean }): CriticMode | undefined {
  return settings.critic ?? (settings.adversarialReview === undefined ? undefined : settings.adversarialReview ? 'on' : 'off')
}
