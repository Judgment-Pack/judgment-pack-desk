import { createContext, useContext, type ReactNode } from 'react'
export type VerifiedSession = { subject: string; issuer: string | null; name?: string; email?: string; expiresAt?: string; localAccess?: boolean }
const Context = createContext<VerifiedSession | null>(null)
export const useVerifiedSession = () => useContext(Context)
export function VerifiedSessionProvider({ value, children }: { value: VerifiedSession; children: ReactNode }) {
  return <Context.Provider value={value}>{children}</Context.Provider>
}
