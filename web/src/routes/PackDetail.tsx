import { useState } from 'react'
import { Link, useParams } from 'react-router-dom'
import { PackSemanticView } from '../components/PackSemanticView'
import { ErrorBox, Loading, Pill } from '../components/primitives'
import { usePack } from '../mcp/queries'

type Tab = 'semantic' | 'raw'

/** One pack: the reading view, and the document exactly as it is on disk. */
export function PackDetail() {
  const { packId } = useParams<{ packId: string }>()
  const { data, error, isPending } = usePack(packId)
  const [tab, setTab] = useState<Tab>('semantic')

  if (isPending) return <Loading what={`pack ${packId}`} />
  if (error) return <ErrorBox title={`Could not load pack ${packId}`} error={error} />
  if (!data) return null

  const { document: doc, meta, raw } = data

  return (
    <article className="detail">
      <nav className="crumbs">
        <Link to="/">Packs</Link>
        <span aria-hidden="true">/</span>
        <span>{packId}</span>
      </nav>

      <header className="detail-head">
        <h1>{doc.title}</h1>
        <p className="ids">
          <code>{doc.id}</code>
          <Pill tone="strong">v{doc.version}</Pill>
          <Pill tone="quiet">spec {doc.specVersion}</Pill>
        </p>
        {doc.description && <p className="lede">{doc.description}</p>}
        <p className="meta">
          {meta.path && <code>{meta.path}</code>}
          {meta.bytes !== undefined && <span>{meta.bytes.toLocaleString()} bytes</span>}
          {meta.sha256 && (
            <span title={meta.sha256}>sha256 {meta.sha256.slice(0, 12)}…</span>
          )}
        </p>
      </header>

      <div className="tabs" role="tablist">
        <TabButton current={tab} value="semantic" onSelect={setTab}>
          Document
        </TabButton>
        <TabButton current={tab} value="raw" onSelect={setTab}>
          Raw JSON
        </TabButton>
        <Link className="tab tab-action" to={`/packs/${encodeURIComponent(packId ?? '')}/evaluate`}>
          Evaluate…
        </Link>
      </div>

      {tab === 'semantic' ? (
        <PackSemanticView document={doc} />
      ) : (
        <figure className="json json-full">
          <pre>
            <code>{prettyPrint(raw)}</code>
          </pre>
        </figure>
      )}
    </article>
  )
}

function TabButton({
  current,
  value,
  onSelect,
  children
}: {
  current: Tab
  value: Tab
  onSelect: (tab: Tab) => void
  children: string
}) {
  const selected = current === value
  return (
    <button
      type="button"
      role="tab"
      aria-selected={selected}
      className={selected ? 'tab tab-on' : 'tab'}
      onClick={() => onSelect(value)}
    >
      {children}
    </button>
  )
}

/** Re-indent for reading. If it will not parse, show the bytes unaltered
 *  rather than nothing — the file is the thing being inspected. */
function prettyPrint(raw: string): string {
  try {
    return JSON.stringify(JSON.parse(raw), null, 2)
  } catch {
    return raw
  }
}
