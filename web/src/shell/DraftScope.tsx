import { msg } from '../i18n'
import type { ReactNode } from 'react'
import { useDirtyGuard } from './useDirtyGuard'
/** Kept as a composition boundary; all forms now share the shell's single guard. */
export function DraftScope({ children }: { children: ReactNode }) { return <>{children}</> }
export function useUnsavedChanges(dirty: boolean): void {
  useDirtyGuard(dirty, msg('Leave settings and discard your unsaved changes?'))
}
