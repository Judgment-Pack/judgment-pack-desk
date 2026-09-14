import { ConditionTree } from '../../packs/document/ConditionTree'
import { Digest } from '../../ui/Digest'
import { Disclosure } from '../../ui/Disclosure'
import type { Ledger, SourceRecord } from '../ledger'
import type { RunState } from '../run'
import type { Selection } from './DraftPanels'
import styles from './ResearchAuthoring.module.css'

function Verification({ record }: { record: SourceRecord }) {
  const v = record.verification
  if (v.state === 'unchecked') {
    return <p className={styles.detail}>Receipt unchecked: the session is not sealed and verified yet.</p>
  }
  if (v.state === 'verified') {
    return (
      <p className={styles.detail}>
        Receipt verified at {v.at} under the pinned key {v.keyId}: signature, chain, seal and re-digest all hold. This establishes byte lineage within the gateway's bounds, not truth, currency or origin.
      </p>
    )
  }
  return (
    <div className={styles.section}>
      <p className={styles.detail}>Receipt verification failed at {v.at}. The findings, as the verifier reported them:</p>
      <ul className={styles.unknowns}>
        {v.findings.map((finding, index) => (
          <li key={index}>
            {finding.status}
            {finding.callIndex !== null ? ` (receipt ${finding.callIndex})` : ' (session)'}
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

export function SourceInspector({ selection, ledger, state }: { selection: Selection; ledger: Ledger; state: RunState }) {
  if (selection === null) {
    return <p className={styles.empty}>Choose a source, an excerpt or a rule to inspect it here.</p>
  }
  if (selection.kind === 'rule') {
    const latest = state.candidates.at(-1)
    const rules = ((latest?.document as { rules?: unknown })?.rules ?? []) as Record<string, unknown>[]
    const rule = rules.find((r) => r.id === selection.id)
    if (!rule) return <p className={styles.empty}>Rule {selection.id} is not in the current draft.</p>
    const refs = Array.isArray(rule.sourceRefs) ? (rule.sourceRefs as string[]) : []
    const citations = state.citations.filter((c) => refs.includes(c.sourceId))
    return (
      <div className={styles.inspector}>
        <section className={styles.section}>
          <h3>Rule {selection.id}</h3>
          <p className={styles.detail}>{typeof rule.description === 'string' ? rule.description : ''}</p>
          <ConditionTree readOnly structured condition={rule.when} at={`/rules/${selection.id}/when`} />
          <dl className={styles.facts}>
            <dt>Outcome</dt>
            <dd>{String(rule.outcome ?? '—')}</dd>
            <dt>On unknown</dt>
            <dd>{String(rule.onUnknown ?? '—')}</dd>
          </dl>
        </section>
        <section className={styles.section}>
          <h4>Sources this rule cites</h4>
          {refs.length === 0 && <p className={styles.detail}>None. This rule is an assumption unless a source is added.</p>}
          {citations.map((citation) => {
            const excerpt = citation.excerptId ? ledger.excerpt(citation.excerptId) : undefined
            return (
              <div key={citation.sourceId} className={styles.section}>
                <p className={styles.detail}>
                  <strong>{citation.sourceId}</strong> · {citation.traced ? 'traced' : `not traced: ${citation.reason}`}
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
  if (!record) return <p className={styles.empty}>Nothing recorded under {selection.id}.</p>
  const document = record.document
  return (
    <div className={styles.inspector}>
      <section className={styles.section}>
        <h3>
          {record.id} · {record.kind === 'search' ? 'search' : 'page'}
        </h3>
        {record.kind === 'search' && <p className={styles.detail}>Query: {record.request.query}</p>}
        {document && <p className={styles.detail}>{document.title || '(untitled)'}</p>}
        {record.request.url && (
          <p className={styles.url}>
            <a href={record.request.url} target="_blank" rel="noreferrer noopener">
              {record.request.url}
            </a>
          </p>
        )}
        {record.failure !== null && <p className={styles.detail}>Retrieval failed: {record.failure}</p>}
      </section>
      {excerpt && document && (
        <section className={styles.section}>
          <h4>Excerpt {excerpt.id}</h4>
          <p className={styles.hint}>
            Characters {excerpt.start}–{excerpt.end} of the rendered text, shown in context.
          </p>
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
          <h4>Dates</h4>
          <dl className={styles.facts}>
            <dt>Retrieved</dt>
            <dd>{record.acquisition?.observedAt || record.requestedAt} (the gateway's adapter read it then)</dd>
            <dt>Page declares issued</dt>
            <dd>{document.pageDates.issued ?? 'unknown'}</dd>
            <dt>Page declares modified</dt>
            <dd>{document.pageDates.modified ?? 'unknown'}</dd>
            <dt>Reader reported</dt>
            <dd>{document.providerReportedTime ?? 'nothing'}{document.providerReportedTime ? ' (may be the server’s Last-Modified; not a publication date)' : ''}</dd>
            {document.httpStatus !== undefined && (
              <>
                <dt>HTTP at the page</dt>
                <dd>{document.httpStatus}</dd>
              </>
            )}
            <dt>Rendered text</dt>
            <dd>{document.text.length} characters{document.pages ? `, ${document.pages} pages` : ''}</dd>
          </dl>
        </section>
      )}
      {record.hits && (
        <section className={styles.section}>
          <h4>Hits (provider snippets, not pages)</h4>
          <ol className={styles.unknowns}>
            {record.hits.map((hit) => (
              <li key={hit.rank}>
                <strong>{hit.title || '(untitled)'}</strong>
                <div className={styles.url}>{hit.url}</div>
                <div className={styles.hint}>{hit.snippet}</div>
                {hit.providerDate && <div className={styles.hint}>provider-reported date: {hit.providerDate}</div>}
              </li>
            ))}
          </ol>
        </section>
      )}
      <section className={styles.section}>
        <h4>Receipt</h4>
        {record.acquisition ? (
          <>
            <Verification record={record} />
            <dl className={styles.facts}>
              <dt>Session</dt>
              <dd className={styles.mono}>{record.acquisition.session} / {record.acquisition.callIndex}</dd>
              <dt>Result digest</dt>
              <dd>
                <Digest value={record.acquisition.resultDigest.replace(/^sha256:/, '')} />
              </dd>
              <dt>Endpoint</dt>
              <dd className={styles.mono}>{record.acquisition.endpoint ?? 'null'}</dd>
              <dt>Snapshot</dt>
              <dd className={styles.mono}>{record.acquisition.snapshot ?? 'null'}</dd>
              <dt>Peer identity</dt>
              <dd className={styles.mono}>{record.acquisition.peerIdentity ?? 'null'}</dd>
              <dt>Adapter</dt>
              <dd className={styles.mono}>
                {record.acquisition.adapter ? `${record.acquisition.adapter.name} ${record.acquisition.adapter.version} ${record.acquisition.adapter.digest}` : 'null'}
              </dd>
            </dl>
          </>
        ) : (
          <p className={styles.detail}>No receipt: the acquisition did not complete.</p>
        )}
      </section>
      {record.excerpts.length > 0 && (
        <section className={styles.section}>
          <h4>Excerpts recorded from this source</h4>
          <ul className={styles.unknowns}>
            {record.excerpts.map((item) => (
              <li key={item.id}>
                <span className={styles.badge}>{item.id}</span> characters {item.start}–{item.end}
                <blockquote className={styles.excerpt}>{item.text}</blockquote>
              </li>
            ))}
          </ul>
        </section>
      )}
      {record.response && (
        <Disclosure title="Receipt and result, as received">
          <pre className={styles.excerpt}>{record.response.text.length > 20_000 ? record.response.text.slice(0, 20_000) + '\n… (truncated for display)' : record.response.text}</pre>
        </Disclosure>
      )}
    </div>
  )
}
