/** One review query for the Packs workspace: the collection's badges and the review page read the same answer. */
import { createContext, useContext, type ReactNode } from 'react'
import { useQuery, type UseQueryResult } from '@tanstack/react-query'
import { readReview, REVIEW_KEY, type Review } from './client'

const ReviewContext = createContext<UseQueryResult<Review, Error> | null>(null)

export function ReviewProvider({ children }: { children: ReactNode }) {
  const query = useQuery({ queryKey: REVIEW_KEY, queryFn: ({ signal }) => readReview(signal), retry: false, staleTime: 0, refetchOnWindowFocus: true })
  return <ReviewContext.Provider value={query}>{children}</ReviewContext.Provider>
}

/** The review, where the Packs workspace provides one. */
export function useReview(): UseQueryResult<Review, Error> | null {
  return useContext(ReviewContext)
}
