import { ChatContext } from '@judgment-pack/harness/context/chat'

/**
 * One `ChatContext` per chat, the host's half of the harness's chat-scoped
 * context (`@judgment-pack/harness` v0.2.0, agent#8): handed to every run of
 * the chat as `context.chat`, it keeps the chat's ledger and read cache across
 * the runs, so a second turn reads each instruction, schema and test set once.
 *
 * It lives in memory only, as the package keeps it: a reloaded page starts the
 * chat's runs on a fresh one, and the chat's saved checkpoint carries what a
 * run needs. **Disposed when the chat is deleted** (`ChatStore.remove`), which
 * clears what it holds and refuses every later run on it. Nothing here is
 * keyed by anything but the chat's id, so what one chat retained is never
 * reachable from another.
 */
const held = new Map<string, ChatContext>()

/** The chat's context, made on first use. */
export function chatContextFor(chatId: string): ChatContext {
  let context = held.get(chatId)
  if (context === undefined || context.isDisposed) {
    context = new ChatContext()
    held.set(chatId, context)
  }
  return context
}

/** The chat was deleted: clear what its context holds and forget it. */
export function disposeChatContext(chatId: string): void {
  held.get(chatId)?.dispose()
  held.delete(chatId)
}
