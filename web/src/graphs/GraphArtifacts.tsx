import { msg } from '../i18n'
import { ArtifactReference } from '../chat/ArtifactReference'
import { ReadingDetails } from '../chat/ReadingDetails'
import { graphDraftHref, type GraphDraft } from './drafts'
import styles from '../chat/ChatWorkspace.module.css'

function GraphReference({ chatId, draft, open = false }: { chatId: string; draft: GraphDraft; open?: boolean }) {
  return <ArtifactReference title={draft.id} description={draft.saved ? msg('Saved') : msg('Graph draft')}
    indicator={open ? msg('Open') : undefined}
    action={draft.saved ? msg('Open graph') : msg('Review graph')}
    href={draft.saved ? `/graphs/${encodeURIComponent(draft.id)}` : graphDraftHref(chatId, draft.draftId)}/>
}

/** Same artifact and focused reader as packs; the current draft needs no navigation action. */
export function GraphArtifacts({ chatId, drafts, openDraftId }: { chatId: string; drafts: GraphDraft[]; openDraftId?: string }) {
  return <ReadingDetails title={msg('Graph drafts')}>
    <div className={styles.artifactList} aria-label={msg('Conversation graphs')}>
      {drafts.map(draft => <GraphReference key={draft.draftId} chatId={chatId} draft={draft} open={draft.draftId === openDraftId}/>)}
    </div>
  </ReadingDetails>
}

/** Show a new proposal after the conversation, never repeat the draft already in the workspace. */
export function GraphDraftNotice({ chatId, drafts, openDraftId }: { chatId: string; drafts: GraphDraft[]; openDraftId?: string }) {
  const draft = drafts.filter(item => !item.saved).at(-1)
  return draft && draft.draftId !== openDraftId ? <GraphReference chatId={chatId} draft={draft}/> : null
}
