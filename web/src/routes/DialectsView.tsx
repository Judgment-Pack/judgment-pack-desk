import { DialectsContent } from '../dialects/DialectsView'

/** Generalized source preparations share the standard full-width page shell. */
export function DialectsView() {
  return <article className="detail" data-layout="page" data-measure="full">
    <DialectsContent />
  </article>
}
