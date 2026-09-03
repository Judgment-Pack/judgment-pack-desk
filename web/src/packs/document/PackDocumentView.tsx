/**
 * One pack, read.
 *
 * Two rules govern the whole file, and both are about honesty rather than
 * layout.
 *
 * **Present members render in the document's own key order.** A pack that puts
 * `rules` before `outcomes` is a pack whose author put them that way, and a
 * page that silently re-sorted would be showing a document nobody wrote.
 *
 * **An omitted optional member gets a line saying it is omitted**, spliced in
 * at the position the schema's order would give it. The view this replaces
 * used a `Section` that returns null with nothing inside it, so an absent
 * `applicability` and a view that had simply forgotten to draw one looked
 * exactly alike.
 *
 * Every block goes through `Block`, so every block carries its pointer as
 * `data-pointer` and as its element id. That is the address a diagnostic
 * anchors on, a deep link reaches, the Inspector selects, and — next phase — a
 * form field writes through.
 */
import type { ReactNode } from 'react'
import type { PackDocument } from '../../mcp/types'
import { ApplicabilityBlock } from './ApplicabilityBlock'
import { Block } from './Block'
import { DecisionBlock } from './DecisionBlock'
import { EscalationBlock } from './EscalationBlock'
import { EvidenceBlock } from './EvidenceBlock'
import { ExceptionsBlock } from './ExceptionsBlock'
import { ExtensionsBlock } from './ExtensionsBlock'
import { IdentityBlock } from './IdentityBlock'
import { MemberOutline, type OutlineEntry } from './MemberOutline'
import { MetadataBlock } from './MetadataBlock'
import { OutcomesBlock } from './OutcomesBlock'
import { OmittedMember } from './OmittedMember'
import { RulesBlock } from './RulesBlock'
import { SourcesBlock } from './SourcesBlock'
import { readingOrder, unitCount, unitIsPresent, type MemberUnit } from './members'
import styles from './PackDocument.module.css'

/** What an absent member means, in the document's own vocabulary. */
const ABSENCE_NOTE: Record<string, string> = {
  applicability: 'the pack does not narrow its own scope.',
  evidenceRequirements: 'the pack requires no evidence.',
  sources: 'the pack cites nothing.',
  exceptions: 'no rule is excepted.',
  fallbackOutcome: 'no outcome is named as the fallback.',
  escalation: 'the pack names nowhere to escalate.',
  metadata: 'the document records no metadata.',
  extensions: 'the document carries no extensions.'
}

export function PackDocumentView({
  document: doc,
  active,
  children
}: {
  document: PackDocument
  /** The member the outline marks as the one being read. */
  active: string | null
  /** The check strip, rendered between the outline and the first member. */
  children?: ReactNode
}) {
  const order = readingOrder(doc)
  const entries: OutlineEntry[] = order.map((unit) => ({
    id: unit.id,
    label: unit.label,
    pointer: unit.pointer,
    present: unitIsPresent(doc, unit),
    count: unitCount(doc, unit)
  }))

  return (
    <article className={styles.document} data-pointer="">
      <MemberOutline entries={entries} active={active} />
      {children}
      {order.map((unit) => (
        <MemberBlock key={unit.id} unit={unit} document={doc} />
      ))}
    </article>
  )
}

function MemberBlock({ unit, document: doc }: { unit: MemberUnit; document: PackDocument }) {
  if (!unitIsPresent(doc, unit)) {
    return (
      <OmittedMember pointer={unit.pointer} label={unit.label} note={ABSENCE_NOTE[unit.id]} />
    )
  }
  switch (unit.id) {
    case 'identity':
      return <IdentityBlock document={doc} />
    case 'decision':
      return <DecisionBlock decision={doc.decision} at="/decision" />
    case 'applicability':
      return <ApplicabilityBlock applicability={doc.applicability!} at="/applicability" />
    case 'evidenceRequirements':
      return <EvidenceBlock requirements={doc.evidenceRequirements!} at="/evidenceRequirements" />
    case 'sources':
      return <SourcesBlock sources={doc.sources!} at="/sources" />
    case 'outcomes':
      return (
        <OutcomesBlock outcomes={doc.outcomes} fallback={doc.fallbackOutcome} at="/outcomes" />
      )
    case 'rules':
      return <RulesBlock rules={doc.rules} at="/rules" />
    case 'exceptions':
      return <ExceptionsBlock exceptions={doc.exceptions!} at="/exceptions" />
    case 'fallbackOutcome':
      return (
        <Block pointer="/fallbackOutcome">
          <h2 className={styles.heading}>Fallback outcome</h2>
          <p>
            <code className={styles.id}>{doc.fallbackOutcome}</code>
          </p>
        </Block>
      )
    case 'escalation':
      return <EscalationBlock escalation={doc.escalation!} at="/escalation" />
    case 'metadata':
      return <MetadataBlock metadata={doc.metadata!} at="/metadata" />
    case 'extensions':
      return <ExtensionsBlock extensions={doc.extensions} at="/extensions" heading="Extensions" />
    default:
      return null
  }
}
