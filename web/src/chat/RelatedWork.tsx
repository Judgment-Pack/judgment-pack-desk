import type { ReactNode } from 'react'
import { msg } from '../i18n'
import {ChatActionLink} from './ChatActions'
import { OverflowTooltip } from '../ui/Tooltip'
import { useDetailsSlot } from '../shell/DetailsSlot'
import { useInspectorControls } from '../shell/InspectorSlot'
import { ReadingDetails } from './ReadingDetails'
import styles from './RelatedWork.module.css'

/** Shared presentation; feature adapters supply identity, state and guarded actions. */
export function RelatedWork({ children }: { children: ReactNode }) {
  return <ReadingDetails title={msg('Related work')}><div className={styles.list}>{children}</div></ReadingDetails>
}

export function RelatedWorkEntry({ title, icon, description, href, action, open, controls, children }: {
  title: string; icon: ReactNode; description: ReactNode; href?: string; action?: string; open?: boolean; controls?: ReactNode; children?: ReactNode
}) {
  const details = useDetailsSlot(), pane = useInspectorControls()
  const visible = open && !pane.mainCovered
  const navigate = () => { details.dismissInspection?.(); pane.revealMain?.() }
  return <section className={styles.entry} aria-label={title} data-related-work-entry>
    <div className={styles.heading}>
      <div className={styles.identity}>
        <span className={styles.icon} aria-hidden="true">{icon}</span>
        <div className={styles.text}>
          <OverflowTooltip><strong className={styles.title}>{title}</strong></OverflowTooltip>
          <div className={styles.caption}>{description}{visible && <> · <span>{msg('Open in workspace')}</span></>}</div>
        </div>
      </div>
      <div className={styles.actions}>{controls ?? (!visible && href && action ? <ChatActionLink to={href} onClick={navigate}>{action}</ChatActionLink> : null)}</div>
    </div>
    {children && <div className={styles.content}>{children}</div>}
  </section>
}

/** A flat secondary action, also used inside collapsed history. */
export function RelatedWorkAction({ label, detail, children }: { label: ReactNode; detail?: ReactNode; children?: ReactNode }) {
  return <div className={styles.actionRow}><div className={styles.actionText}><span>{label}</span>{detail && <small>{detail}</small>}</div>{children && <div className={styles.actions}>{children}</div>}</div>
}

export function RelatedWorkLink({ href, children }: { href: string; children: ReactNode }) {
  const details = useDetailsSlot(), pane = useInspectorControls()
  return <ChatActionLink to={href} onClick={() => { details.dismissInspection?.(); pane.revealMain?.() }}>{children}</ChatActionLink>
}
