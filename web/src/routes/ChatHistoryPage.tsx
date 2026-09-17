import { Message } from '../i18n/Message'
import { msg, useLocale } from '../i18n'
import { useMemo } from 'react'
import { useNavigate } from 'react-router-dom'
import { ChatHistoryList } from '../chat/ChatHistory'
import { openNewChat } from '../chat/navigation'
import { useChats } from '../chat/ChatProvider'
import { useInspectorPresentation } from '../shell/InspectorPresentation'
import { PageHeader } from '../ui/PageLayout'
import { Button } from '../ui/Button'
import { IconPlus } from '../shell/icons'

/** Full project history is a destination; pack history stays within Assistant. */
export function ChatHistoryPage() {
  const locale = useLocale()
  const { store } = useChats()
  const navigate = useNavigate()
  const presentation = useMemo(() => ({ title: msg("Assistant"), available: false, open: false, onOpenChange: () => {}, width: 400, onResize: () => {}, onReset: () => {}, minimumMainWidth: 480, maximumWidth: 640 }), [locale])
  useInspectorPresentation(presentation)
  return <div data-measure="wide" data-layout="page">
    <PageHeader title={msg("Chat history")} actions={<Button disabled={!store?.canCreate} onClick={() => { if (store?.canCreate) openNewChat(navigate, store.startChat(undefined, undefined, true)) }}><Message text={"<0/> New chat"} slots={[<IconPlus />]} /></Button>} />
    <ChatHistoryList />
  </div>
}
