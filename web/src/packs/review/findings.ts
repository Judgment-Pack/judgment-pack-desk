/**
 * The runtime's `packs verify` findings, in plain words (ADR-0009, section 2).
 *
 * The words are keyed by the runtime's own finding name and say what it
 * found, nothing more: the desk forms no verdict of its own. A name this table
 * does not know is shown as the runtime wrote it, never dropped.
 */
import { msg } from '../../i18n'
import type { Review, ReviewFile, ReviewFinding } from './client'

export function findingWords(name: string): string {
  switch (name) {
    case 'config-drift': return msg('The project file changed; every decision waits for a lock')
    case 'document-drift': return msg('Changed since the last lock')
    case 'lock-entry-missing': return msg('New, never locked')
    case 'locked-but-undeclared': return msg('Removed from the project')
    case 'document-missing': return msg('File missing')
    case 'path-mismatch': return msg('Locked at another path')
    default: return name
  }
}

/** Each pack's findings, by its decision id. */
export function packFindings(review: Review | undefined): Map<string, ReviewFinding[]> {
  const found = new Map<string, ReviewFinding[]>()
  for (const finding of review?.findings ?? []) {
    if (finding.kind !== 'pack' || !finding.id) continue
    found.set(finding.id, [...(found.get(finding.id) ?? []), finding])
  }
  return found
}

/** The runtime's findings about one file of the review. */
export function fileFindings(review: Review, file: ReviewFile): ReviewFinding[] {
  return review.findings.filter(finding => file.kind === 'config'
    ? finding.name === 'config-drift'
    : finding.kind === file.kind && finding.id === file.id)
}

/** The findings no file of the review is about. */
export function otherFindings(review: Review): ReviewFinding[] {
  return review.findings.filter(finding => !review.files.some(file => fileFindings(review, file).includes(finding)))
}
