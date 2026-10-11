import type { ReactNode } from 'react'
import { msg } from '../i18n'
import styles from './BriefPane.module.css'

export function GraphReview({ draft = false, blocked, tests, actions }: {
  owner: string; title: string; content: string; draft?: boolean; blocked?: string; tests?: string; actions?: ReactNode
}) {
  return <div className={styles.review}>
    <section aria-label={msg('Readiness')}><h2>{msg('Readiness')}</h2>
      <dl className={styles.readiness}><dt>{msg('Definition')}</dt><dd>{blocked || (draft ? msg('Saved draft') : msg('Saved graph'))}</dd>
        <dt>{msg('Test results')}</dt><dd>{tests || msg('No test result in this view')}</dd></dl>
      <p className={styles.meta}>{msg('Review the composition and its referenced packs. Saved test observations do not authorize an operational decision.')}</p>
    </section>
    {actions && <section aria-label={msg('Review changes')}><h2>{msg('Review changes')}</h2><p className={styles.meta}>{msg('Review the exact file changes before saving.')}</p><div>{actions}</div></section>}
  </div>
}
