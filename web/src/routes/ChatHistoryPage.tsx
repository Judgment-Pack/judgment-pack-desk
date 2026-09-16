import { useMemo } from 'react'
import { useNavigate } from 'react-router-dom'
import { ChatHistoryList, chatHref } from '../chat/ChatHistory'
import { useChats } from '../chat/ChatProvider'
import { useInspectorPresentation } from '../shell/InspectorPresentation'
import { PageHeader } from '../ui/PageLayout'
import { Button } from '../ui/Button'
import { IconPlus } from '../shell/icons'

/** Full project history is a destination; pack history stays within Assistant. */
export function ChatHistoryPage() {
  const { store } = useChats()
  const navigate = useNavigate()
  const presentation = useMemo(() => ({ title: 'Assistant', available: false, open: false, onOpenChange: () => {}, width: 400, onResize: () => {}, onReset: () => {}, minimumMainWidth: 480, maximumWidth: 640 }), [])
  useInspectorPresentation(presentation)
  return <div data-measure="wide" data-layout="page">
    <PageHeader title="Chat history" actions={<Button disabled={!store?.canCreate} onClick={() => { if (store?.canCreate) navigate(chatHref(store.create())) }}><IconPlus /> New chat</Button>} />
    <ChatHistoryList />
  </div>
}
