import { Link } from 'react-router-dom'
import type { ReactNode } from 'react'
import type { PackDocument } from '../mcp/types'
import { isRecord } from './document/MisshapenMember'
import type { RootMember } from './document/members'
import { useDocumentSelection } from './document/Block'
import { Button, ButtonLink } from '../ui/Button'
import { PageHeader } from '../ui/PageLayout'
import { ExpandableText } from '../ui/ExpandableText'
import { InspectionRow } from '../ui/InspectionRow'
import { PACK_TERMS } from './terminology'
import { Popover } from '../ui/Popover'
import { useMediaQuery } from '../shell/useMediaQuery'
import { entries, text } from './logicModel'
import styles from './PackWorkspace.module.css'

export type PackSection = 'overview' | 'logic' | 'rules' | 'evidence' | 'document' | 'test'
export const PACK_GROUPS: Record<'rules' | 'evidence', readonly RootMember[]> = {
  rules: ['decision', 'applicability', 'outcomes', 'rules', 'exceptions', 'fallbackOutcome', 'escalation'],
  evidence: ['evidenceRequirements', 'sources']
}

export function PackNavigation({ packId, current }: { packId: string; current: PackSection }) {
  const base = `/packs/${encodeURIComponent(packId)}`
  const active = current === 'rules' || current === 'evidence' ? 'logic' : current
  return <nav className={styles.navigation} aria-label="Pack sections">
    {([['overview', 'Overview'], ['logic', 'Logic'], ['test', 'Tests']] as const).map(([value, label]) =>
      <Link key={value} to={value === 'test' ? `${base}/evaluate` : `${base}?view=${value}`}
        aria-current={active === value ? 'page' : undefined}>{label}</Link>)}
    {current === 'document' && <Link to={`${base}?view=document`} aria-current="page">Full document</Link>}
  </nav>
}

/** Compound header: one title, one question, one primary task, one divider. */
export function PackHeader({ packId, document: doc, current, actions, hasMatrix = false, details }: {
  packId: string; document?: PackDocument; current: PackSection; actions?: ReactNode; hasMatrix?: boolean; details?: ReactNode
}) {
  const base = `/packs/${encodeURIComponent(packId)}`
  const narrow = useMediaQuery('(max-width: 599px)')
  return <PageHeader variant="title" title={text(doc?.title, packId)}
    navigation={<PackNavigation packId={packId} current={current} />}
    actions={<div className={styles.actions}>{actions}
      <Popover title="Pack details" trigger={<Button variant="quiet" aria-label="More pack actions">{narrow ? '…' : 'More'}</Button>}>
        <dl className={styles.metadata}>
          <div><dt>Title</dt><dd>{text(doc?.title, packId)}</dd></div>
          <div><dt>Version</dt><dd>{text(doc?.version)}</dd></div>
          <div><dt>Pack ID</dt><dd>{text(doc?.id, packId)}</dd></div>
        </dl>
        {details}
        <div className={styles.moreActions}><ButtonLink variant="quiet" to={`${base}?view=document`}>Full document</ButtonLink>{hasMatrix && <ButtonLink variant="quiet" to={`${base}/matrix`}>Saved cases</ButtonLink>}</div>
      </Popover>
      {current !== 'test' && <ButtonLink variant="primary" to={`${base}/evaluate`}>Test pack</ButtonLink>}
    </div>} />
}

export function PackQuestion({ document: doc }: { document?: PackDocument }) {
  const decision = isRecord(doc?.decision) ? doc.decision : undefined
  const question = text(decision?.question, '')
  return question ? <ExpandableText text={question} label="question" /> : null
}

/** A short brief of declared content, not a computed disposition. */
export function PackOverview({ document: doc, packId = '' }: { document: PackDocument; packId?: string }) {
  const { at, select } = useDocumentSelection()
  const evidence = entries(doc.evidenceRequirements)
  const outcomes = entries(doc.outcomes)
  const decision = isRecord(doc.decision) ? doc.decision : undefined
  const question = text(decision?.question, '').trim()
  const context = [...new Set([text(decision?.intent, ''), text(doc.description, '')].map(value => value.trim()).filter(value => value && value !== question))]
  return <section className={styles.overview} aria-label="Pack overview">
    {context.length > 0 && <section><h2>About this pack</h2>{context.map((paragraph, index) => <p key={index}>{paragraph}</p>)}</section>}
    <section className={styles.group} aria-label={PACK_TERMS.outcomes.label}>
      <h2>{PACK_TERMS.outcomes.label}</h2>
      <ul className={styles.outcomes}>{outcomes.slice(0, 4).map((value, index) =>
        <li key={index}>{isRecord(value) ? text(value.label, text(value.id)) : 'Unrecognized outcome'}</li>)}</ul>
      <div>
        {outcomes.length > 4 && <p className={styles.muted}>{outcomes.length - 4} more outcomes in Logic.</p>}
        {outcomes.length === 0 && <p>No outcomes are declared.</p>}
      </div>
    </section>
    <section className={styles.group}>
      <div className={styles.sectionHeading}><h2>At a glance</h2>
        <ButtonLink variant="quiet" to={`/packs/${encodeURIComponent(packId)}?view=logic`}>View logic</ButtonLink>
      </div>
      <dl className={styles.metadata}>
        <div><dt>Evidence needed</dt><dd>{evidence.filter(x => isRecord(x) && x.required === true).length} required · {evidence.filter(x => isRecord(x) && x.required === false).length} optional</dd></div>
        <div><dt>Decision logic</dt><dd>{entries(doc.rules).length} rules · {entries(doc.exceptions).length} special cases</dd></div>
      </dl>
    </section>
    <section>
      <InspectionRow label={PACK_TERMS.sources.label} value={entries(doc.sources).length} aria-label="View source references"
        current={at === '/sources'} onClick={() => select('/sources')} />
    </section>
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
