import { DefinitionNavigation } from '../definitions/DefinitionNavigation'
import { DefinitionOverview, overviewText } from '../definitions/DefinitionOverview'
import { DefinitionJsonDialog } from '../definitions/DefinitionJsonDialog'
import overviewStyles from '../definitions/DefinitionOverview.module.css'
import { msg, useLocale } from '../i18n'
import { Link, useSearchParams } from 'react-router-dom'
import { useRef, useState, type ReactNode } from 'react'
import type { PackDocument } from '../mcp/types'
import { isRecord } from './document/MisshapenMember'
import type { RootMember } from './document/members'
import { Button, ButtonLink } from '../ui/Button'
import { PageHeader } from '../ui/PageLayout'
import { ExpandableText } from '../ui/ExpandableText'
import { PACK_TERMS } from './terminology'
import { Menu, OptionsMenu } from '../ui/Menu'
import { Dialog, DialogActions } from '../ui/Dialog'
import { MoveToFolderItem } from './folders/FolderBrowser'
import { IconHistory, IconDetails, IconMatrix, IconInfo } from '../shell/icons'
import { entries, text } from './logicModel'
import styles from './PackWorkspace.module.css'

export type PackSection = 'overview' | 'logic' | 'rules' | 'evidence' | 'document' | 'test' | 'review' | 'brief' | 'history' | 'usage'
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
  const active = current === 'rules' ? 'logic' : current
  const sections = [['overview', msg('Overview')], ['logic', msg('Logic')], ['evidence', msg('Sources')], ['test', msg('Tests')], ['review', msg('Review')], ['brief', msg('Brief')], ['history', msg('History')], ['usage', msg('Usage')]]
  if (current === 'document') sections.push(['document', msg('Full document')])
  return <DefinitionNavigation label={msg('Pack sections')} current={active} items={sections.map(([value,label]) => ({value:value!,label:label!,href:link(value==='test'?`${base}/evaluate`:`${base}?view=${value}`)}))}/>

}

/** Compound header: one title, one question, one primary task, one divider. */
export function PackHeader({ packId, document: doc, current, actions, hasMatrix = false, details, sourceText, checkStatus }: {
  packId: string; document?: PackDocument; current: PackSection; actions?: ReactNode; hasMatrix?: boolean; details?: ReactNode; sourceText?: string; checkStatus?: ReactNode
}) {
  useLocale()
  const [showJson,setShowJson] = useState(false)
  const [showDetails,setShowDetails] = useState(false), trigger=useRef<HTMLButtonElement>(null), handoff=useRef(false)
  const base = `/packs/${encodeURIComponent(packId)}`
  const link = usePackLink()
  return <><PageHeader variant="title" title={text(doc?.title, packId)} meta={text(doc?.version, '')}
    navigation={<PackNavigation packId={packId} current={current} />}
    actions={<div className={styles.actions}>{checkStatus && <Link className={styles.checkStatus} to={link(`${base}?view=review`)}>{checkStatus}</Link>}{actions}
      <OptionsMenu label={msg('Pack options')} triggerRef={trigger} onOpenChange={open=>{if(open)handoff.current=false}} onCloseAutoFocus={event=>{if(handoff.current)event.preventDefault()}}>
        <MoveToFolderItem id={packId} opener={trigger} onSelect={()=>{handoff.current=true}}/>
        <Menu.Separator/>
        <Menu.Item icon={<IconHistory/>} asChild><Link to={`/jobs/new?pack=${encodeURIComponent(packId)}`}>{msg('Create job…')}</Link></Menu.Item>
        {hasMatrix && <Menu.Item icon={<IconMatrix/>} asChild><Link to={link(`${base}/matrix`)}>{msg('Saved cases')}</Link></Menu.Item>}
        <Menu.Item icon={<IconDetails/>} disabled={sourceText === undefined} onSelect={()=>{handoff.current=true;setShowJson(true)}}>{msg('View JSON')}</Menu.Item>
        <Menu.Separator/>
        <Menu.Item icon={<IconInfo/>} onSelect={()=>{handoff.current=true;setShowDetails(true)}}>{msg('Pack details…')}</Menu.Item>
      </OptionsMenu>

    </div>} />
    <DefinitionJsonDialog open={showJson} onOpenChange={setShowJson} text={sourceText ?? ''} title={msg('Pack JSON')} openerRef={trigger}/>
    <Dialog open={showDetails} onOpenChange={setShowDetails} title={msg('Pack details')} openerRef={trigger}>
      <dl className={styles.metadata}>
        <div><dt>{msg('Title')}</dt><dd>{text(doc?.title,packId)}</dd></div>
        <div><dt>{msg('Version')}</dt><dd>{text(doc?.version)}</dd></div>
        <div><dt>{msg('Pack ID')}</dt><dd>{text(doc?.id,packId)}</dd></div>
      </dl>
      {details}
      <DialogActions><Button onClick={()=>setShowDetails(false)}>{msg('Close')}</Button></DialogActions>
    </Dialog>
  </>
}

