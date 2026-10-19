import { useLayoutEffect, useRef, type ReactNode } from 'react'
import styles from './CollectionLayout.module.css'
import {PageLoading} from './PageLoading'

/** Library-style collection: fixed controls and footer around one scrolling body. */
export function CollectionLayout({ label, toolbar, footer, children, scrollKey, loading }: {
  label: string
  toolbar?: ReactNode
  footer?: ReactNode
  children: ReactNode
  scrollKey?: string
  loading?: string
}) {
  const viewport = useRef<HTMLDivElement>(null)
  useLayoutEffect(() => { if (viewport.current) viewport.current.scrollTop = 0 }, [scrollKey])
  return <div className={styles.root} data-collection-layout>
    {toolbar && <div className={styles.toolbar}>{toolbar}</div>}
    <div ref={viewport} className={styles.scroll} data-collection-scroll data-loading={!!loading || undefined} role="region" aria-label={label} tabIndex={0}>{loading ? <PageLoading>{loading}</PageLoading> : children}</div>
    {footer && <div className={styles.footer} data-collection-bottom>{footer}</div>}
  </div>
}

/** The same empty-result hierarchy and gutter as Library. */
export function CollectionEmpty({title,children}:{title:string;children:ReactNode}){
 return <section className={styles.empty} role="status"><h2>{title}</h2>{children}</section>
}
