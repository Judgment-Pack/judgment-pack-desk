import { useEffect, useState } from 'react'

export const PAGE_SIZES = [25, 50, 100] as const
export type PageSize = typeof PAGE_SIZES[number]

/** Paging owns positions only; callers keep filtering, sorting and selection. */
export function usePagination(total: number, scope: string) {
  const [size, setSize] = useState<PageSize>(50)
  const [position, setPosition] = useState({ scope, page: 1 })
  const pages = Math.max(1, Math.ceil(total / size))
  // A changed query cannot briefly render the old page, even before effects run.
  const page = position.scope === scope ? Math.min(position.page, pages) : 1
  useEffect(() => {
    if (position.scope !== scope || position.page !== page) setPosition({ scope, page })
  }, [position, scope, page])
  return {
    total, size, page, pages, start: (page - 1) * size, end: Math.min(page * size, total),
    setPage: (next: number) => setPosition({ scope, page: Math.max(1, Math.min(next, pages)) }),
    setSize: (next: PageSize) => { setSize(next); setPosition({ scope, page: 1 }) }
  }
}
export type PaginationState = ReturnType<typeof usePagination>
