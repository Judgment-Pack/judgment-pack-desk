/**
 * Whether the pack bytes a release is made from are in the project's reviewed
 * set (ADR-0009, question 4). Jobs shows it in "Review this release", and it
 * refuses nothing: a job is created the same way whatever it says.
 *
 * **What "in the reviewed set" means here, and nothing more:** the project's
 * lock pins these exact bytes for the decision id the release was checked
 * for. Desk establishes it by comparing the SHA-256 of the exact bytes the
 * release is made from (the release's own `pack`, which is what `get_pack`
 * served when the release was checked) with the lock's own entry for that id.
 * The entry comes from the review step's one reading (`GET /api/review`),
 * which also carries the runtime's `packs verify` findings over that same
 * reading, and those are shown beside it in the review step's words.
 *
 * The two answer different questions, so neither stands in for the other. The
 * findings are about the project's files as the reading found them; the
 * comparison is about the release's bytes, which were read earlier. A pack
 * edited after the release was checked changes the findings, not the release.
 */
import { digestOf } from '../research/checkCandidate'
import { readReview, type Review, type ReviewFinding } from '../packs/review/client'
import { packFindings } from '../packs/review/findings'

/** How the lock's entry for the release's decision id stands to the release's bytes. */
export type LockedBytes = 'same' | 'other' | 'none'

export type ReleaseStanding =
  /** The lock pins these exact bytes for the id, and the configuration is the one locked. */
  | { state: 'reviewed'; bytes: 'same'; findings: ReviewFinding[] }
  /** The lock pins other bytes for the id, or none. */
  | { state: 'draft'; bytes: 'other' | 'none'; findings: ReviewFinding[] }
  /** The project keeps no lock. */
  | { state: 'no-lock' }
  /** The project's configuration changed after its last lock: the runtime holds every decision by id. */
  | { state: 'config-drift'; bytes: LockedBytes; findings: ReviewFinding[] }
  /** The review could not be read, or could not say. `reason` is in its own words. */
  | { state: 'unreadable'; reason: string }

/** The SHA-256 of a text's UTF-8 bytes, as the runtime and the lock write a digest. */
export async function bytesDigest(text: string): Promise<string> {
  return 'sha256:' + await digestOf(text)
}

/**
 * The standing of a release's bytes, `digest`, made from the pack the project
 * declares as `packId`, against one review. The id is the project's decision
 * id, which keys the lock; a release's own `packId` is the pack document's.
 */
export function standingOf(review: Review, packId: string, digest: string): ReleaseStanding {
  if (review.blocked) return { state: 'unreadable', reason: review.blocked }
  if (!review.locked) return { state: 'no-lock' }
  // A lock the runtime could not read is not a reviewed set Desk can compare with.
  if (review.status === 'error') return { state: 'unreadable', reason: review.diagnostics.map(item => item.message).join(' ') }
  const findings = [...review.findings.filter(finding => finding.name === 'config-drift'), ...(packFindings(review).get(packId) ?? [])]
  const locked = review.files.find(file => file.kind === 'pack' && file.id === packId)?.locked
  const bytes: LockedBytes = !locked ? 'none' : locked === digest ? 'same' : 'other'
  if (findings.some(finding => finding.name === 'config-drift')) return { state: 'config-drift', bytes, findings }
  return bytes === 'same' ? { state: 'reviewed', bytes, findings } : { state: 'draft', bytes, findings }
}

/** Read the project's review, and compare the release's exact bytes with it. */
export async function readReleaseStanding(pack: string, packId: string, signal?: AbortSignal): Promise<ReleaseStanding> {
  const [review, digest] = await Promise.all([readReview(signal), bytesDigest(pack)])
  return standingOf(review, packId, digest)
}
