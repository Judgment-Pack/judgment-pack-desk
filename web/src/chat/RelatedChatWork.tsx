import {PolicyChatProposals} from '../policies/PolicyChatProposals'
import {dialectQuery} from '../dialects/queries'
import {PlanCard} from '../assistant/planning/PlanCard'
import { ChatTestProposals, useChatTestProposals } from '../packs/test-workspace/ChatTestProposals'
import { useLocation } from 'react-router-dom'
import { useQuery } from '@tanstack/react-query'
import { msg } from '../i18n'
import { IconGraph, IconMatrix, IconPack } from '../shell/icons'
import { Disclosure } from '../ui/Disclosure'
import { ChatAction } from './ChatActions'
import { CodeBlock } from '../ui/CodeBlock'
import { useDialectRecipeEditing } from '../dialects/recipeEditing'
import { validDialectId, type DialectChatAction } from '../dialects/chatActions'
import type { DialectRecipeProposal } from '../dialects/proposals'
import { RecipeReference } from '../dialects/DialectRecipeArtifacts'
import { DialectActionReference, dialectActionTitle, dialectActionOutcome } from '../dialects/DialectActionArtifacts'
import { graphDraftHref, type GraphDraft } from '../graphs/drafts'
import { draftHref } from '../packs/drafts/model'
import { useChats } from './ChatProvider'
import type { Chat } from './store'
import { dialectChatHref } from './navigation'
import { RelatedWork, RelatedWorkEntry, RelatedWorkAction, RelatedWorkLink } from './RelatedWork'
import styles from './RelatedWork.module.css'

export interface WorkContext { kind: 'pack' | 'graph' | 'input' | 'policy'; id: string; title: string; detail?: string; text?: string; tests?: string; open?: boolean }
interface DialectGroup { key: string; id?: string; name: string; drafts: DialectRecipeProposal[]; actions: DialectChatAction[]; historical?: boolean }

