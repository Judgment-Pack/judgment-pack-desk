import { Message } from '../../i18n/Message'
import { systemMessage, msg, useLocale } from '../../i18n'
import { projectLogic } from '../../packs/logicModel'
import { LogicInspector } from '../../packs/inspector/LogicInspector'
import type { PackDocument } from '../../mcp/types'
import { ConditionTree } from '../../packs/document/ConditionTree'
import { Digest } from '../../ui/Digest'
import { Disclosure } from '../../ui/Disclosure'
import type { Ledger, SourceRecord } from '../ledger'
import type { RunState } from '../run'
import type { Selection } from './DraftPanels'
import styles from './ResearchAuthoring.module.css'

function Verification({ record }: { record: SourceRecord }) {
  useLocale()
  const v = record.verification
  if (v.state === 'unchecked') {
    return <p className={styles.detail}>{msg("Receipt unchecked: the session is not sealed and verified yet.")}</p>
  }
  if (v.state === 'verified') {
    return (
      <p className={styles.detail}><Message text={"Receipt verified at <0/> under the pinned key <1/>: signature, chain, seal and re-digest all hold. This establishes byte lineage within the gateway's bounds, not truth, currency or origin."} slots={[v.at, v.keyId]} /></p>
    )
  }
  return (
    <div className={styles.section}>
      <p className={styles.detail}><Message text={"Receipt verification failed at <0/>. The findings, as the verifier reported them:"} slots={[v.at]} /></p>
      <ul className={styles.unknowns}>
        {v.findings.map((finding, index) => (
          <li key={index}>
            {finding.status}
            {finding.callIndex !== null ? msg(" (receipt {{value0}})", { value0: finding.callIndex }) : msg(" (session)")}
          </li>
        ))}
      </ul>
    </div>
  )
}

function context(text: string, start: number, end: number): { before: string; hit: string; after: string } {
  const from = Math.max(0, start - 200)
  const to = Math.min(text.length, end + 200)
  return { before: (from > 0 ? '…' : '') + text.slice(from, start), hit: text.slice(start, end), after: text.slice(end, to) + (to < text.length ? '…' : '') }
}

