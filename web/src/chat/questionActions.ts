import { sourceMessage } from '../i18n/source'
import type { ChatStore } from './store'

/** Wait for an earlier checkpoint write; never bypass its revision conflict. */
async function saveAnswer(store: ChatStore): Promise<boolean> {
  // Submit/Continue is an explicit save retry. Keep the original revision guard;
  // a persistent conflict still blocks model continuation.
  if (store.getSnapshot().error) store.retrySave()
  if (store.getSnapshot().saving) await new Promise<void>((resolve, reject) => {
    const timer = setTimeout(() => { unsubscribe(); reject(Error(sourceMessage('Chat is still saving. Your answer is kept here; try Continue again.'))) }, 10_000)
    const unsubscribe = store.subscribe(() => {
      if (store.getSnapshot().saving) return
      clearTimeout(timer); unsubscribe(); resolve()
    })
    if (!store.getSnapshot().saving) { clearTimeout(timer); unsubscribe(); resolve() }
  })
  return store.flush()
}
export async function continueQuestion(store: ChatStore, chatId: string, questionId: string): Promise<void> {
  if (!await saveAnswer(store)) throw Error(store.getSnapshot().error || sourceMessage('Your answer is kept here. Retry saving, then choose Continue.'))
  let continued = false
  const started = store.perform(chatId, binding => { continued = binding.run?.continueQuestion(questionId) ?? false })
  if (!started || !continued) throw Error(sourceMessage('This question cannot continue now. Check the current conversation and connection.'))
}
export async function submitQuestion(store: ChatStore, chatId: string, questionId: string, kind: 'answered' | 'help'): Promise<void> {
  let prepared = false
  const accepted = store.perform(chatId, binding => {
    prepared = binding.run?.prepareQuestionAnswer(questionId, kind) ?? false
    if (prepared && binding.run) store.report(chatId, {...binding,state:binding.run.getSnapshot()})
  })
  if (!accepted || !prepared) throw Error(sourceMessage('This question changed or cannot be answered now. Review the current conversation.'))
  await continueQuestion(store, chatId, questionId)
}
