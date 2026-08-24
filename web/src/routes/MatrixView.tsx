import { Link, useParams } from 'react-router-dom'
import { CoverageReport } from '../components/CoverageReport'
import { MatrixRowList } from '../components/MatrixRowList'
import { Empty, ErrorBox, Loading, Pill, Section } from '../components/primitives'
import { usePackMatrix } from '../mcp/queries'
import type { PackTestEntry } from '../mcp/types'

/**
 * The project's declared matrices, run — every pack's, or one pack's.
 *
 * Two things are on this page and they answer different questions. The rows say
 * whether what a project wrote about its own packs still holds. The coverage
 * report says how much of each pack the rows are about, and it is the one that
 * usually has something to say: a matrix can pass every row it has while
 * stating nothing about most of the outcomes its pack declares. So the gaps
 * lead, and the rows follow.
 *
 * Neither is a claim about this runtime, and no row is an authorization. The
 * payload says so in its own label, which is shown rather than summarized.
 */
export function MatrixView() {
  const { packId } = useParams<{ packId?: string }>()
  const { data, error, isPending, isFetching } = usePackMatrix(packId)

  if (isPending) return <Loading what={packId ? `the ${packId} matrix` : "the project's matrices"} />
  if (error) {
    return <ErrorBox title={packId ? `Could not run the ${packId} matrix` : 'Could not run the matrices'} error={error} />
  }
  if (!data) return null

  const packs = data.packs ?? []

  return (
    <article className="detail">
      <nav className="crumbs">
        <Link to="/">Project</Link>
        <span aria-hidden="true">/</span>
        {packId ? (
          <>
            <Link to={`/packs/${encodeURIComponent(packId)}`}>{packId}</Link>
            <span aria-hidden="true">/</span>
            <span>Matrix</span>
          </>
        ) : (
          <span>Matrix</span>
        )}
      </nav>

      <header className="detail-head">
        <h1>{packId ? `${packId} matrix` : 'Project matrix'}</h1>
        <p className="ids">
          <Pill tone={data.status === 'passed' ? 'strong' : 'quiet'}>{data.status}</Pill>
          <span>
            {data.summary.passed} of {data.summary.total}{' '}
            {data.summary.total === 1 ? 'row' : 'rows'} passed
          </span>
          {data.summary.mismatched > 0 && (
            <Pill tone="strong">{data.summary.mismatched} mismatched</Pill>
          )}
          {isFetching && <span className="quiet">re-running…</span>}
        </p>
        <p className="meta">
          {data.configPath && <code>{data.configPath}</code>}
          {data.configVersion && <span>configVersion {data.configVersion}</span>}
          {data.evaluatorSpecVersion && <span>evaluator {data.evaluatorSpecVersion}</span>}
          {data.experimental && <span>experimental surface</span>}
        </p>
        {data.status === 'skipped' && (
          <p className="note note-warn">
            No row ran. A run in which nothing ran is reported skipped and never
            passed — a green gate over zero rows would say a project was tested
            when nothing was.
          </p>
        )}
      </header>

      {packs.length === 0 ? (
        <Empty>
          This project declares no pack with a matrix. Add a <code>matrix</code> to
          an entry in <code>jpack.json</code> to see its rows here.
        </Empty>
      ) : (
        packs.map((entry) => <PackMatrixEntry key={entry.id} entry={entry} />)
      )}

      {data.label && (
        <p className="note">
          <strong>What this reports.</strong> {data.label}
        </p>
      )}
    </article>
  )
}

function PackMatrixEntry({ entry }: { entry: PackTestEntry }) {
  const rows = entry.rows ?? []
  return (
    <section className="matrix-entry">
      <header className="matrix-entry-head">
        <h2>
          <Link to={`/packs/${encodeURIComponent(entry.id)}`}>{entry.id}</Link>
        </h2>
        <Pill tone={entry.status === 'passed' ? 'quiet' : 'strong'}>{entry.status}</Pill>
        {entry.packVersion && <Pill tone="quiet">v{entry.packVersion}</Pill>}
        <span className="quiet">
          {entry.summary.passed}/{entry.summary.total} rows
        </span>
      </header>
      <p className="meta">
        {entry.matrixPath && <code>{entry.matrixPath}</code>}
        {entry.origins?.map((origin) => (
          <span key={origin.origin}>
            {origin.rows} {origin.rows === 1 ? 'row' : 'rows'} from {origin.origin}
          </span>
        ))}
      </p>
      {entry.detail && <p className="note note-warn">{entry.detail}</p>}

      <Section title="Coverage">
        <CoverageReport coverage={entry.coverage} />
      </Section>

      <Section title="Rows" count={rows.length}>
        {rows.length === 0 ? (
          <Empty>This pack declares no matrix, so no row ran for it.</Empty>
        ) : (
          <MatrixRowList rows={rows} />
        )}
      </Section>
    </section>
  )
}
