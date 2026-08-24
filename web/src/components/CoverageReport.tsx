import { parseProbe } from '../mcp/canonical'
import type { MatrixProbe } from '../mcp/types'
import { Empty } from './primitives'

/**
 * The derived coverage report (ADR-0014, ADR-0016, ADR-0023).
 *
 * The report's job is showing gaps, so the gaps take the top of it and the
 * covered probes sit underneath. That ordering is the whole point of the view:
 * a run can pass every row it has and still say nothing about most of what the
 * pack can do, and a report that led with its green rows would hide exactly
 * that.
 *
 * Nothing here gates. A missing probe moves no status and fails no run — it is
 * a fact about what the rows state, and the runtime's own sentence saying so
 * is shown verbatim rather than paraphrased.
 */

const FAMILY_LABELS: Record<string, string> = {
  outcome: 'Declared outcomes',
  reason: 'Resolution reasons',
  boundary: 'Boundary probes',
  edge: 'Graph edges',
  other: 'Other probes'
}

/** Families in the order the report reads best: what a pack declares, then
 *  what it can conclude, then the thresholds, then the graph's own seams. */
const FAMILY_ORDER = ['outcome', 'reason', 'boundary', 'edge', 'other']

function familyOf(probe: MatrixProbe): string {
  const family = parseProbe(probe.probe).family
  return FAMILY_LABELS[family] ? family : 'other'
}

export function CoverageReport({
  coverage,
  /** Set where every probe is namespaced to a graph node already. */
  groupByNode = false
}: {
  coverage: MatrixProbe[] | undefined
  groupByNode?: boolean
}) {
  if (!coverage || coverage.length === 0) {
    return (
      <Empty>
        No coverage was reported. An absent report is not an empty one — where
        the runtime said why, the entry's own status and detail above say it,
        and nothing is invented here in their place.
      </Empty>
    )
  }

  const missing = coverage.filter((probe) => probe.status !== 'covered')
  const covered = coverage.filter((probe) => probe.status === 'covered')

  return (
    <>
      <p className={missing.length > 0 ? 'note note-warn' : 'note'}>
        {missing.length === 0 ? (
          <>
            Every one of the {coverage.length} derived{' '}
            {coverage.length === 1 ? 'probe is' : 'probes are'} witnessed by a row.
          </>
        ) : (
          <>
            <strong>
              {missing.length} of {coverage.length}{' '}
              {coverage.length === 1 ? 'probe is' : 'probes are'} unwitnessed.
            </strong>{' '}
            No row states what the pack does in {missing.length === 1 ? 'this case' : 'these cases'}.
            Coverage informs and never gates, so the run's status does not reflect this.
          </>
        )}
      </p>

      {missing.length > 0 && (
        <ProbeGroups probes={missing} tone="missing" groupByNode={groupByNode} />
      )}
      {covered.length > 0 && (
        <details className="coverage-covered">
          <summary>
            {covered.length} witnessed {covered.length === 1 ? 'probe' : 'probes'}
          </summary>
          <ProbeGroups probes={covered} tone="covered" groupByNode={groupByNode} />
        </details>
      )}
    </>
  )
}

function ProbeGroups({
  probes,
  tone,
  groupByNode
}: {
  probes: MatrixProbe[]
  tone: 'missing' | 'covered'
  groupByNode: boolean
}) {
  if (groupByNode) {
    // A graph's probes are namespaced per node, and the report emits the nodes
    // in the walk's evaluation order. Grouping keeps that order rather than
    // sorting it into an order nothing on the wire states.
    const order: string[] = []
    const byNode = new Map<string, MatrixProbe[]>()
    for (const probe of probes) {
      const key = parseProbe(probe.probe).node ?? 'the graph'
      if (!byNode.has(key)) {
        byNode.set(key, [])
        order.push(key)
      }
      byNode.get(key)!.push(probe)
    }
    return (
      <>
        {order.map((node) => (
          <div key={node} className="coverage-group">
            <h4 className="coverage-group-title">
              {node === 'the graph' ? 'The graph' : <code>{node}</code>}
            </h4>
            <ProbeList probes={byNode.get(node)!} tone={tone} />
          </div>
        ))}
      </>
    )
  }

  const order = FAMILY_ORDER.filter((family) => probes.some((probe) => familyOf(probe) === family))
  return (
    <>
      {order.map((family) => (
        <div key={family} className="coverage-group">
          <h4 className="coverage-group-title">{FAMILY_LABELS[family]}</h4>
          <ProbeList probes={probes.filter((probe) => familyOf(probe) === family)} tone={tone} />
        </div>
      ))}
    </>
  )
}

function ProbeList({ probes, tone }: { probes: MatrixProbe[]; tone: 'missing' | 'covered' }) {
  return (
    <ul className={`probes probes-${tone}`}>
      {probes.map((probe) => {
        const parsed = parseProbe(probe.probe)
        return (
          <li key={probe.probe} className="probe">
            <div className="probe-head">
              <code className="probe-name">{parsed.rest}</code>
              <span className={`probe-status probe-status-${probe.status}`}>{probe.status}</span>
            </div>
            {probe.detail && <p className="probe-detail">{probe.detail}</p>}
          </li>
        )
      })}
    </ul>
  )
}
