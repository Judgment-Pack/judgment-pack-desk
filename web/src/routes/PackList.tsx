import { Link } from 'react-router-dom'
import { Empty, ErrorBox, Loading, Pill } from '../components/primitives'
import { usePacks } from '../mcp/queries'
import type { PackSummary } from '../mcp/types'

/** The project's packs, as `list_packs` resolves them from jpack.json. */
export function PackList() {
  const { data, error, isPending } = usePacks()

  if (isPending) return <Loading what="the project's packs" />
  if (error) return <ErrorBox title="Could not list the packs" error={error} />

  const packs = data?.packs ?? []
  if (packs.length === 0) {
    return (
      <Empty>
        {data?.note ??
          'This project declares no packs. Add one to jpack.json to see it here.'}
      </Empty>
    )
  }

  return (
    <>
      <p className="note">
        {packs.length} {packs.length === 1 ? 'pack' : 'packs'} declared in{' '}
        <code>{data?.configPath ?? 'jpack.json'}</code>
      </p>
      <ul className="cards">
        {packs.map((pack) => (
          <PackRow key={pack.id} pack={pack} />
        ))}
      </ul>
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
        {pack.matrix && <Pill>test matrix</Pill>}
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
