import { useMatch, useParams } from 'react-router-dom'
import { DialectsContent } from '../dialects/DialectsView'
import { DialectDraftPage, NewDialectPage } from '../dialects/DialectDraftPage'

/** Drafts and saved dialects share one page frame and one content gutter. */
export function DialectsView() {
  const creating = useMatch('/dialects/new')
  const { draftId } = useParams()
  return <article className="detail" data-layout="page" data-measure="full">
    {creating ? <NewDialectPage /> : draftId ? <DialectDraftPage /> : <DialectsContent />}
  </article>
}
