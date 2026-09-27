import { useRegisteredChanges, type UnsavedOptions } from './UnsavedChanges'
/** Registers a buffer with the shell's single styled navigation guard. */
export function useDirtyGuard(dirty: boolean, question: string, options: UnsavedOptions = {}) {
  return useRegisteredChanges(dirty, question, options)
}
