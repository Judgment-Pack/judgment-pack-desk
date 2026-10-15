import {LoadingState} from '../ui/LoadingState'
import { useMemo, useState } from 'react'
import { useSearchParams } from 'react-router-dom'
import { msg, useLocale } from '../i18n'
import { useGraphDocument } from '../mcp/queries'
import { useInspectorPresentation } from '../shell/InspectorPresentation'
import { useInspectorPortal } from '../shell/InspectorSlot'
import { useShellState } from '../shell/paneState'
import { useBuilderPreference } from '../builders/BuilderSplit'
import { GraphAssistant } from './GraphAssistant'

export function SavedGraphAssistant({ graphId }: { graphId: string }) {
  const locale = useLocale(), graph = useGraphDocument(graphId), shell = useShellState()
  const [params, setParams] = useSearchParams(), [chatId, setChatId] = useState<string>()
  const [open, setOpen] = useBuilderPreference<boolean>('graph:assistant-open', true)
  useInspectorPresentation(useMemo(() => ({ title: msg('Assistant'), contextTitle: graphId, workspaceTools: true,
    open, onOpenChange: setOpen, width: shell.inspectorWidth ?? 400, onResize: shell.resizeInspector,
    onReset: shell.resetInspectorWidth, minimumMainWidth: 480, maximumWidth: 640
  }), [locale, graphId, open, setOpen, shell.inspectorWidth, shell.resizeInspector, shell.resetInspectorWidth]))
  return useInspectorPortal(graph.data && !graph.error ? <GraphAssistant workspace={graphId}
    proposal={{id: graphId, path: graph.data.meta.path, content: graph.data.raw, baseSha256: graph.data.meta.sha256}}
    chatId={params.get('chat') ?? chatId} onChat={id => { setChatId(id); if (params.get('chat') !== id) setParams(previous => { const next = new URLSearchParams(previous); next.set('chat', id); return next }, {replace: true}) }} busy={false} selection=""/>
    : graph.error ? <p role="status">{graph.error.message}</p> : <LoadingState inset>{msg('Loading graph…')}</LoadingState>)
}
