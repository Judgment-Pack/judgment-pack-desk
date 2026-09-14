import { Link, useParams } from 'react-router-dom'
import { CoverageReport } from '../components/CoverageReport'
import { MatrixRowList } from '../components/MatrixRowList'
import { Empty, ErrorBox, Loading, Pill, Section, statusTone } from '../components/primitives'
import { recordActivity } from '../shell/consoleLog'
import { useMcp } from '../mcp/McpProvider'
import { usePackMatrix, usePacks } from '../mcp/queries'
import type { PackTest, PackTestEntry } from '../mcp/types'
import { PageHeader, PageBody } from '../ui/PageLayout'
import { PacksNavigation } from '../packs/PacksNavigation'
import { Button } from '../ui/Button'
import { PackNavigation, TestNavigation } from '../packs/PackWorkspace'

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
  const { status } = useMcp()
  const inventory = usePacks()
  const { data, error, isFetching, refetch } = usePackMatrix(packId, false)
  const run = () => {
    if (status !== 'ready' || isFetching) return
    recordActivity('Pack tests started.')
    void refetch().then(result => recordActivity(result.error ? 'Pack tests failed.' : `Pack tests completed: ${result.data?.status ?? 'no result'}.`))
  }
  const configured = (inventory.data?.packs ?? []).filter(pack => pack.matrix && (!packId || pack.id === packId))
  return <article className="detail" data-measure="full" data-layout="page">
    <PageHeader variant={packId ? 'context' : 'collection'} title="Packs" context={packId} titleHref={packId ? '/packs' : undefined}
      navigation={packId ? <PackNavigation packId={packId} current="test" /> : <PacksNavigation current="tests" />}
      actions={<Button onClick={run} disabled={status !== 'ready' || isFetching}>
        {isFetching ? 'Running…' : packId ? 'Run tests' : 'Run all tests'}
      </Button>} />
    <PageBody width="full">
      {packId && <TestNavigation packId={packId} saved hasMatrix />}
      {isFetching && <Loading what="test results" />}
      {error ? <ErrorBox title="Could not run pack tests" error={error} /> : data ? <MatrixResults data={data} packId={packId} /> : <>
        <h2 className="section-title">{packId ? 'Saved cases' : 'All pack tests'}</h2>
        <p className="quiet">Run saved cases to check expected outcomes and find coverage gaps.</p>
        {inventory.error ? <ErrorBox title="Could not list packs" error={inventory.error} />
          : inventory.isPending ? <Loading what="packs with saved cases" />
          : configured.length ? <ul className="cards">
            {configured.map(pack => <li key={pack.id} className="card">
              <Link to={`/packs/${encodeURIComponent(pack.id)}/matrix`}>{pack.id}</Link>
              <p className="quiet">Saved cases configured · No results loaded</p>
            </li>)}
          </ul> : <Empty>No saved test cases are configured{packId ? ' for this pack' : ''}.</Empty>}
      </>}
    </PageBody>
  </article>
}

function MatrixResults({ data, packId }: { data: PackTest; packId?: string }) {
  const packs = data.packs ?? []
  return <>
      <header className="detail-head">
        <h2 className="section-title">{packId ? 'Saved cases' : 'All pack tests'}</h2>
        <p className="ids">
          <Pill tone={statusTone(data.status)}>{data.status}</Pill>
          <span>
            {data.summary.passed} of {data.summary.total}{' '}
            {data.summary.total === 1 ? 'row' : 'rows'} passed
          </span>
          {data.summary.mismatched > 0 && (
            <Pill tone="danger">{data.summary.mismatched} mismatched</Pill>
          )}
          <span className="quiet">Last run</span>
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
  </>
}

function PackMatrixEntry({ entry }: { entry: PackTestEntry }) {
  const rows = entry.rows ?? []
  return (
    <section className="matrix-entry">
      <header className="matrix-entry-head">
        <h2>
          <Link to={`/packs/${encodeURIComponent(entry.id)}`}>{entry.id}</Link>
        </h2>
        <Pill tone={statusTone(entry.status)}>{entry.status}</Pill>
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
          <Empty>
            No rows were reported for this pack{entry.detail ? ' — the note above says why' : ''}.
          </Empty>
        ) : (
          <MatrixRowList rows={rows} />
        )}
      </Section>
    </section>
  )
}
