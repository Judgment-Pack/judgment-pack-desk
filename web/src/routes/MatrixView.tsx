import { Message } from '../i18n/Message'
import { msg, useLocale } from '../i18n'
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
  useLocale()
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
    <PageHeader variant={packId ? 'context' : 'collection'} title={msg("Packs")} context={packId} titleHref={packId ? "/packs" : undefined}
      navigation={packId ? <PackNavigation packId={packId} current="test" /> : <PacksNavigation current="tests" />}
      actions={<Button onClick={run} disabled={status !== 'ready' || isFetching}>
        {isFetching ? msg("Running…") : packId ? msg("Run tests") : msg("Run all tests")}
      </Button>} />
    <PageBody width="full">
      {packId && <TestNavigation packId={packId} saved hasMatrix />}
      {isFetching && <Loading what={msg("test results")} />}
      {error ? <ErrorBox title={msg("Could not run pack tests")} error={error} /> : data ? <MatrixResults data={data} packId={packId} /> : <>
        <h2 className="section-title">{packId ? msg("Saved cases") : msg("All pack tests")}</h2>
        <p className="quiet">{msg("Run saved cases to check expected outcomes and find coverage gaps.")}</p>
        {inventory.error ? <ErrorBox title={msg("Could not list packs")} error={inventory.error} />
          : inventory.isPending ? <Loading what={msg("packs with saved cases")} />
          : configured.length ? <ul className="cards">
            {configured.map(pack => <li key={pack.id} className="card">
              <Link to={`/packs/${encodeURIComponent(pack.id)}/matrix`}>{pack.id}</Link>
              <p className="quiet">{msg("Saved cases configured · No results loaded")}</p>
            </li>)}
          </ul> : <Empty><Message text={"No saved test cases are configured<0/>."} slots={[packId ? msg(" for this pack") : '']} /></Empty>}
      </>}
    </PageBody>
  </article>
}

function MatrixResults({ data, packId }: { data: PackTest; packId?: string }) {
  useLocale()
  const packs = data.packs ?? []
  return <>
      <header className="detail-head">
        <h2 className="section-title">{packId ? msg("Saved cases") : msg("All pack tests")}</h2>
        <p className="ids">
          <Pill tone={statusTone(data.status)}>{data.status}</Pill>
          <span><Message text={"<0/> of <1/><2/><3/> passed"} slots={[data.summary.passed, data.summary.total, ' ', data.summary.total === 1 ? msg("row") : msg("rows")]} /></span>
          {data.summary.mismatched > 0 && (
            <Pill tone="danger"><Message text={"<0/> mismatched"} slots={[data.summary.mismatched]} /></Pill>
          )}
          <span className="quiet">{msg("Last run")}</span>
        </p>
        <p className="meta">
          {data.configPath && <code>{data.configPath}</code>}
          {data.configVersion && <span><Message text={"configVersion <0/>"} slots={[data.configVersion]} /></span>}
          {data.evaluatorSpecVersion && <span><Message text={"evaluator <0/>"} slots={[data.evaluatorSpecVersion]} /></span>}
          {data.experimental && <span>{msg("experimental surface")}</span>}
        </p>
        {data.status === 'skipped' && (
          <p className="note note-warn">{msg("No row ran. A run in which nothing ran is reported skipped and never passed — a green gate over zero rows would say a project was tested when nothing was.")}</p>
        )}
      </header>

      {packs.length === 0 ? (
        <Empty><Message text={"This project declares no pack with a matrix. Add a <0/> to an entry in <1/> to see its rows here."} slots={[<code>matrix</code>, <code>jpack.json</code>]} /></Empty>
      ) : (
        packs.map((entry) => <PackMatrixEntry key={entry.id} entry={entry} />)
      )}

      {data.label && (
        <p className="note">
          <strong>{msg("What this reports.")}</strong> {data.label}
        </p>
      )}
  </>
}

function PackMatrixEntry({ entry }: { entry: PackTestEntry }) {
  useLocale()
  const rows = entry.rows ?? []
  return (
    <section className="matrix-entry">
      <header className="matrix-entry-head">
        <h2>
          <Link to={`/packs/${encodeURIComponent(entry.id)}`}>{entry.id}</Link>
        </h2>
        <Pill tone={statusTone(entry.status)}>{entry.status}</Pill>
        {entry.packVersion && <Pill tone="quiet">v{entry.packVersion}</Pill>}
        <span className="quiet"><Message text={"<0/>/<1/> rows"} slots={[entry.summary.passed, entry.summary.total]} /></span>
      </header>
      <p className="meta">
        {entry.matrixPath && <code>{entry.matrixPath}</code>}
        {entry.origins?.map((origin) => (
          <span key={origin.origin}><Message text={"<0/> <1/> from <2/>"} slots={[origin.rows, origin.rows === 1 ? msg("row") : msg("rows"), origin.origin]} /></span>
        ))}
      </p>
      {entry.detail && <p className="note note-warn">{entry.detail}</p>}

      <Section title={msg("Coverage")}>
        <CoverageReport coverage={entry.coverage} />
      </Section>

      <Section title={msg("Rows")} count={rows.length}>
        {rows.length === 0 ? (
          <Empty><Message text={"No rows were reported for this pack<0/>."} slots={[entry.detail ? msg(" — the note above says why") : '']} /></Empty>
        ) : (
          <MatrixRowList rows={rows} />
        )}
      </Section>
    </section>
  )
}
