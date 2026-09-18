import { Message } from '../i18n/Message'
import { msg, useLocale } from '../i18n'
import { Link, useSearchParams } from 'react-router-dom'
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

// A view change must not switch the active conversation for this pack.
function usePackLink() {
  const [params] = useSearchParams()
  const chat = params.get('chat')
  return (path: string) => chat ? `${path}${path.includes('?') ? '&' : '?'}chat=${encodeURIComponent(chat)}` : path
}

export function PackNavigation({ packId, current }: { packId: string; current: PackSection }) {
  useLocale()
  const base = `/packs/${encodeURIComponent(packId)}`
  const link = usePackLink()
  const active = current === 'rules' || current === 'evidence' ? 'logic' : current
  return <nav className={styles.navigation} aria-label={msg("Pack sections")}>
    {([['overview', msg('Overview')], ['logic', msg('Logic')], ['test', msg('Tests')]] as const).map(([value, label]) =>
      <Link key={value} to={link(value === 'test' ? `${base}/evaluate` : `${base}?view=${value}`)}
        aria-current={active === value ? 'page' : undefined}>{label}</Link>)}
    {current === 'document' && <Link to={link(`${base}?view=document`)} aria-current="page">{msg("Full document")}</Link>}
  </nav>
}

/** Compound header: one title, one question, one primary task, one divider. */
export function PackHeader({ packId, document: doc, current, actions, hasMatrix = false, details }: {
  packId: string; document?: PackDocument; current: PackSection; actions?: ReactNode; hasMatrix?: boolean; details?: ReactNode
}) {
  useLocale()
  const base = `/packs/${encodeURIComponent(packId)}`
  const link = usePackLink()
  const narrow = useMediaQuery('(max-width: 599px)')
  return <PageHeader variant="title" title={text(doc?.title, packId)}
    navigation={<PackNavigation packId={packId} current={current} />}
    actions={<div className={styles.actions}>{actions}
      <Popover title={msg("Pack details")} trigger={<Button variant="quiet" aria-label={msg("More pack actions")}>{narrow ? '…' : msg("More")}</Button>}>
        <dl className={styles.metadata}>
          <div><dt>{msg("Title")}</dt><dd>{text(doc?.title, packId)}</dd></div>
          <div><dt>{msg("Version")}</dt><dd>{text(doc?.version)}</dd></div>
          <div><dt>{msg("Pack ID")}</dt><dd>{text(doc?.id, packId)}</dd></div>
        </dl>
        {details}
        <div className={styles.moreActions}><ButtonLink variant="quiet" to={link(`${base}?view=document`)}>{msg("Full document")}</ButtonLink>{hasMatrix && <ButtonLink variant="quiet" to={link(`${base}/matrix`)}>{msg("Saved cases")}</ButtonLink>}</div>
      </Popover>
      {current !== 'test' && <ButtonLink variant="primary" to={link(`${base}/evaluate`)}>{msg("Test pack")}</ButtonLink>}
    </div>} />
}

export function PackQuestion({ document: doc }: { document?: PackDocument }) {
  useLocale()
  const decision = isRecord(doc?.decision) ? doc.decision : undefined
  const question = text(decision?.question, '')
  return question ? <ExpandableText text={question} label={msg("question")} /> : null
}

/** A short brief of declared content, not a computed disposition. */
export function PackOverview({ document: doc, packId = '', logicHref, onViewLogic, onViewSources }: { document: PackDocument; packId?: string; logicHref?: string; onViewLogic?: () => void; onViewSources?: () => void }) {
  useLocale()
  const { at, select } = useDocumentSelection()
  const evidence = entries(doc.evidenceRequirements)
  const outcomes = entries(doc.outcomes)
  const decision = isRecord(doc.decision) ? doc.decision : undefined
  const question = text(decision?.question, '').trim()
  const context = [...new Set([text(decision?.intent, ''), text(doc.description, '')].map(value => value.trim()).filter(value => value && value !== question))]
  return <section className={styles.overview} aria-label={msg("Pack overview")}>
    {context.length > 0 && <section><h2>{msg("About this pack")}</h2>{context.map((paragraph, index) => <p key={index}>{paragraph}</p>)}</section>}
    <section className={styles.group} aria-label={PACK_TERMS.outcomes.label}>
      <h2>{PACK_TERMS.outcomes.label}</h2>
      <ul className={styles.outcomes}>{outcomes.slice(0, 4).map((value, index) =>
        <li key={index}>{isRecord(value) ? text(value.label, text(value.id)) : msg("Unrecognized outcome")}</li>)}</ul>
      <div>
        {outcomes.length > 4 && <p className={styles.muted}><Message text={"<0/> more outcomes in Logic."} slots={[outcomes.length - 4]} /></p>}
        {outcomes.length === 0 && <p>{msg("No outcomes are declared.")}</p>}
      </div>
    </section>
    <section className={styles.group}>
      <div className={styles.sectionHeading}><h2>{msg("At a glance")}</h2>
        <>{onViewLogic ? <Button variant="quiet" onClick={onViewLogic}>{msg("View logic")}</Button> : packId ? <ButtonLink variant="quiet" to={logicHref ?? `/packs/${encodeURIComponent(packId)}?view=logic`}>{msg("View logic")}</ButtonLink> : null}</>
      </div>
      <dl className={styles.metadata}>
        <div><dt>{msg("Evidence needed")}</dt><dd><Message text={"<0/> required · <1/> optional"} slots={[evidence.filter(x => isRecord(x) && x.required === true).length, evidence.filter(x => isRecord(x) && x.required === false).length]} /></dd></div>
        <div><dt>{msg("Decision logic")}</dt><dd><Message text={"<0/> rules · <1/> special cases"} slots={[entries(doc.rules).length, entries(doc.exceptions).length]} /></dd></div>
      </dl>
    </section>
    <section>
      <InspectionRow label={PACK_TERMS.sources.label} value={entries(doc.sources).length} aria-label={msg("View source references")}
        current={at === '/sources'} onClick={() => onViewSources ? onViewSources() : select('/sources')} />
    </section>
  </section>
}

export function TestNavigation({ packId, saved = false, hasMatrix }: {
  packId: string; saved?: boolean; hasMatrix: boolean
}) {
  useLocale()
  const base = `/packs/${encodeURIComponent(packId)}`
  const link = usePackLink()
  return <nav className={styles.subnavigation} aria-label={msg("Test modes")}>
    <Link to={link(`${base}/evaluate`)} aria-current={!saved ? 'page' : undefined}>{msg("Try inputs")}</Link>
    {hasMatrix && <Link to={link(`${base}/matrix`)} aria-current={saved ? 'page' : undefined}>{msg("Saved cases")}</Link>}
  </nav>
}
