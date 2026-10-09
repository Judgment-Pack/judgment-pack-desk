import { Message } from '../i18n/Message'
import { msg, useLocale } from '../i18n'
import { useQuery } from '@tanstack/react-query'
import { Link } from 'react-router-dom'
import { Button } from '../ui/Button'
import { Empty, Loading, Pill, Section } from '../components/primitives'
import { useGraphDocument } from '../mcp/queries'
import { useMcp } from '../mcp/McpProvider'
import { GraphFindingsPanel, Diagnostic } from './GraphFindings'
import { GraphLabels } from './GraphLabels'
import { readGraphPlan, shownMessage, shownPath, type GraphFeed, type GraphPlan as Plan, type GraphStep } from './client'

/**
 * The Plan view of one graph (ADR-0011, section 3): the runtime's
 * `graph explain` for the configured id, and the findings beside it.
 *
 * The plan carries no digest of the document it read, so it is shown as its
 * own answer, beside the findings and never joined to the diagram or to a
 * matrix run. The findings carry one (`graphSha256`) and are compared with the
 * digest of the document the diagram was drawn from.
 */
export function GraphPlanView({ graphId }: { graphId: string }) {
  useLocale()
  const { graphDocumentSupported } = useMcp()
  const served = useGraphDocument(graphId)
  const sha256 = graphDocumentSupported && !served.error ? served.data?.meta.sha256 : undefined
  const query = useQuery({ queryKey: ['desk-graph-plan', graphId], queryFn: ({ signal }) => readGraphPlan(graphId, signal), refetchOnWindowFocus: false, retry: false })
  const plan = query.data
  return <section aria-label={msg("Graph plan")}>
    <Section title={msg("Plan")}>
      <>
        <p className="quiet">{msg("From the runtime's graph explain. The plan names no digest of the document it read, so it is not tied to the diagram.")}</p>
        {query.isPending && <Loading what={msg("the plan")} />}
        {query.error && <p className="note note-warn">{query.error.message}</p>}
        {plan && <PlanBody plan={plan} />}
        <Button variant="quiet" onClick={() => void query.refetch()} disabled={query.isFetching}>{query.isFetching && !query.isPending ? msg("Reading…") : msg("Read the plan again")}</Button>
      </>
    </Section>
    <GraphFindingsPanel graphId={graphId} sha256={sha256} />
  </section>
}

function PlanBody({ plan }: { plan: Plan }) {
  useLocale()
  const steps = plan.steps ?? []
  const known = [plan.graphPath, plan.configPath, ...steps.map(step => step.path)]
  return <>
    <GraphLabels labels={plan} />
    <p className="ids"><Pill tone="neutral"><span lang="en">{plan.status}</span></Pill>
      {plan.graphId && <span><Message text={"graph id <0/>"} slots={[plan.graphId]} /></span>}
      {plan.graphVersion && <span><Message text={"version <0/>"} slots={[plan.graphVersion]} /></span>}
      {plan.resultNode && <span><Message text={"result <0/>"} slots={[plan.resultNode]} /></span>}
      {plan.graphPath && <code>{shownPath(plan.graphPath)}</code>}
    </p>
    {(plan.diagnostics ?? []).length > 0 && <ul className="findings">{plan.diagnostics!.map((diagnostic, index) => <Diagnostic key={index} diagnostic={diagnostic} known={known} />)}</ul>}
    {plan.status !== 'planned' && <p className="quiet">{msg("The runtime has no plan for this graph. The findings below are where its checks are.")}</p>}
    {plan.status === 'planned' && steps.length === 0 && <Empty>{msg("The plan has no steps.")}</Empty>}
    {steps.length > 0 && <ol className="cards">{steps.map((step, index) => <Step key={index} step={step} known={known} />)}</ol>}
  </>
}

function Step({ step, known }: { step: GraphStep; known: (string | undefined)[] }) {
  useLocale()
  return <li className="card">
    <div className="card-head">
      <h3>{step.order !== undefined && <>{step.order}. </>}{step.node}</h3>
      {step.pack && <Link to={`/packs/${encodeURIComponent(step.pack)}`}><Message text={"pack <0/>"} slots={[step.pack]} /></Link>}
      {step.packVersion && <Pill tone="quiet">v{step.packVersion}</Pill>}
    </div>
    <p className="meta">
      {step.packId && <code>{step.packId}</code>}
      {step.path && <code>{shownPath(step.path)}</code>}
    </p>
    {step.detail && <p className="note note-warn" lang="en">{shownMessage(step.detail, known)}</p>}
    {(step.feeds ?? []).length > 0
      ? <ul>{step.feeds!.map((feed, index) => <li key={index}><Feed feed={feed} /></li>)}</ul>
      : <p className="quiet">{msg("Fed by no other step.")}</p>}
  </li>
}

function Feed({ feed }: { feed: GraphFeed }) {
  useLocale()
  return <>
    <Message text={"from <0/>"} slots={[feed.from]} />
    {feed.fact !== undefined && <> · <Message text={"fact <0/>"} slots={[<code key="fact">{feed.fact}</code>]} /></>}
    {feed.evidence !== undefined && <> · <Message text={"evidence requirement <0/>"} slots={[feed.evidence]} /></>}
    {feed.onUnresolved !== undefined && <> · <Message text={"when unresolved <0/>"} slots={[<code key="unresolved">{feed.onUnresolved}</code>]} /></>}
  </>
}
