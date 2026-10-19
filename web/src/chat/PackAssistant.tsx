import {useFileContent} from '../files/queries'
import { PackProposalCard } from './PackProposalCard'
import { legacyPackBasis, pendingPackCandidate, samePack } from './packProposal'
import {ChatPlaceholder} from './ChatPlaceholder'
import { usePackChatContext } from './packChatContext'
import { msg, useLocale } from '../i18n'
import { useEffect, useRef, useState } from 'react'
import { useLocation, useNavigate, useSearchParams } from 'react-router-dom'
import { useEditing } from '../packs/edit/editingContext'
import type { BufferIdentity } from '../packs/edit/useDocumentBuffer'
import { applyProposal } from '../assistant/acceptProposal'
import { AssistantPane } from '../assistant/AssistantPane'
import { useInspectorControls } from '../shell/InspectorSlot'
import {ChatAction} from './ChatActions'
import { ChatPanel } from './ChatPanel'
import { useChats } from './ChatProvider'
import { assistantChatHref } from './navigation'
import { useTestStorage } from '../packs/test-workspace/store'
import { savedTestCases } from '../assistant/criticCases'

export function PackAssistant({ packId, title = packId, path, digest, draft, editing, identity, busy, diagnostics, active = true }: {
  active?: boolean; packId: string; title?: string; path?: string; digest?: string; draft?: string; editing: boolean; identity?: BufferIdentity; busy: () => string; diagnostics: { count: number; bytes: string } | undefined; onEdit?: () => void
}) {
  useLocale()
  const { store, ready, chats, drafts, bindings, error } = useChats()
  const [params] = useSearchParams()
  const navigate = useNavigate()
  const location = useLocation()
  const slot = useInspectorControls()
  const session = useEditing()
  const savedFile = useFileContent(path)
  const context = usePackChatContext(packId, draft, title)
  const tests = useTestStorage(packId)
  const explicit = params.get('chat')
  const available = [...chats, ...drafts]
  const chat = (explicit ? available.find(chat => chat.id === explicit && chat.pack?.id === packId) : undefined)
    ?? available.filter(chat => chat.pack?.id === packId && !chat.archived).sort((a,b) => b.updatedAt.localeCompare(a.updatedAt))[0]
  const creating = useRef(false)
  useEffect(() => {
    if (!store || !ready || !active) return
    if (chat) { creating.current = false; store.activate(chat.id) }
    else if (slot.open && store.canCreate && !creating.current) {
      creating.current = true
      const next = store.startChat({ id: packId, path: path ?? '', digest: digest ?? '' })
      navigate(assistantChatHref(next, location), { replace: true })
    }
  }, [active, store, ready, chat?.id, packId, path, digest, slot.open, navigate, location.pathname, location.search])
  const state = chat ? bindings.get(chat.id)?.state : undefined
  const candidate = state?.candidates.at(-1)
  const durableCandidate = state ? pendingPackCandidate(state, chat?.createdCandidateDigest) : undefined
  const durableBasis = durableCandidate?.packReview?.basis ?? (durableCandidate && state && chat?.pack ? legacyPackBasis(state,chat.pack,durableCandidate) : undefined)
  const [baseline, setBaseline] = useState<{ chatId: string; bytes: string; revision: number; identity?: BufferIdentity; fromView?: boolean; path?: string } | null>(null)
  // Recovery restores comparison data only. Apply still requires unchanged bytes
  // and a current editor identity, followed by the ordinary explicit Save.
  useEffect(() => {
    if (chat && durableCandidate && durableBasis && durableBasis.packId === packId && durableBasis.path === path && baseline?.chatId !== chat.id) {
      setBaseline({chatId:chat.id,bytes:durableBasis.text,revision:durableCandidate.revision-1,path,fromView:!editing,identity:editing ? identity : undefined})
    }
  }, [chat?.id,durableCandidate?.digest,durableBasis?.text,packId,path,editing,identity,baseline?.chatId])
  useEffect(() => {
    if (editing && baseline?.fromView && baseline.chatId === chat?.id && baseline.path === path && baseline.bytes === draft && identity) {
      setBaseline({ ...baseline, identity, fromView: false })
    }
  }, [editing, baseline, chat?.id, path, draft, identity])
  const [accepted, setAccepted] = useState('')
  const alreadySaved = samePack(candidate?.text,savedFile.data?.content) || !editing && samePack(candidate?.text,draft)
  const proposed = !alreadySaved && baseline?.chatId === chat?.id && candidate && candidate.revision > baseline.revision && state?.status !== 'running' ? candidate.document : undefined
  const unchanged = !!baseline && baseline.bytes === draft && baseline.identity?.path === identity?.path && baseline.identity?.generation === identity?.generation && baseline.identity?.revision === identity?.revision
  const eligible = editing && session.editing && unchanged && !busy() && session.pending.size === 0 && state?.status !== 'running' && proposed !== undefined
  // Standalone embeddings retain the existing assistant. The application
  // provides the project store above all routes.
  if (!store) return <AssistantPane draft={draft} editing={editing} identity={identity} busy={busy} diagnostics={diagnostics} criticCases={savedTestCases(tests.suite)} />
  if (!chat) return <ChatPlaceholder loading={!ready} error={!!error}>{!store.canCreate ? msg("Chat history is full. Export and delete an older chat from Chat history.") : msg("Open Assistant to start a conversation about this pack.")}</ChatPlaceholder>
  const actions = proposed === undefined || !candidate ? undefined : <PackProposalCard candidate={candidate} packId={packId} path={path} savedText={savedFile.data?.content ?? (!editing ? draft : baseline?.bytes)} comparisonText={draft} busy={state?.status==='running' || Boolean(busy()) || Boolean(store.running)} onCheck={()=>store.perform(chat.id, active=>active.run?.recheck(),false)}
    onCorrect={()=>store.perform(chat.id,active=>active.run?.send(msg('Correct the proposed pack update using the current policy and saved test findings. Keep saved cases unchanged; ask me if an expectation conflicts with policy.')))}
    basis={durableBasis ?? (baseline ? {packId,path:path??'',text:baseline.bytes} : undefined)} plan={chat.interaction==='plan'}
    onSaved={()=>{if(editing){const next=new URLSearchParams(params);next.delete('edit');navigate({pathname:location.pathname,search:next.toString()},{replace:true})}}}
    actionDetail={editing ? accepted===candidate.digest ? msg('Applied to the editor. Use Save to write these draft changes.') : !unchanged ? msg('The document changed since this request. Send another message against the current draft.') : undefined : undefined}
    actions={editing && <ChatAction disabled={!eligible || accepted===candidate.digest} onClick={()=>{
        if(!eligible || proposed===undefined)return
        session.write(current=>current.text===baseline?.bytes?applyProposal(current,proposed):current,{coalesceKey:`chat-accept:${chat.id}:${candidate.digest}`})
        setAccepted(candidate.digest)
      }}>{accepted===candidate.digest?msg('Applied to draft'):msg('Apply to draft')}</ChatAction>
    }/>
  return <ChatPanel active={active} placement="pane" key={chat.id} chat={chat} proposalActions={actions ?? null} context={draft === undefined ? undefined : { ...context!, text: draft, beforeSend: () => { if (baseline?.chatId === chat.id && baseline.bytes === draft && (unchanged || baseline.fromView && !editing)) return; setBaseline({ chatId: chat.id, bytes: draft, revision: candidate?.revision ?? 0, identity, fromView: !editing, path }); setAccepted('') } }} locked={Boolean(busy())} />
}
