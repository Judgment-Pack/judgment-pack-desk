import { decodeCheckpoint, restoreLedger, type SavedSource } from '../../chat/checkpoint'
import { readAttachments } from '../../chat/responseHistory'
import { INITIAL_STATE } from '../../research/run'
import { Ledger } from '../../research/ledger'
import type { PackDraft } from './model'

/** Authoring inputs travel beside the pack, without becoming declared policy references. */
export function sourceHandoff(draft: Pick<PackDraft, 'documents' | 'sourceFiles' | 'checkpoint'>) {
  return { version: 1, documents: draft.documents, files: draft.sourceFiles ?? [], retrievals: draft.checkpoint.sources }
}
export function readSourceHandoff(value: unknown) {
  const record = value as ReturnType<typeof sourceHandoff> | undefined
  if (!record || record.version !== 1 || !Array.isArray(record.files) || record.files.some(file => !file || typeof file.name !== 'string' || typeof file.text !== 'string')) {
    throw Error('Saved authoring sources could not be read. The saved files have not been changed.')
  }
  const documents = readAttachments(record.documents)
  const checkpoint = decodeCheckpoint({state: INITIAL_STATE, sources: record.retrievals})
  return {documents, files: record.files, retrievals: checkpoint.sources}
}
export function restoreSourceHandoff(retrievals: SavedSource[]) {
  const ledger = new Ledger('saved-pack')
  restoreLedger(ledger, retrievals)
  return ledger.sources
}
