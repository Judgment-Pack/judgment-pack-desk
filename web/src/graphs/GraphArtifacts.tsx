import { msg } from '../i18n'
import { ButtonLink } from '../ui/Button'
import { graphDraftHref, type GraphDraft } from './drafts'
import styles from './GraphWorkspace.module.css'

/** A saved artifact opens its declared graph, never a superseded write offer. */
export function GraphArtifacts({chatId, drafts}: {chatId: string; drafts: GraphDraft[]}) {
  return <section aria-label={msg('Conversation graphs')}>
    <h2>{msg('Graphs')}</h2>
    {drafts.map(draft => <div className={styles.draft} key={draft.draftId}>
      <span>{draft.id} · {draft.saved ? msg('Saved') : msg('Graph draft')}</span>
      <ButtonLink to={draft.saved ? `/graphs/${encodeURIComponent(draft.id)}` : graphDraftHref(chatId, draft.draftId)}>{draft.saved ? msg('Open graph') : msg('Review graph')}</ButtonLink>
    </div>)}
  </section>
}

/** Keep a single pending artifact visible; the toolbar opens the whole collection. */
export function GraphDraftNotice({chatId, drafts}: {chatId: string; drafts: GraphDraft[]}) {
  const draft = drafts.filter(item => !item.saved).at(-1)
  return draft ? <div className={styles.draft}>
    <span>{draft.id} · {msg('Graph draft')}</span>
    <ButtonLink to={graphDraftHref(chatId, draft.draftId)}>{msg('Review graph')}</ButtonLink>
  </div> : null
}
