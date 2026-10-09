import { Message } from '../i18n/Message'
import { msg, useLocale } from '../i18n'
import { useQuery } from '@tanstack/react-query'
import { Button } from '../ui/Button'
import { Loading, Pill, Section } from '../components/primitives'
import { GraphLabels } from './GraphLabels'
import { readGraphFindings, shownMessage, shownPath, type GraphDiagnostic, type GraphFinding, type GraphFindings } from './client'

export const GRAPH_FINDINGS_KEY = ['desk-graph-findings'] as const

/** The runtime's `graph validate` for the project, read when the page opens and when the owner asks again. */
export function useGraphFindings(enabled = true) {
  return useQuery({ queryKey: GRAPH_FINDINGS_KEY, queryFn: ({ signal }) => readGraphFindings(signal), enabled, refetchOnWindowFocus: false, retry: false })
}

/** One diagnostic, in the runtime's words. */
export function Diagnostic({ diagnostic, known }: { diagnostic: GraphDiagnostic; known?: (string | undefined)[] }) {
  return <li>
    {diagnostic.code && <code lang="en">{diagnostic.code}</code>}{diagnostic.code && diagnostic.message ? ' ' : ''}
    {diagnostic.message && <span lang="en">{shownMessage(diagnostic.message, known)}</span>}
    {diagnostic.instancePath ? <> <code lang="en">{diagnostic.instancePath}</code></> : null}
  </li>
}

/**
 * `findings` is the id of one graph, or nothing for all. The status is the
 * runtime's own, shown as its status and never as Desk's verdict. Where the
 * graph's document is in hand, `sha256` is its digest: the runtime reports
 * `graphSha256` for what it checked, and where the two differ the findings are
 * about another revision of the file (ADR-0011, section 3).
 */
export function GraphFindingsPanel({ graphId, sha256 }: { graphId?: string; sha256?: string }) {
  useLocale()
  const query = useGraphFindings()
  const answer = query.data
  return <Section title={msg("Findings")}>
    <>
      <p className="quiet">{msg("From the runtime's graph validate. It checks the graph documents and validates no pack.")}</p>
      {query.isPending && <Loading what={msg("findings")} />}
      {query.error && <p className="note note-warn">{query.error.message}</p>}
      {answer && <Findings answer={answer} graphId={graphId} sha256={sha256} />}
      <Button variant="quiet" onClick={() => void query.refetch()} disabled={query.isFetching}>{query.isFetching && !query.isPending ? msg("Checking…") : msg("Check again")}</Button>
    </>
  </Section>
}

function Findings({ answer, graphId, sha256 }: { answer: GraphFindings; graphId?: string; sha256?: string }) {
  useLocale()
  const all = answer.graphs ?? []
  const rows = graphId === undefined ? all : all.filter(row => row.id === graphId)
  const known = all.flatMap(row => [row.path, row.rowsPath, answer.configPath])
  return <>
    <GraphLabels labels={answer} />
    <p className="ids"><Pill tone="neutral"><span lang="en">{answer.status}</span></Pill>
      {answer.summary && <span><Message text={"summary <0/>"} slots={[<code lang="en" key="summary">{`passed ${answer.summary.passed}, failed ${answer.summary.failed}, total ${answer.summary.total}`}</code>]} /></span>}</p>
    {(answer.diagnostics ?? []).length > 0 && <ul className="findings">{answer.diagnostics!.map((diagnostic, index) => <Diagnostic key={index} diagnostic={diagnostic} known={known} />)}</ul>}
    {rows.length === 0 && graphId !== undefined && <p className="quiet">{msg("The runtime's findings do not name this graph.")}</p>}
    {answer.status === 'skipped' && all.length === 0 && <p className="quiet">{msg("The runtime skipped the check: this project declares no graph.")}</p>}
    {rows.map(row => <FindingRow key={row.id} row={row} known={known} elsewhere={graphId === undefined} sha256={sha256} />)}
  </>
}

function FindingRow({ row, known, elsewhere, sha256 }: { row: GraphFinding; known: (string | undefined)[]; elsewhere: boolean; sha256?: string }) {
  useLocale()
  const other = sha256 !== undefined && row.graphSha256 !== undefined && sha256 !== row.graphSha256
  return <div className="card">
    <p className="ids">
      {elsewhere && <strong>{row.id}</strong>}
      <Pill tone="neutral"><span lang="en">{row.status}</span></Pill>
      {row.path && <code>{shownPath(row.path)}</code>}
      {row.graphSha256 && <span className="quiet"><Message text={"checked bytes <0/>"} slots={[<code key="digest">{row.graphSha256}</code>]} /></span>}
    </p>
    {other && <p className="note note-warn">{msg("These findings are about another revision of the graph file than the diagram shows.")}</p>}
    {(row.diagnostics ?? []).length > 0 && <ul className="findings">{row.diagnostics!.map((diagnostic, index) => <Diagnostic key={index} diagnostic={diagnostic} known={known} />)}</ul>}
  </div>
}
