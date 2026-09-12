import { Link } from 'react-router-dom'
import type { PackDocument } from '../mcp/types'
import { isRecord } from './document/MisshapenMember'
import type { RootMember } from './document/members'
import styles from './PackWorkspace.module.css'

export type PackSection = 'overview' | 'rules' | 'evidence' | 'document' | 'test'
export const PACK_GROUPS: Record<'rules' | 'evidence', readonly RootMember[]> = {
  rules: ['decision', 'applicability', 'outcomes', 'rules', 'exceptions', 'fallbackOutcome', 'escalation'],
  evidence: ['evidenceRequirements', 'sources']
}

/** Route navigation shares one order on reading, evaluation and saved-case pages. */
export function PackNavigation({ packId, current }: { packId: string; current: PackSection }) {
  const base = `/packs/${encodeURIComponent(packId)}`
  return <nav className={styles.navigation} aria-label="Pack sections">
    {([
      ['overview', 'Overview'], ['rules', 'Rules'], ['evidence', 'Evidence & sources'],
      ['test', 'Test'], ['document', 'Full document']
    ] as const).map(([value, label]) => <Link key={value}
      to={value === 'test' ? `${base}/evaluate` : `${base}?view=${value}`}
      aria-current={current === value ? 'page' : undefined}>{label}</Link>)}
  </nav>
}

/** A summary of declared content, never a computed decision or validity verdict. */
export function PackOverview({ document: doc }: { document: PackDocument }) {
  const decision = isRecord(doc.decision) ? doc.decision : undefined
  const count = (value: unknown) => Array.isArray(value) ? value.length : '—'
  return <section className={styles.overview} aria-label="Pack overview">
    <div>
      <h2>{typeof doc.title === 'string' ? doc.title : 'Untitled pack'}</h2>
      {typeof doc.description === 'string' && doc.description !== '' && <p>{doc.description}</p>}
    </div>
    <div className={styles.question}>
      <span className={styles.label}>Decision question</span>
      <p>{typeof decision?.question === 'string' && decision.question !== ''
        ? decision.question : 'No decision question is declared.'}</p>
      {typeof decision?.intent === 'string' && <span className={styles.muted}>{decision.intent}</span>}
    </div>
    <dl className={styles.counts}>
      <div><dt>Rules</dt><dd>{count(doc.rules)}</dd></div>
      <div><dt>Outcomes</dt><dd>{count(doc.outcomes)}</dd></div>
      <div><dt>Evidence requirements</dt><dd>{count(doc.evidenceRequirements ?? [])}</dd></div>
      <div><dt>Sources</dt><dd>{count(doc.sources ?? [])}</dd></div>
    </dl>
    <section className={styles.outcomes} aria-label="Declared outcomes">
      <h3>Possible outcomes</h3>
      {Array.isArray(doc.outcomes) && doc.outcomes.length > 0 ? <ul>
        {doc.outcomes.map((outcome, index) => <li key={index}>
          <strong>{isRecord(outcome) && typeof outcome.label === 'string' ? outcome.label : 'Unrecognized outcome'}</strong>
          {isRecord(outcome) && typeof outcome.description === 'string' && <span>{outcome.description}</span>}
        </li>)}
      </ul> : <p>No outcomes are declared.</p>}
    </section>
    <p className={styles.muted}>Explore the rules and their sources, then try inputs in Test. A returned outcome is separate from whether a saved test case passes.</p>
    <dl className={styles.metadata}>
      <div><dt>Version</dt><dd>{typeof doc.version === 'string' ? doc.version : 'Not declared'}</dd></div>
      <div><dt>Pack ID</dt><dd>{typeof doc.id === 'string' ? doc.id : 'Not declared'}</dd></div>
    </dl>
  </section>
}

export function TestNavigation({ packId, saved = false, hasMatrix }: {
  packId: string; saved?: boolean; hasMatrix: boolean
}) {
  const base = `/packs/${encodeURIComponent(packId)}`
  return <nav className={styles.subnavigation} aria-label="Test modes">
    <Link to={`${base}/evaluate`} aria-current={!saved ? 'page' : undefined}>Try inputs</Link>
    {hasMatrix && <Link to={`${base}/matrix`} aria-current={saved ? 'page' : undefined}>Saved cases</Link>}
  </nav>
}