export function PackQuestion({ document: doc }: { document?: PackDocument }) {
  useLocale()
  const decision = isRecord(doc?.decision) ? doc.decision : undefined
  const question = text(decision?.question, '')
  return question ? <ExpandableText text={question} label={msg("question")} /> : null
}

/** A short reading summary; detailed logic, sources and check findings have their own tabs. */
export function PackOverview({ document: doc, packId = '', logicHref, onViewLogic }: { document: PackDocument; packId?: string; logicHref?: string; onViewLogic?: () => void }) {
  useLocale()
  const decision = isRecord(doc.decision) ? doc.decision : undefined
  const [purpose, description, ...context] = overviewText(decision?.question, decision?.intent, doc.description)
  const outcomes = entries(doc.outcomes)
  const evidence = entries(doc.evidenceRequirements)
  const readableEvidence = doc.evidenceRequirements === undefined || (Array.isArray(doc.evidenceRequirements) && evidence.every(value => isRecord(value) && typeof value.required === 'boolean'))
  const evidenceSummary = !readableEvidence ? msg('Evidence requirements could not be read.') : evidence.length
    ? msg('{{required}} required · {{optional}} optional', { required: evidence.filter(value => isRecord(value) && value.required === true).length, optional: evidence.filter(value => isRecord(value) && value.required === false).length })
    : msg('No separate evidence requirements are declared.')
  const handoff = isRecord(doc.escalation?.target) ? text(doc.escalation.target.name, '') : ''
  const action = onViewLogic ? <Button variant="quiet" onClick={onViewLogic}>{msg('View logic')}</Button> : packId ? <ButtonLink variant="quiet" to={logicHref ?? `/packs/${encodeURIComponent(packId)}?view=logic`}>{msg('View logic')}</ButtonLink> : undefined
  // Keep the fallback visible once, even when it lies beyond the first few outcomes.
  const shown = outcomes.slice(0, 4)
  const fallback = outcomes.find(value => isRecord(value) && value.id === doc.fallbackOutcome)
  if (fallback && !shown.includes(fallback)) shown[shown.length - 1] = fallback
  return <DefinitionOverview label={msg('Pack overview')} purpose={purpose} description={description} context={context}
    resultLabel={PACK_TERMS.outcomes.label} action={action}
    result={<>
      <ul className={overviewStyles.outcomes}>{shown.map((value, index) => <li key={index}>
        {isRecord(value) ? text(value.label, text(value.id)) : msg('Unrecognized outcome')}
        {isRecord(value) && value.id === doc.fallbackOutcome && <span className={overviewStyles.fallback}>{msg('Fallback')}</span>}
      </li>)}</ul>
      {outcomes.length > shown.length && <p className={overviewStyles.muted}>{msg('{{count}} more outcomes in Logic.', { count: outcomes.length - shown.length })}</p>}
      {!outcomes.length && <p className={overviewStyles.muted}>{doc.outcomes !== undefined && !Array.isArray(doc.outcomes) ? msg('Outcomes could not be read.') : msg('No outcomes are declared.')}</p>}
    </>}
    details={[
      { label: msg('Scope'), value: doc.applicability ? msg('Restricted by a condition') : msg('No scope restriction') },
      { label: msg('Evidence needed'), value: evidenceSummary },
      ...(handoff ? [{ label: msg('Human handoff'), value: handoff }] : [])
    ]}/>
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
