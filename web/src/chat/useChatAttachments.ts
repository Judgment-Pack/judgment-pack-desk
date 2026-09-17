import { sourceMessage } from '../i18n/source'
import { useEffect, useRef, useState } from 'react'
import type { ChatStore } from './store'

export const TEXT_ATTACHMENT_ACCEPT = '.txt,.md,.json,.csv'
const LIMIT = 4

/** Local text ingestion; gateway adapters can replace ingestion without owning
 * the composer. Keep reads out of a send and discard late results after leaving
 * the chat. A batch is accepted together, so an invalid file cannot send only
 * part of the context the user chose. */
export function useChatAttachments(store: ChatStore | null, chatId: string, disabled: boolean) {
  const [reading, setReading] = useState(false)
  const [error, setError] = useState('')
  const active = useRef<object | null>(null)
  useEffect(() => {
    setReading(false)
    setError('')
    return () => { active.current = null }
  }, [store, chatId, disabled])
  const isReading = () => active.current !== null
  const cancel = () => { active.current = null; setReading(false) }
  const attach = async (files: FileList | readonly File[] | null) => {
    if (!files?.length || !store || disabled || isReading()) return
    const current = () => {
      const snapshot = store.getSnapshot()
      return [...snapshot.chats, ...snapshot.drafts].find(item => item.id === chatId)
    }
    const chat = current()
    if (!chat || store.getSnapshot().bindings.get(chatId)?.run?.running) return
    const operation = {}
    active.current = operation
    setReading(true)
    setError('')
    try {
      if (files.length + (chat.attachments?.length ?? 0) > LIMIT) throw new Error(sourceMessage("Attach up to four text files at a time."))
      const chosen = [...files]
      // Check the entire batch before reading any bytes.
      for (const file of chosen) {
        if (file.size > 200_000) throw new Error(sourceMessage("{{value0}} is over the 200 KB text-file limit.", { value0: file.name }))
        if (!/\.(txt|md|json|csv)$/i.test(file.name)) throw new Error(sourceMessage("Choose .txt, .md, .json or .csv files. PDF and connected sources are not available yet."))
      }
      const pieces = await Promise.all(chosen.map(async file => {
        let text: string
        try { text = new TextDecoder('utf-8', { fatal: true }).decode(await file.arrayBuffer()) }
        catch { throw new Error(sourceMessage("{{value0}} could not be read as UTF-8 text.", { value0: file.name })) }
        if (text.includes('\0')) throw new Error(sourceMessage("{{value0}} is not a text file.", { value0: file.name }))
        return { id: crypto.randomUUID(), name: file.name, text }
      }))
      if (active.current !== operation) return
      const latest = current()
      if (!latest || store.getSnapshot().bindings.get(chatId)?.run?.running) return
      const existing = latest.attachments ?? []
      if (existing.length + pieces.length > LIMIT) throw new Error(sourceMessage("Attach up to four text files at a time."))
      store.update(chatId, { attachments: [...existing, ...pieces] })
    } catch (cause) {
      if (active.current === operation) setError((cause as Error).message)
    } finally {
      if (active.current === operation) { active.current = null; setReading(false) }
    }
  }
  return { reading, error, isReading, attach, cancel }
}
