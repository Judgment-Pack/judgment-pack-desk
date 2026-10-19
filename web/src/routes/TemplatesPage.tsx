import { TemplatesPage as TemplatesContent } from '../exchange/TemplatesPage'

/** Catalog review uses the same route measure as Dialect authoring. */
export function TemplatesPage() {
  return <article className="detail" data-layout="page" data-measure="full">
    <TemplatesContent />
  </article>
}
