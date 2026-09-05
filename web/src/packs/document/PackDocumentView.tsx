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
 *
 * **The document is one tab stop.** Ninety-odd blocks cannot each be one, so a
 * roving tab index puts `tabIndex={0}` on exactly one of them and the arrow
 * keys move it: Down and Up step through the blocks in document order, Home and
 * End reach the ends, Enter and Space select. Without it the only keyboard
 * route into `?at` was the outline, which addresses the twelve member units and
 * nothing under them — no rule card, no condition operand, no review. The
 * handler is here rather than on each block because "the next block" is a fact
 * about the document, and it reads the rendered order off the DOM for the same
 * reason the diagnostic anchoring does: a derivation would be a second model of
 * this file, free to drift from it.
 */
import { useRef, useState, type KeyboardEvent, type ReactNode } from 'react'
import type { PackDocument } from '../../mcp/types'
import { ApplicabilityBlock } from './ApplicabilityBlock'
import { Block, CursorContext, useDocumentSelection } from './Block'
import { DecisionBlock } from './DecisionBlock'
import { EscalationBlock } from './EscalationBlock'
import { EvidenceBlock } from './EvidenceBlock'
import { ExceptionsBlock } from './ExceptionsBlock'
import { ExtensionsBlock } from './ExtensionsBlock'
import {
  DescriptionBlock,
  IdBlock,
  SpecVersionBlock,
  TitleBlock,
  VersionBlock
} from './IdentityBlock'
import { MemberOutline, type OutlineEntry } from './MemberOutline'
import { MisshapenMember } from './MisshapenMember'
import { IdRefField } from '../edit/fields'
import { useEditing } from '../edit/editingContext'
import { MetadataBlock } from './MetadataBlock'
import { OutcomesBlock } from './OutcomesBlock'
import { OmittedMember } from './OmittedMember'
import { RulesBlock } from './RulesBlock'
import { SourcesBlock } from './SourcesBlock'
import { outlineUnits, readingOrder, unitCount, unitIsPresent, type MemberUnit } from './members'
import styles from './PackDocument.module.css'

/** What an absent member means, in the document's own vocabulary. */
const ABSENCE_NOTE: Record<string, string> = {
  // `description` is optional in the schema, and its absence had no line at
  // all while the five identity members were drawn as one unit: the unit was
  // present because `title` was, so nothing ever said the description was not
  // there. An optional member's absence is a fact about the document.
  description: 'the pack carries no description.',
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
  // The nav is not the page. Reading order is one unit per member, in the
  // document's own order; the outline collapses the identity members into one
  // entry, at the position of the first of them.
  const entries: OutlineEntry[] = outlineUnits(doc, order).map((unit) => ({
    id: unit.id,
    label: unit.label,
    pointer: unit.pointer,
    present: unit.members.some(
      (member) => (doc as unknown as Record<string, unknown>)[member] !== undefined
    ),
    count: unitCount(doc, unit)
  }))

  const { at, select } = useDocumentSelection()
  const article = useRef<HTMLElement | null>(null)
  const [moved, setMoved] = useState<string | null>(null)
  // Before anything has moved the stop, it follows the selection — so a deep
  // link's block is where Tab comes back to — and before there is a selection
  // it is the document's first member.
  const cursor = moved ?? at ?? order[0]?.pointer ?? null

  return (
    <CursorContext.Provider value={{ at: cursor, move: setMoved }}>
      <article
        ref={article}
        className={styles.document}
        data-pointer=""
        onKeyDown={(event) => onDocumentKey(event, article.current, select, setMoved)}
      >
        <MemberOutline entries={entries} active={active} />
        {children}
        {order.map((unit) => (
          <MemberBlock key={unit.id} unit={unit} document={doc} />
        ))}
      </article>
    </CursorContext.Provider>
  )
}

/**
 * The document's keyboard: move the stop, or select where it is.
 *
 * Anything inside a control of its own — the outline's links, and whatever the
 * check strip carries — is left alone: Enter on a link is the link's, and
 * arrowing out of one would be a surprise.
 */
