import { useChats } from '../../chat/ChatProvider'
import { msg } from '../../i18n'
import { IconMatrix } from '../../shell/icons'
import { Disclosure } from '../../ui/Disclosure'
import { RelatedWorkAction, RelatedWorkEntry, RelatedWorkLink } from '../../chat/RelatedWork'
import { useTestStorage } from './store'
import { proposalCases } from './proposals'
import { testReviewHref } from './chatTools'
import styles from '../../chat/RelatedWork.module.css'

export function useChatTestProposals(chatId: string) {
  const { query } = useTestStorage('')
  const { packDrafts } = useChats()
  const entries = Object.entries(query.data?.content.suites ?? {}).flatMap(([owner, suite]) => (suite.proposals ?? [])
    .filter(p => p.chatId === chatId && p.attempts.length && p.state === 'ready')
    .map(proposal => ({ owner, proposal, draft: packDrafts.some(draft => draft.id === owner) })))
  // Finalization carries the same proposals into the saved pack's suite.
  const unique = new Map<string, typeof entries[number]>()
  for (const entry of entries) if (!unique.has(entry.proposal.id) || !entry.draft) unique.set(entry.proposal.id, entry)
  return [...unique.values()].sort((a,b) => a.proposal.at.localeCompare(b.proposal.at))
}
export function ChatTestProposals({ chatId, history = false }: { chatId: string; history?: boolean }) {
  const proposals = useChatTestProposals(chatId)
  const latest = proposals.at(-1)
  if (!latest) return null
  const count = (entry: typeof latest) => proposalCases(entry.proposal).length
  const label = (entry: typeof latest) => count(entry) === 1 ? msg('1 case') : msg('{{count}} cases', {count: count(entry)})
  const saved = (entry: typeof latest) => Object.keys(entry.proposal.savedCases ?? {}).length
  const href = (entry: typeof latest) => testReviewHref({ owner: entry.owner, draft: entry.draft }, chatId, entry.proposal.id)
  if (!history && saved(latest) >= count(latest)) return null
  return <RelatedWorkEntry title={msg('Proposed test cases')} icon={<IconMatrix/>}
    description={label(latest) + ' · ' + (saved(latest) ? msg('{{count}} saved', { count: saved(latest) }) : msg('Not saved'))}
    controls={<RelatedWorkLink href={href(latest)}>{msg('Review cases')}</RelatedWorkLink>}>
    {history && proposals.length > 1 && <Disclosure title={msg('History')}><div className={styles.history}>{proposals.slice(0,-1).reverse().map(entry => <RelatedWorkAction key={entry.proposal.id} label={entry.proposal.at} detail={label(entry)}>
      <RelatedWorkLink href={href(entry)}>{msg('Review cases')}</RelatedWorkLink>
    </RelatedWorkAction>)}</div></Disclosure>}
  </RelatedWorkEntry>
}
