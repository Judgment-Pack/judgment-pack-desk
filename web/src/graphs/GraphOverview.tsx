import { msg, useLocale } from '../i18n'
import type { GraphDocument } from '../mcp/types'
import { Button, ButtonLink } from '../ui/Button'
import { DefinitionOverview, overviewText } from '../definitions/DefinitionOverview'
import styles from '../definitions/DefinitionOverview.module.css'

export function GraphOverview({ document, description, loading, unavailable, logicHref, onViewLogic }: {
  document?: GraphDocument; description?: string; loading?: boolean; unavailable?: string; logicHref?: string; onViewLogic?: () => void
}) {
  useLocale()
  const [purpose, ...context] = overviewText(description, document?.description)
  const nodes = document?.nodes && typeof document.nodes === 'object' && !Array.isArray(document.nodes) ? Object.entries(document.nodes) : undefined
  const packs = nodes?.every(([, node]) => node && typeof node.pack === 'string' && !!node.pack.trim()) ? [...new Set(nodes.map(([, node]) => node.pack))] : undefined
  const resultNode = nodes?.find(([id]) => id === document?.result)
  const action = onViewLogic ? <Button variant="quiet" onClick={onViewLogic}>{msg('View logic')}</Button> : logicHref ? <ButtonLink variant="quiet" to={logicHref}>{msg('View logic')}</ButtonLink> : undefined
  return <DefinitionOverview label={msg('Graph overview')} purpose={purpose} context={context} loading={loading}
    unavailable={unavailable || (!document ? msg('No readable graph document was returned.') : undefined)}
    resultLabel={msg('Final result')} action={action}
    result={<>
      <p className={styles.resultName}>{resultNode ? resultNode[0] : document?.result ? msg('The result step could not be found.') : msg('No result step has been selected.')}</p>
      {resultNode?.[1]?.pack && <p className={styles.muted}>{msg('Provided by {{pack}}.', { pack: resultNode[1].pack })}</p>}
      <p className={styles.summary}>{nodes && packs ? [msg('{{count}} steps', { count: nodes.length }), msg('{{count}} distinct packs', { count: packs.length })].join(' · ') : msg('Composition details could not be read.')}</p>
    </>}
    details={[
      { label: msg('Referenced packs'), value: packs ? packs.join(', ') || msg('No packs referenced yet.') : msg('Not available') },
      { label: msg('Connections'), value: Array.isArray(document?.edges) ? document.edges.length : msg('Not available') }
    ]}/>
}