function onDocumentKey(
  event: KeyboardEvent<HTMLElement>,
  article: HTMLElement | null,
  select: (pointer: string) => void,
  move: (pointer: string) => void
): void {
  if (article === null) return
  const target = event.target as HTMLElement
  if (typeof target.closest !== 'function') return
  if (target.closest('a, button, input, select, textarea, [role="tab"]') !== null) return
  const here = target.getAttribute('data-pointer')
  if (here === null || here === '') return

  if (event.key === 'Enter' || event.key === ' ') {
    event.preventDefault()
    select(here)
    move(here)
    return
  }

  const blocks = [...article.querySelectorAll<HTMLElement>('[data-pointer]')].filter(
    (element) => element.getAttribute('data-pointer') !== ''
  )
  const current = blocks.indexOf(target)
  if (current < 0) return
  const last = blocks.length - 1
  let next: number | undefined
  if (event.key === 'ArrowDown') next = Math.min(last, current + 1)
  else if (event.key === 'ArrowUp') next = Math.max(0, current - 1)
  else if (event.key === 'Home') next = 0
  else if (event.key === 'End') next = last
  if (next === undefined) return
  event.preventDefault()
  const destination = blocks[next]
  if (destination === undefined) return
  move(destination.getAttribute('data-pointer')!)
  destination.focus()
}

/**
 * What shape each root member has to be for the controls that draw it.
 *
 * **Not the schema, and not a verdict.** `validate` is what says whether a
 * document is a pack. This is the narrower question the renderer has to answer
 * before it draws anything: `"rules": {}` is valid JSON that somebody can have
 * on disk — this desk can write it — and `rules.map` took the whole route down
 * with it. A member that is not the shape below gets a block saying so, at its
 * own pointer, with its bytes in it.
 */
const MEMBER_SHAPE: Record<string, 'list' | 'object' | 'string'> = {
  specVersion: 'string',
  id: 'string',
  version: 'string',
  title: 'string',
  description: 'string',
  decision: 'object',
  applicability: 'object',
  evidenceRequirements: 'list',
  sources: 'list',
  outcomes: 'list',
  rules: 'list',
  exceptions: 'list',
  fallbackOutcome: 'string',
  escalation: 'object',
  metadata: 'object',
  extensions: 'object'
}

const WORD: Record<'list' | 'object' | 'string', string> = {
  list: 'a list',
  object: 'an object',
  string: 'a string'
}

function isShape(value: unknown, shape: 'list' | 'object' | 'string'): boolean {
  if (shape === 'list') return Array.isArray(value)
  if (shape === 'string') return typeof value === 'string'
  return typeof value === 'object' && value !== null && !Array.isArray(value)
}

/**
 * The outcome a pack falls back to, chosen from the outcomes it declares.
 *
 * Read-only it is a `code` element; in a form it is the same optional id
 * reference the rules use, so "not declared" is a choice and a dangling id is
 * shown as one rather than silently dropped.
 */
function FallbackOutcomeBlock({ fallback }: { fallback: string | undefined }) {
  const { editing, ids } = useEditing()
  return (
    <Block pointer="/fallbackOutcome">
      <h2 className={styles.heading}>Fallback outcome</h2>
      {editing ? (
        <IdRefField pointer="/fallbackOutcome" label="fallback outcome" ids={ids.outcomes} optional />
      ) : (
        <p>
          <code className={styles.id}>{fallback}</code>
        </p>
      )}
    </Block>
  )
}

function MemberBlock({ unit, document: doc }: { unit: MemberUnit; document: PackDocument }) {
  if (!unitIsPresent(doc, unit)) {
    // Only an optional member reaches this: `readingOrder` leaves a missing
    // required one out of the list entirely, because its absence is a refusal
    // the runtime issues at that pointer rather than an omission this page
    // states. Asserted there, and this is the shape that follows from it.
    return (
      <OmittedMember
        pointer={unit.pointer}
        label={unit.blockLabel ?? unit.label}
        note={ABSENCE_NOTE[unit.id]}
      />
    )
  }
  const shape = MEMBER_SHAPE[unit.id]
  const held = (doc as unknown as Record<string, unknown>)[unit.members[0]!]
  if (shape !== undefined && !isShape(held, shape)) {
    return (
      <MisshapenMember
        pointer={unit.pointer}
        label={unit.blockLabel ?? unit.label}
        expected={WORD[shape]}
        value={held}
      />
    )
  }
  switch (unit.id) {
    case 'title':
      return <TitleBlock document={doc} />
    case 'version':
      return <VersionBlock document={doc} />
    case 'specVersion':
      return <SpecVersionBlock document={doc} />
    case 'id':
      return <IdBlock document={doc} />
    case 'description':
      return <DescriptionBlock document={doc} />
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
      return <FallbackOutcomeBlock fallback={doc.fallbackOutcome} />
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