/** Group by persisted identity, never the display name or number of operations. */
export function relatedDialects(chat: Pick<Chat, 'input' | 'inputDrafts' | 'inputActions'>, current?: WorkContext): DialectGroup[] {
  const groups = new Map<string, DialectGroup>()
  const add = (key: string, id: string | undefined, name: string) => {
    const group = groups.get(key) ?? { key, id, name, drafts: [], actions: [] }
    if (name) group.name = name
    groups.set(key, group)
    return group
  }
  if (current?.kind === 'input' && validDialectId(current.id)) add(current.id, current.id, current.title)
  if (validDialectId(chat.input?.id)) add(chat.input!.id, chat.input!.id, '')
  for (const draft of chat.inputDrafts ?? []) {
    const group = add(draft.savedInputId ?? 'draft:' + draft.draftId, draft.savedInputId, draft.name)
    group.drafts.push(draft)
    // An earlier unbound creation proposal is not a newly required review.
    // Keep it in history without guessing an association from its display name.
    group.historical = !draft.savedInputId && draft !== chat.inputDrafts?.at(-1)
  }
  for (const action of chat.inputActions ?? []) add(action.inputId, action.inputId, action.name).actions.push(action)
  return [...groups.values()]
}
function ContextDetails({ context }: { context?: WorkContext }) {
  if (!context?.text) return null
  return <Disclosure title={msg('Included with your message')}>
    <CodeBlock text={context.text} label={msg('Included with your message')}/>
    {context.tests && <CodeBlock text={context.tests} label={msg('Saved tests and observed runs')}/>}
  </Disclosure>
}
function DialectWork({ chatId, group, context, disabled }: { chatId: string; group: DialectGroup; context?: WorkContext; disabled?: boolean }) {
  const location = useLocation()
  const editing = useDialectRecipeEditing(group.id ?? '')
  const editHint = msg('Save or discard Recipe edits before reviewing this action.')
  const saved = useQuery({ ...dialectQuery(group.id??''), enabled: !!group.id, retry: false })
  const doc = saved.data?.document, latestDraft = group.drafts.at(-1), latestAction = group.actions.at(-1)
  const draft = latestDraft && !latestDraft.savedInputId ? latestDraft : undefined
  const action = latestAction && !latestAction.applied ? latestAction : undefined
  const history = group.drafts.filter(d => d !== draft).length + group.actions.filter(a => a !== action).length
  const own = context?.kind === 'input' && context.id === group.id ? context : undefined
  const target = draft?.target === 'records' ? msg('Records') : draft?.target === 'companies' ? msg('Companies') : draft?.target === 'people' ? msg('People') : msg('People at selected companies')
  return <RelatedWorkEntry title={doc?.name ?? (group.name || msg('Dialect'))} icon={<IconMatrix/>}
    description={draft ? <>{group.historical ? msg('Earlier proposal · Not saved') : msg('New dialect')} · {target}</> : <>{msg('Dialect')}{doc && <> · {msg('Recipe version {{version}}', { version: doc.recipeVersion })}</>}</>}
    href={group.id ? dialectChatHref(group.id, chatId, location.pathname === '/dialects/' + group.id ? location : { pathname: '/dialects/' + group.id, search: '?tab=recipe' }) : undefined} action={msg('Open dialect')} open={own?.open || !!group.id && location.pathname === '/dialects/' + group.id}
    controls={draft ? <RecipeReference chatId={chatId} draft={draft} disabled={disabled} presentation="action"/> : undefined}>
    {saved.isError && <RelatedWorkAction label={msg('This input could not be loaded.')}><ChatAction variant="quiet" onClick={() => void saved.refetch()}>{msg('Retry')}</ChatAction></RelatedWorkAction>}
    {action && <RelatedWorkAction label={dialectActionTitle(action)} detail={editing ? editHint : msg('Needs review')}><DialectActionReference chatId={chatId} action={action} disabled={disabled} presentation="action"/></RelatedWorkAction>}
    {!!history && <Disclosure title={msg('History')}><div className={styles.history}>
      {group.drafts.filter(d => d !== draft).map(d => <RelatedWorkAction key={d.draftId} label={d.savedInputId ? msg('Recipe saved') : msg('Earlier proposal · Not saved')}>{!d.savedInputId && <RecipeReference chatId={chatId} draft={d} disabled={disabled} presentation="action"/>}</RelatedWorkAction>)}
      {group.actions.filter(a => a !== action).map(a => <RelatedWorkAction key={a.id} label={dialectActionTitle(a)} detail={a.applied ? dialectActionOutcome(a) : editing ? editHint : msg('Not applied')}>{!a.applied && <DialectActionReference chatId={chatId} action={a} disabled={disabled} presentation="action"/>}</RelatedWorkAction>)}
    </div></Disclosure>}
    <ContextDetails context={own}/>
  </RelatedWorkEntry>
}

interface GraphGroup { key: string; id: string; saved: boolean; drafts: GraphDraft[] }
export function relatedGraphs(chat: Pick<Chat, 'graph' | 'graphDrafts'>): GraphGroup[] {
  const groups = new Map<string, GraphGroup>()
  const add = (id: string, path: string, saved: boolean) => {
    const key = path + '\n' + id
    const group = groups.get(key) ?? { key, id, saved, drafts: [] }
    group.saved ||= saved; groups.set(key, group); return group
  }
  if (chat.graph) add(chat.graph.id, chat.graph.path, chat.graph.workspace === chat.graph.id)
  for (const draft of chat.graphDrafts ?? []) add(draft.id, draft.path, !!draft.saved || !!draft.baseSha256).drafts.push(draft)
  return [...groups.values()]
}
function GraphWork({ chatId, group, current, openGraphDraftId }: { chatId: string; group: GraphGroup; current?: WorkContext; openGraphDraftId?: string }) {
  const latest = group.drafts.at(-1), pending = latest && !latest.saved ? latest : undefined
  const own = current?.kind === 'graph' && current.id === group.id ? current : undefined
  const draftOpen = !!pending && pending.draftId === openGraphDraftId
  const history = group.drafts.filter(d => d !== pending)
  return <RelatedWorkEntry title={own?.title || group.id} icon={<IconGraph/>} description={group.saved ? msg('Graph') : msg('Graph draft')}
    href={group.saved ? '/graphs/' + encodeURIComponent(group.id) + '?chat=' + encodeURIComponent(chatId) : pending ? graphDraftHref(chatId, pending.draftId) : undefined}
    action={group.saved ? msg('Open graph') : msg('Review graph')} open={group.saved ? own?.open : draftOpen || own?.open}>
    {pending && group.saved && <RelatedWorkAction label={msg('Proposed graph update')} detail={draftOpen ? msg('Open in workspace') : msg('Needs review')}>
      {!draftOpen && <RelatedWorkLink href={graphDraftHref(chatId, pending.draftId)}>{msg('Review graph')}</RelatedWorkLink>}
    </RelatedWorkAction>}
    {!!history.length && <Disclosure title={msg('History')}><div className={styles.history}>{history.map(d => <RelatedWorkAction key={d.draftId} label={d.saved ? msg('Graph saved') : msg('Earlier proposal · Not saved')}>
      {!d.saved && <RelatedWorkLink href={graphDraftHref(chatId, d.draftId)}>{msg('Review graph')}</RelatedWorkLink>}
    </RelatedWorkAction>)}</div></Disclosure>}
    <ContextDetails context={own}/>
  </RelatedWorkEntry>
}

