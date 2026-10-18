export interface GraphDraftIdentity { originChatId?: string; draftId: string }
export const graphDraftKey = (draft: GraphDraftIdentity) => (draft.originChatId ?? '') + ':' + draft.draftId
export const graphDraftFolderId = (draft: GraphDraftIdentity) => 'graph-draft:' + graphDraftKey(draft)

/** A bare historical workspace is usable only when its original owner is known. */
export function graphDraftWorkspaceMatches(identity: GraphDraftIdentity & {id?:string;path?:string}, chat: {id: string; graph?: {workspace: string;id?:string;path?:string}}, known: readonly GraphDraftIdentity[]): boolean {
  if (chat.graph?.workspace === graphDraftKey(identity)) return true
  if (!identity.originChatId || chat.graph?.workspace !== identity.draftId) return false
  if (chat.id === identity.originChatId) return true
  // Saved graph workspaces are bare graph IDs too. Uniqueness of the draft ID
  // alone cannot distinguish an unrelated saved graph's helper.
  if (!identity.id || !identity.path || chat.graph.id !== identity.id || chat.graph.path !== identity.path) return false
  const owners = new Set(known.filter(draft => draft.draftId === identity.draftId).map(draft => draft.originChatId))
  return owners.size === 1 && owners.has(identity.originChatId)
}
