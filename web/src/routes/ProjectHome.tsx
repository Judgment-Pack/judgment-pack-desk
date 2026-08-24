import { Link } from 'react-router-dom'
import { ErrorBox, Loading, Pill } from '../components/primitives'
import { useConfiguredGraphs, usePacks } from '../mcp/queries'
import type { PackSummary } from '../mcp/types'

/**
 * The project, as the runtime resolves it: what it declares, and the two
 * rehearsals that can be run over those declarations.
 *
 * The three entries are gated on what the project actually declares rather
 * than always offered:
 *
 * - **Packs** come from `list_packs`, which reports the matrix flag per pack —
 *   so whether a matrix exists costs nothing to know.
 * - **Graphs** come from `experimental_list_graphs` where the runtime serves
 *   it (ADR-0029) — one call that evaluates nothing. Against a runtime with no
 *   such tool the fallback is what this page always did: run the graph walk to
 *   find out, affordable because the tool writes nothing (a row is a rehearsal,
 *   not a decision) and the result is the same cached query the graphs page
 *   then reads.
 */
export function ProjectHome() {
  const { data, error, isPending } = usePacks()
  const graphs = useConfiguredGraphs()

  if (isPending) return <Loading what="the project" />
  if (error) return <ErrorBox title="Could not read the project" error={error} />

  const packs = data?.packs ?? []
  const withMatrix = packs.filter((pack) => pack.matrix)

  return (
    <>
      <header className="detail-head">
        <h1>This project</h1>
        <p className="meta">
          {data?.configPath && <code>{data.configPath}</code>}
          {data?.configVersion && <span>configVersion {data.configVersion}</span>}
          <span>
            {packs.length} {packs.length === 1 ? 'pack' : 'packs'}
          </span>
        </p>
      </header>

      <ul className="cards cards-nav">
        {withMatrix.length > 0 && (
          <li className="card card-link">
            <div className="card-head">
              <h3>
                <Link to="/matrix">Matrix and coverage</Link>
              </h3>
              <Pill tone="quiet">
                {withMatrix.length} of {packs.length}
              </Pill>
            </div>
            <p>
              Run the rows this project wrote about its own packs, and see the
              derived report of what those rows leave unsaid.
            </p>
          </li>
        )}
        {graphs.isPending ? (
          <li className="card">
            <div className="card-head">
              <h3>Graphs</h3>
              <Pill tone="quiet">running…</Pill>
            </div>
            <p>
              {graphs.fromInventory
                ? 'Reading what this project configures.'
                : 'The graph walk is running to find what this project configures.'}
            </p>
          </li>
        ) : graphs.error ? (
          <li className="card card-link">
            <div className="card-head">
              <h3>
                <Link to="/graphs">Graphs</Link>
              </h3>
              <Pill tone="danger">error</Pill>
            </div>
            <p>
              {graphs.fromInventory
                ? 'The configured graphs could not be listed — open Graphs for the error itself.'
                : 'The graph walk could not run, which says nothing about whether graphs are configured — open Graphs for the error itself.'}
            </p>
          </li>
        ) : (
          graphs.count > 0 && (
            <li className="card card-link">
              <div className="card-head">
                <h3>
                  <Link to="/graphs">Graphs</Link>
                </h3>
                <Pill tone="quiet">
                  {graphs.count} {graphs.count === 1 ? 'graph' : 'graphs'}
                </Pill>
              </div>
              <p>
                The compositions this project configures: each one's walk, its
                rows, and the coverage derived per node and per edge.
              </p>
            </li>
          )
        )}
      </ul>

      <h2 className="section-title">
        Packs<span className="count">{packs.length}</span>
      </h2>
      {packs.length === 0 ? (
        <p className="empty">
          {data?.note ?? 'This project declares no packs. Add one to jpack.json to see it here.'}
        </p>
      ) : (
        <ul className="cards">
          {packs.map((pack) => (
            <PackRow key={pack.id} pack={pack} />
          ))}
        </ul>
      )}
    </>
  )
}

function PackRow({ pack }: { pack: PackSummary }) {
  return (
    <li className="card card-link">
      <div className="card-head">
        <h3>
          <Link to={`/packs/${encodeURIComponent(pack.id)}`}>{pack.id}</Link>
        </h3>
        {pack.packVersion && <Pill tone="quiet">v{pack.packVersion}</Pill>}
        {pack.matrix && (
          <Link className="pill pill-neutral" to={`/packs/${encodeURIComponent(pack.id)}/matrix`}>
            test matrix
          </Link>
        )}
      </div>
      {pack.description && <p>{pack.description}</p>}
      <p className="meta">
        {pack.path && <code>{pack.path}</code>}
        {pack.evidenceRequirements?.length ? (
          <span>
            {pack.evidenceRequirements.length} evidence{' '}
            {pack.evidenceRequirements.length === 1 ? 'requirement' : 'requirements'}
          </span>
        ) : null}
        {pack.consultedFactPaths?.length ? (
          <span>
            {pack.consultedFactPaths.length} consulted fact{' '}
            {pack.consultedFactPaths.length === 1 ? 'path' : 'paths'}
          </span>
        ) : null}
      </p>
    </li>
  )
}
