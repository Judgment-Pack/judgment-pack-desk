/**
 * Which divergent digest pairs this connection has already asked about.
 *
 * A graph whose matrix run and served document report different digests is
 * shown withdrawn, and both answers are asked for again so the next pair can
 * re-bind (ADR-0030). The re-ask has to be asked *once*, and "once" is the
 * whole difficulty: the answers that come back may disagree again, and the page
 * must settle into the withdrawal rather than ask forever.
 *
 * Remembering only the pair asked about last is not enough, and the failure is
 * not hypothetical. A file being edited back and forth lands A/B, then C/D,
 * then A/B again — and a last-pair-only memory sees the third as new, asks
 * again, and runs the graph suite indefinitely over two revisions that are
 * merely alternating. So the memory is a *set*, and every distinct pair a
 * connection has asked about stays in it.
 *
 * Two more properties the set has and a component-local memory could not:
 *
 * - **It outlives the component.** Routing from `/graphs` to `/graphs/:id` and
 *   back unmounts and remounts the entry. A memory living in that component
 *   would come back empty and ask again about a pair it had already asked
 *   about, which is the alternating bug reached by a different road.
 * - **It is keyed by connection epoch, and only the current epoch is kept.** A
 *   reconnect gives every document query a new cache identity, so the answers
 *   after one are genuinely new answers that deserve to be asked about again —
 *   and dropping the older epochs is what stops the memory growing across a
 *   session of reconnects. Within one epoch the set grows by one entry per
 *   distinct disagreement, each of which cost a real refetch cycle to observe.
 *
 * This module holds no opinion about what a divergence *means*. It records that
 * a pair was asked about; the binding itself is `bindGraphDigests`.
 */
import { statedDigest } from './graphDocument'

/** Every pair asked about, by the connection epoch it was asked under. */
const askedByEpoch = new Map<number, Set<string>>()

/**
 * One divergent pair's identity, normalised exactly as the comparison is.
 *
 * The identity has to fold case and whitespace for the same reason
 * `bindGraphDigests` does: hex is case-insensitive, so one pair spelled two
 * ways is one pair. An identity built from raw strings would file those two
 * spellings separately and ask about the same disagreement twice — which is the
 * alternating bug again, this time between two spellings of one revision. The
 * normalisation is imported rather than repeated so the two cannot drift.
 *
 * Encoded with `JSON.stringify` rather than joined on a separator, because a
 * configured graph id is arbitrary text and any separator chosen could appear
 * inside one.
 */
export function divergentPairIdentity(input: {
  /** The configured graph id, since two graphs may disagree independently. */
  graphId: string
  /** The digest the matrix entry reported. */
  run: string | undefined
  /** The digest the runtime served beside the document. */
  served: string | undefined
}): string {
  return JSON.stringify([
    input.graphId,
    statedDigest(input.run) ?? null,
    statedDigest(input.served) ?? null
  ])
}

/**
 * Record one pair as asked about, and say whether this call is the one that
 * recorded it.
 *
 * True exactly once per pair per connection: the caller asks the runtime again
 * on a true and does nothing on a false. Recording and answering are one call
 * so that no caller can check and then forget to record.
 */
export function recordDivergentPair(connectionEpoch: number, identity: string): boolean {
  // A reconnect retires every pair asked under an older epoch. Their document
  // queries are keyed by that epoch and can never answer again, so keeping them
  // would only grow the map across a session of reconnects.
  for (const epoch of askedByEpoch.keys()) {
    if (epoch !== connectionEpoch) askedByEpoch.delete(epoch)
  }
  let asked = askedByEpoch.get(connectionEpoch)
  if (!asked) {
    asked = new Set<string>()
    askedByEpoch.set(connectionEpoch, asked)
  }
  if (asked.has(identity)) return false
  asked.add(identity)
  return true
}

/**
 * Forget every pair, on every epoch.
 *
 * The page never needs this — a reconnect prunes, and a page that forgot would
 * ask again about disagreements it had already asked about. It exists so that a
 * test starts from a connection that has asked about nothing, rather than from
 * whatever the test before it left behind.
 */
export function forgetDivergentPairs(): void {
  askedByEpoch.clear()
}
