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
import { Disclosure } from '../ui/Disclosure'
import { PACK_TERMS } from './terminology'
import { Popover } from '../ui/Popover'
import { useMediaQuery } from '../shell/useMediaQuery'
import { entries, outcomeLabel, text } from './logicModel'
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
    actions={<div className={styles.actions}>{!narrow && actions}
      <Popover title="Pack details" trigger={<Button variant="quiet" aria-label="More pack actions">{narrow ? '…' : 'More'}</Button>}>
        <dl className={styles.metadata}>
          <div><dt>Title</dt><dd>{text(doc?.title, packId)}</dd></div>
          <div><dt>Version</dt><dd>{text(doc?.version)}</dd></div>
          <div><dt>Pack ID</dt><dd>{text(doc?.id, packId)}</dd></div>
        </dl>
        {details}
        <div className={styles.moreActions}>{narrow && actions}<ButtonLink variant="quiet" to={`${base}?view=document`}>Full document</ButtonLink>{hasMatrix && <ButtonLink variant="quiet" to={`${base}/matrix`}>Saved cases</ButtonLink>}</div>
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
  const escalation = isRecord(doc.escalation) ? doc.escalation : undefined
  const target = isRecord(escalation?.target) ? escalation.target : undefined
  return <section className={styles.overview} aria-label="Pack overview">
    <section>
      <h2>What this pack needs</h2>
      <InspectionRow label={PACK_TERMS.applicability.label} value={doc.applicability ? 'View conditions' : 'No scope restriction set'}
        aria-label="View conditions" current={at === '/applicability'} onClick={() => select('/applicability')} />
      <InspectionRow label={PACK_TERMS.evidenceRequirements.label}
        value={`${evidence.filter(x => isRecord(x) && x.required === true).length} required · ${evidence.filter(x => isRecord(x) && x.required === false).length} optional`}
        aria-label="View evidence needed" current={at === '/evidenceRequirements'} onClick={() => select('/evidenceRequirements')} />
    </section>
    <section className={styles.group} aria-label={PACK_TERMS.outcomes.label}>
      <div className={styles.sectionHeading}><h2>{PACK_TERMS.outcomes.label}</h2>
        <ButtonLink variant="quiet" to={`/packs/${encodeURIComponent(packId)}?view=logic`}>View logic</ButtonLink>
      </div>
      <div className={styles.outcomes}>{outcomes.slice(0, 4).map((value, index) =>
        <InspectionRow key={index} label={isRecord(value) ? text(value.label, text(value.id)) : 'Unrecognized outcome'}
          aria-label={`View outcome: ${isRecord(value) ? text(value.label, text(value.id)) : 'Unrecognized outcome'}`}
          current={at === `/outcomes/${index}`} onClick={() => select(`/outcomes/${index}`)} />)}
        {outcomes.length > 4 && <InspectionRow label={`View all ${outcomes.length} outcomes`} current={at === '/outcomes'} onClick={() => select('/outcomes')} />}
        {outcomes.length === 0 && <p>No outcomes are declared.</p>}
      </div>
      <p className={styles.muted}>{entries(doc.rules).length} decision rules and {entries(doc.exceptions).length} special cases contribute to this decision.</p>
    </section>
    <section className={styles.group}>
      <dl className={styles.metadata}>
        <div><dt>{PACK_TERMS.fallbackOutcome.label}</dt><dd>{doc.fallbackOutcome === undefined ? 'Not declared' : outcomeLabel(doc, doc.fallbackOutcome)}</dd></div>
        <div><dt>Handoff target</dt><dd>{text(target?.name)}</dd></div>
      </dl>
      <p className={styles.muted}>A fallback outcome does not itself request a handoff.</p>
    </section>
    <section>
      <InspectionRow label={PACK_TERMS.sources.label} value={entries(doc.sources).length} aria-label="View source references"
        current={at === '/sources'} onClick={() => select('/sources')} />
      {typeof doc.description === 'string' && <Disclosure title="Author description" className={styles.description}><p>{doc.description}</p></Disclosure>}
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
