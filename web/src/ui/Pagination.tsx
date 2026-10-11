import { msg, useLocale } from '../i18n'
import { IconChevronLeft, IconChevronRight } from '../shell/icons'
import { Button } from './Button'
import { MenuChoice } from './Menu'
import { Tooltip } from './Tooltip'
import { PAGE_SIZES, type PageSize, type PaginationState } from './usePagination'
import styles from './Pagination.module.css'

/** Shared collection footer, independent of table markup and data fetching. */
export function Pagination({ pagination, complete = true, className }: {
  pagination: PaginationState
  complete?: boolean
  className?: string
}) {
  const locale = useLocale()
  const { total, size, page, pages, start, end, setPage, setSize } = pagination
  const number = (value: number) => value.toLocaleString(locale)
  const summary = !complete ? msg('{{count}} loaded items', { count: total })
    : pages > 1 ? msg('{{start}}–{{end}} of {{total}} items', { start: number(start + 1), end: number(end), total: number(total) })
    : msg('{{count}} items', { count: total })
  return <div className={[styles.footer, className].filter(Boolean).join(' ')} data-collection-footer>
    <span className={styles.total} role="status" aria-atomic="true">{summary}</span>
    {total > PAGE_SIZES[0] && <div className={styles.controls}>
      <MenuChoice label={msg('Items per page')} value={String(size)} onValueChange={value => setSize(Number(value) as PageSize)}
        triggerClassName={styles.size} options={PAGE_SIZES.map(value => ({ value: String(value), label: msg('{{size}} per page', { size: number(value) }) }))}/>
      {pages > 1 && <nav className={styles.pages} aria-label={msg('Pagination')}>
        <Tooltip content={msg('Previous page')}><Button variant="quiet" size="icon" aria-label={msg('Previous page')} disabled={page === 1} onClick={() => setPage(page - 1)}><IconChevronLeft/></Button></Tooltip>
        <span className={styles.position} role="status" aria-atomic="true">{msg('Page {{page}} of {{pages}}', { page: number(page), pages: number(pages) })}</span>
        <Tooltip content={msg('Next page')}><Button variant="quiet" size="icon" aria-label={msg('Next page')} disabled={page === pages} onClick={() => setPage(page + 1)}><IconChevronRight/></Button></Tooltip>
      </nav>}
    </div>}
  </div>
}