/** Reads live state so reviewing a proposal updates its existing entry in place. */
export function RelatedChatWork({ chatId, current, openGraphDraftId, disabled }: { chatId: string; current?: WorkContext; openGraphDraftId?: string; disabled?: boolean }) {
  const { chats, drafts, packDrafts } = useChats()
  const testProposals = useChatTestProposals(chatId)
  const location = useLocation()
  const chat = [...chats, ...drafts].find(item => item.id === chatId)
  if (!chat) return <RelatedWork><p>{msg('This chat is no longer available.')}</p></RelatedWork>
  const inputs = relatedDialects(chat, current)
  const packDraft = packDrafts.find(draft => draft.id === chat.draftId)
  const pack = chat.pack ?? packDraft?.finalized
  const graphs = relatedGraphs(chat)
  const historicalInputs = inputs.filter(group => group.historical)
  const ownPack = current?.kind === 'pack' && (current.id === pack?.id || current.id === packDraft?.id) ? current : undefined
  const hasWork = chat.plans?.length || testProposals.length || inputs.length || chat.policy || pack || packDraft || graphs.length
  return <RelatedWork>
    <PlanCard chat={chat} history/>
    {!hasWork && <p className={styles.empty}>{msg('Work you create or review in this chat will appear here.')}</p>}
    {inputs.filter(group => !group.historical).map(group => <DialectWork key={group.key} chatId={chatId} group={group} context={current} disabled={disabled}/>)}
    {(pack || packDraft) && <RelatedWorkEntry title={ownPack?.title || pack?.id || packDraft!.title} icon={<IconPack/>}
      description={pack ? msg('Pack') : msg('Pack draft')} action={pack ? msg('Open pack') : msg('Review draft')}
      href={pack ? '/packs/' + encodeURIComponent(pack.id) + '?chat=' + encodeURIComponent(chatId) : draftHref(packDraft!.id)} open={!!ownPack?.open && ownPack.id === (pack?.id ?? packDraft?.id) || location.pathname === (pack ? '/packs/' + encodeURIComponent(pack.id) : draftHref(packDraft!.id))}>
      <ContextDetails context={ownPack}/>
    </RelatedWorkEntry>}
    {chat.policy&&<RelatedWorkEntry title={current?.kind==='policy'?current.title:msg('Policy')} icon={<IconPack/>} description={msg('Saved policy draft')} action={msg('Open policy')} href={'/policies/'+chat.policy.id+'?chat='+chatId} open={current?.kind==='policy'&&current.open}><ContextDetails context={current?.kind==='policy'?current:undefined}/></RelatedWorkEntry>}
    <PolicyChatProposals chatId={chatId} disabled={disabled} history/>
    <ChatTestProposals chatId={chatId} history/>
    {graphs.map(group => <GraphWork key={group.key} chatId={chatId} group={group} current={current} openGraphDraftId={openGraphDraftId}/>)}
    {!!historicalInputs.length && <Disclosure title={msg('Earlier proposals')}><div className={styles.history}>
      {historicalInputs.map(group => <DialectWork key={group.key} chatId={chatId} group={group} disabled={disabled}/>)}
    </div></Disclosure>}

  </RelatedWork>
}
