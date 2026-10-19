import { PolicyPage } from '../policies/PolicyPage'

/** Policy drafts and saved policies share one full-width page frame. */
export function PolicyView() {
  return <article className="detail" data-layout="page" data-measure="full">
    <PolicyPage />
  </article>
}
