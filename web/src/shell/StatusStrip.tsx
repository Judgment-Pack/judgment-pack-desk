import { useChats } from '../chat/ChatProvider'
import { DeskHomeNotice, useDeskHome } from '../admin/DeskHomeSettings'
import { ProjectLocationNotice, useProjectLocation } from '../admin/ProjectLocation'
import { AI_CONNECTIONS_KEY } from '../assistant/aiConnections'
import { useQueryClient } from '@tanstack/react-query'
import { msg, systemMessage, useLocale } from '../i18n'
import { useEffectiveConfig } from '../config/DeskConfigProvider'
import { DESK_CONFIG_QUERY_KEY } from '../config/queries'
import { useMcp } from '../mcp/McpProvider'
import { ProjectSafeguardsStatus } from '../packs/upgrade/UpgradeNote'
import { Button, ButtonLink } from '../ui/Button'
import { PopoverClose } from '../ui/Popover'
import { ConnectionStatus } from './ConnectionNotices'
import { DeskNotices, DeskStatusNotice } from './DeskNotices'

export const CONFIG_REFUSED_CUE = 'Settings need attention'
export const CONFIG_UNREAD_CUE = 'Settings unavailable'

/** Global health uses existing configuration reads; visiting a page does not start new probes. */
function SystemNotices() {
  useLocale()
  const { status, known } = useMcp()
  const { store, ready: chatsReady, error: chatError, saving } = useChats()
  const { problems, readFailure, desk, aiConnections } = useEffectiveConfig()
  const client = useQueryClient()
  const refused = problems.length > 0 || (desk?.problems.length ?? 0) > 0
  const unread = readFailure !== undefined || desk?.readFailure !== undefined
  const local = desk?.localGateway
  const managed = !desk?.decoded?.values?.research?.gateway
  const refresh = () => { void client.invalidateQueries({queryKey: DESK_CONFIG_QUERY_KEY}) }
  const settingsAction = <PopoverClose><ButtonLink to="/admin">{msg('Review settings')}</ButtonLink></PopoverClose>
  return <>
    {chatError && <DeskStatusNotice id="chat-history" tone={chatsReady ? 'error' : 'warning'} label={chatsReady ? msg('Chat changes not saved') : msg('Chat history unavailable')}
      detail={systemMessage(chatError)} actions={<>{!chatsReady ? <Button onClick={() => void store?.load()}>{msg('Retry')}</Button> : store?.canCreate ? <Button disabled={saving} onClick={() => store.retrySave()}>{saving ? msg('Retrying…') : msg('Retry saving')}</Button> : null}<PopoverClose><ButtonLink to="/chats">{msg('Manage chat history')}</ButtonLink></PopoverClose></>}>
      {chatsReady ? msg('Chat changes could not be saved. Keep this window open until saving succeeds.') : msg('Saved chats and drafts could not be loaded.')}
    </DeskStatusNotice>}
    {refused && <DeskStatusNotice id="configuration" tone="warning" label={msg(CONFIG_REFUSED_CUE)} actions={settingsAction}>
      {msg('Some settings were not accepted. Review them in Admin.')}
    </DeskStatusNotice>}
    {!refused && unread && <DeskStatusNotice id="configuration" tone="warning" label={msg(CONFIG_UNREAD_CUE)} actions={<><Button onClick={refresh}>{msg('Retry')}</Button>{settingsAction}</>}>
      {msg('Settings could not be read. Reload the page before making changes.')}
    </DeskStatusNotice>}
    {managed && local?.status === 'unavailable' && <DeskStatusNotice id="local-processing" tone="warning" label={msg('Local processing unavailable')}
      detail={local.problem ? systemMessage(local.problem) : undefined}
      actions={<><Button onClick={refresh}>{msg('Refresh')}</Button><PopoverClose><ButtonLink to="/admin#documents">{msg('Review settings')}</ButtonLink></PopoverClose></>}>
      {msg('Local processing is unavailable. Check the details in Admin → Document processing.')}
    </DeskStatusNotice>}
    {aiConnections?.problem && <DeskStatusNotice id="ai-connections" tone="warning" label={msg('AI connections unavailable')}
      detail={systemMessage(aiConnections.problem)} actions={<><Button onClick={() => void client.invalidateQueries({queryKey: AI_CONNECTIONS_KEY})}>{msg('Retry')}</Button><PopoverClose><ButtonLink to="/admin#connections-ai">{msg('Review settings')}</ButtonLink></PopoverClose></>}>
      {msg('Assistant connections could not be loaded.')}
    </DeskStatusNotice>}
    {!refused && !unread && status === 'ready' && known && <ProjectSafeguardsStatus/>}
  </>
}

export function StatusStrip() {
  const location = useProjectLocation()
  const home = useDeskHome()
  return <footer className="desk-strip">
    <SystemNotices/>
    {!home.data?.restartRequired && <ProjectLocationNotice/>}
    <DeskHomeNotice/>
    <div className="desk-strip-left"><DeskNotices/></div>
    {!location.data?.restartRequired && !home.data?.restartRequired && <ConnectionStatus/>}
  </footer>
}
