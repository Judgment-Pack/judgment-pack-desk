import { useState } from 'react'
import type { GraphDocument } from '../mcp/types'
import { usePack } from '../mcp/queries'
import { msg } from '../i18n'
import { Input } from '../ui/Input'
import { Disclosure } from '../ui/Disclosure'
import { ButtonLink } from '../ui/Button'
import { CodeBlock } from '../ui/CodeBlock'
import { declaredSources } from '../research/ui/DraftSources'
import { isRecord } from '../packs/document/MisshapenMember'
import styles from '../definitions/DefinitionWorkspace.module.css'

const text = (value: unknown): string => typeof value === 'string' ? value : ''

/** Sources belong to packs; this view never copies them into the graph. */
export function GraphSources({document}: {document?: GraphDocument}) {
  const [search, setSearch] = useState('')
  const readable = isRecord(document?.nodes)
  const nodes = readable ? Object.entries(document.nodes ?? {}) : []
  const packs = new Map<string, string[]>()
  for (const [name, node] of nodes) {
    if (isRecord(node) && typeof node.pack === 'string') packs.set(node.pack, [...(packs.get(node.pack) ?? []), name])
  }
  const incomplete = nodes.some(([, node]) => !isRecord(node) || typeof node.pack !== 'string')
  return <section className={styles.reading} aria-label={msg('Graph sources')}>
    <div className={styles.toolbar}><Input type="search" aria-label={msg('Search graph sources')} placeholder={msg('Search sources…')} value={search} onChange={event => setSearch(event.target.value)}/></div>
    <p className={styles.muted}>{msg('References declared by the current saved packs. Edit them in their owning pack. Execution receipts belong to the run history.')}</p>
    {incomplete && <p role="status">{msg('Some graph steps could not be read. Their source references are unavailable.')}</p>}
    {!readable ? <p>{msg('No readable graph document was returned.')}</p> : !packs.size && !incomplete ? <p>{msg('No packs referenced yet.')}</p> : [...packs].map(([pack, steps]) => <PackSources key={pack} id={pack} steps={steps} search={search}/>)}
  </section>
}

function PackSources({id, steps, search}: {id: string; steps: string[]; search: string}) {
  const pack = usePack(id), document = pack.data?.document, sources = declaredSources(document)
  const rows = sources.filter(source => [JSON.stringify(source), id, ...steps].join(' ').toLowerCase().includes(search.toLowerCase()))
  const malformed = isRecord(document) && document.sources !== undefined && !Array.isArray(document.sources)
  return <section className={styles.section}>
    <div className={styles.toolbar}><h2>{document?.title ?? id}</h2><div className={styles.actions}><ButtonLink variant="quiet" to={'/packs/' + encodeURIComponent(id) + '?view=evidence'}>{msg('Open pack sources')}</ButtonLink></div></div>
    <p className={styles.muted}>{msg('Used by: {{steps}}', {steps: steps.join(', ')})}</p>
    {pack.error ? <p role="status">{msg('This pack could not be read. Its source references are unavailable.')}</p>
      : !pack.data ? <p role="status">{msg('Loading sources…')}</p>
      : malformed || !document ? <p role="status">{msg('Source references could not be read. Inspect View JSON for the exact definition.')}</p>
      : !rows.length ? <p className={styles.muted}>{sources.length ? msg('No matching sources.') : msg('This pack declares no source references.')}</p>
      : rows.map((source, index) => {
        const record = isRecord(source) ? source : {}, citation = isRecord(record.citation) ? record.citation : {}
        const label = text(record.title) || text(record.id) || msg('Source {{value0}}', {value0: index + 1})
        const locator = isRecord(record.locator) ? text(record.locator.value) : ''
        return <Disclosure key={index} title={label}>
          <div className={styles.section}>
            <p className={styles.muted}>{msg('Declared reference; acquisition and source authority are not established by this declaration.')}</p>
            {locator && <p>{locator}</p>}
            {text(record.publisher) && <p>{text(record.publisher)}</p>}
            {text(citation.excerpt) && <blockquote>{text(citation.excerpt)}</blockquote>}
            {text(citation.location) && <p>{text(citation.location)}</p>}
            <Disclosure title={msg('Exact reference')}><CodeBlock expandable text={JSON.stringify(source, null, 2)} label={label}/></Disclosure>
          </div>
        </Disclosure>
      })}
  </section>
}
