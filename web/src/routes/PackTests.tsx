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
  return (
    <article className={styles.root} data-layout="page">
      <PackHeader packId={packId} document={pack.data?.document} current="test" />
      {pack.error ? (
        <p role="alert">{pack.error.message}</p>
      ) : !pack.data ? (
        <p role="status">{msg('Loading pack…')}</p>
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