export function SourceInspector({ selection, ledger, state, onSelect }: { selection: Selection; ledger: Ledger; state: RunState; onSelect?: (selection: Selection) => void }) {
  useLocale()
  if (selection === null) {
    return <p className={styles.empty}>{msg("Choose a source, an excerpt or a rule to inspect it here.")}</p>
  }
  if (selection.kind === 'logic') {
    const document = state.candidates.at(-1)?.document as PackDocument | undefined
    return document ? <LogicInspector model={projectLogic(document)} at={selection.id} onSelect={id => onSelect?.({ kind: 'logic', id })} advanced={null} mainContent /> : null
  }
  if (selection.kind === 'rule') {
    const latest = state.candidates.at(-1)
    const rules = ((latest?.document as { rules?: unknown })?.rules ?? []) as Record<string, unknown>[]
    const rule = rules.find((r) => r.id === selection.id)
    if (!rule) return <p className={styles.empty}><Message text={"Rule <0/> is not in the current draft."} slots={[selection.id]} /></p>
    const refs = Array.isArray(rule.sourceRefs) ? (rule.sourceRefs as string[]) : []
    const citations = state.citations.filter((c) => refs.includes(c.sourceId))
    return (
      <div className={styles.inspector}>
        <section className={styles.section}>
          <h3><Message text={"Rule <0/>"} slots={[selection.id]} /></h3>
          <p className={styles.detail}>{typeof rule.description === 'string' ? rule.description : ''}</p>
          <ConditionTree readOnly structured condition={rule.when} at={`/rules/${selection.id}/when`} />
          <dl className={styles.facts}>
            <dt>{msg("Outcome")}</dt>
            <dd>{String(rule.outcome ?? '—')}</dd>
            <dt>{msg("On unknown")}</dt>
            <dd>{String(rule.onUnknown ?? '—')}</dd>
          </dl>
        </section>
        <section className={styles.section}>
          <h4>{msg("Sources this rule cites")}</h4>
          {refs.length === 0 && <p className={styles.detail}>{msg("None. This rule is an assumption unless a source is added.")}</p>}
          {citations.map((citation) => {
            const excerpt = citation.excerptId ? ledger.excerpt(citation.excerptId) : undefined
            return (
              <div key={citation.sourceId} className={styles.section}>
                <p className={styles.detail}>
                  <strong>{citation.sourceId}</strong> · {citation.traced ? msg("traced") : msg("not traced: {{value0}}", { value0: systemMessage(citation.reason) })}
                </p>
                {excerpt && <blockquote className={styles.excerpt}>{excerpt.text}</blockquote>}
                {citation.url && <div className={styles.url}>{citation.url}</div>}
              </div>
            )
          })}
        </section>
      </div>
    )
  }
  const excerpt = selection.kind === 'excerpt' ? ledger.excerpt(selection.id) : undefined
  const record = selection.kind === 'source' ? ledger.byId(selection.id) : excerpt ? ledger.byId(excerpt.sourceId) : undefined
  if (!record) return <p className={styles.empty}><Message text={"Nothing recorded under <0/>."} slots={[selection.id]} /></p>
  const document = record.document
  return (
    <div className={styles.inspector}>
      <section className={styles.section}>
        <h3>
          {record.id} · {record.kind === 'search' ? msg("search") : msg("page")}
        </h3>
        {record.kind === 'search' && <p className={styles.detail}><Message text={"Query: <0/>"} slots={[record.request.query]} /></p>}
        {document && <p className={styles.detail}>{document.title || msg("(untitled)")}</p>}
        {record.request.url && (
          <p className={styles.url}>
            <a href={record.request.url} target="_blank" rel="noreferrer noopener">
              {record.request.url}
            </a>
          </p>
        )}
        {record.failure !== null && <p className={styles.detail}><Message text={"Retrieval failed: <0/>"} slots={[record.failure]} /></p>}
      </section>
      {excerpt && document && (
        <section className={styles.section}>
          <h4><Message text={"Excerpt <0/>"} slots={[excerpt.id]} /></h4>
          <p className={styles.hint}><Message text={"Characters <0/>–<1/> of the rendered text, shown in context."} slots={[excerpt.start, excerpt.end]} /></p>
          {(() => {
            const c = context(document.text, excerpt.start, excerpt.end)
            return (
              <blockquote className={styles.excerpt}>
                {c.before}
                <mark>{c.hit}</mark>
                {c.after}
              </blockquote>
            )
          })()}
        </section>
      )}
      {document && (
        <section className={styles.section}>
          <h4>{msg("Dates")}</h4>
          <dl className={styles.facts}>
            <dt>{msg("Retrieved")}</dt>
            <dd><Message text={"<0/> (the gateway's adapter read it then)"} slots={[record.acquisition?.observedAt || record.requestedAt]} /></dd>
            <dt>{msg("Page declares issued")}</dt>
            <dd>{document.pageDates.issued ?? msg("unknown")}</dd>
            <dt>{msg("Page declares modified")}</dt>
            <dd>{document.pageDates.modified ?? msg("unknown")}</dd>
            <dt>{msg("Reader reported")}</dt>
            <dd>{document.providerReportedTime ?? msg("nothing")}{document.providerReportedTime ? msg(" (may be the server’s Last-Modified; not a publication date)") : ''}</dd>
            {document.httpStatus !== undefined && (
              <>
                <dt>{msg("HTTP at the page")}</dt>
                <dd>{document.httpStatus}</dd>
              </>
            )}
            <dt>{msg("Rendered text")}</dt>
            <dd>{document.pages ? msg('{{characters}} characters · Pages: {{pages}}', { characters: document.text.length, pages: document.pages }) : msg('{{count}} characters', { count: document.text.length })}</dd>
          </dl>
        </section>
      )}
      {record.hits && (
        <section className={styles.section}>
          <h4>{msg("Hits (provider snippets, not pages)")}</h4>
          <ol className={styles.unknowns}>
            {record.hits.map((hit) => (
              <li key={hit.rank}>
                <strong>{hit.title || msg("(untitled)")}</strong>
                <div className={styles.url}>{hit.url}</div>
                <div className={styles.hint}>{hit.snippet}</div>
                {hit.providerDate && <div className={styles.hint}><Message text={"provider-reported date: <0/>"} slots={[hit.providerDate]} /></div>}
              </li>
            ))}
          </ol>
        </section>
      )}
      <section className={styles.section}>
        <h4>{msg("Receipt")}</h4>
        {record.acquisition ? (
          <>
            <Verification record={record} />
            <dl className={styles.facts}>
              <dt>{msg("Session")}</dt>
              <dd className={styles.mono}>{record.acquisition.session} / {record.acquisition.callIndex}</dd>
              <dt>{msg("Result digest")}</dt>
              <dd>
                <Digest value={record.acquisition.resultDigest.replace(/^sha256:/, '')} />
              </dd>
              <dt>{msg("Endpoint")}</dt>
              <dd className={styles.mono}>{record.acquisition.endpoint ?? msg("null")}</dd>
              <dt>{msg("Snapshot")}</dt>
              <dd className={styles.mono}>{record.acquisition.snapshot ?? msg("null")}</dd>
              <dt>{msg("Peer identity")}</dt>
              <dd className={styles.mono}>{record.acquisition.peerIdentity ?? msg("null")}</dd>
              <dt>{msg("Adapter")}</dt>
              <dd className={styles.mono}>
                {record.acquisition.adapter ? `${record.acquisition.adapter.name} ${record.acquisition.adapter.version} ${record.acquisition.adapter.digest}` : msg("null")}
              </dd>
            </dl>
          </>
        ) : (
          <p className={styles.detail}>{msg("No receipt: the acquisition did not complete.")}</p>
        )}
      </section>
      {record.excerpts.length > 0 && (
        <section className={styles.section}>
          <h4>{msg("Excerpts recorded from this source")}</h4>
          <ul className={styles.unknowns}>
            {record.excerpts.map((item) => (
              <li key={item.id}><Message text={"<0/> characters <1/>–<2/><3/>"} slots={[<span className={styles.badge}>{item.id}</span>, item.start, item.end, <blockquote className={styles.excerpt}>{item.text}</blockquote>]} /></li>
            ))}
          </ul>
        </section>
      )}
      {record.response && (
        <Disclosure title={msg("Receipt and result, as received")}>
          <pre className={styles.excerpt}>{record.response.text.length > 20_000 ? record.response.text.slice(0, 20_000) + '\n' + msg('… (truncated for display)') : record.response.text}</pre>
        </Disclosure>
      )}
    </div>
  )
}
