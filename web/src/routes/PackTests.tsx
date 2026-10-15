import {PageLoading} from '../ui/PageLoading'
import { useMemo } from 'react'
import { PackAssistant } from '../chat/PackAssistant'
import { useInspectorPortal } from '../shell/InspectorSlot'
import { useInspectorPresentation } from '../shell/InspectorPresentation'
import { useShellState } from '../shell/paneState'
import { useBuilderPreference } from '../builders/BuilderSplit'
import { useParams, useSearchParams } from 'react-router-dom'
import { msg } from '../i18n'
import { usePack, usePacks } from '../mcp/queries'
import { PackHeader } from '../packs/PackWorkspace'
import { TestsContent } from '../packs/test-workspace/TestsWorkspace'
import styles from '../packs/test-workspace/TestsWorkspace.module.css'

export function PackTests() {
  const { packId = '' } = useParams(),
    pack = usePack(packId),
    inventory = usePacks()
  const title = String(pack.data?.document.title ?? packId),
    [params] = useSearchParams()
  const summary = inventory.data?.packs?.find((p) => p.id === packId)
  const shell = useShellState()
  const [open, setOpen] = useBuilderPreference<boolean>('assistant-open', true)
  useInspectorPresentation(useMemo(() => ({ title: msg('Assistant'), contextTitle: title, workspaceTools: true,
    open, onOpenChange: setOpen, width: shell.inspectorWidth ?? 400, onResize: shell.resizeInspector,
    onReset: shell.resetInspectorWidth, minimumMainWidth: 480, maximumWidth: 640
  }), [title, open, setOpen, shell.inspectorWidth, shell.resizeInspector, shell.resetInspectorWidth]))
  const assistant = useInspectorPortal(pack.data ? <div className={styles.retainedAssistant}><PackAssistant key={packId} packId={packId} title={title}
    path={summary?.path} draft={pack.data.raw} editing={false} busy={() => ''} diagnostics={undefined}/></div> : null)
  return (
    <article className={styles.root} data-layout="page">
      {assistant}
      <PackHeader packId={packId} document={pack.data?.document} sourceText={pack.data?.raw} current="test" />
      {pack.error ? (
        <p role="alert">{pack.error.message}</p>
      ) : !pack.data ? (
        <PageLoading>{msg('Loading pack…')}</PageLoading>
      ) : (
        <TestsContent
          key={packId}
          owner={packId}
          document={pack.data.document}
          text={pack.data.raw}
          title={title}
          matrixPath={summary?.matrixPath}
          initialHistory={params.get('history') === '1'}
        />
      )}
    </article>
  )
}
