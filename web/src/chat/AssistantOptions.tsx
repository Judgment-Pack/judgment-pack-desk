import { MODEL_TIERS } from '@judgment-pack/harness/criticSwitch'
import type { CriticTier } from '@judgment-pack/harness/contract'
import { useProviderModels } from '../assistant/providers'
import type { CriticMode } from '@judgment-pack/harness/contract'
import {LoadingState} from '../ui/LoadingState'
import { conversationMode, type AuthoringMode } from '../research/mode'
import { useSearchConnections, useSearchPreference } from '../search/connections'
import { Message } from '../i18n/Message'
import { systemMessage, msg, useLocale } from '../i18n'
import { useRef, useState, type ReactNode } from 'react'
import { useQuery } from '@tanstack/react-query'
import { useFileListing } from '../files/queries'
import { useMcp } from '../mcp/McpProvider'
import { listAllTools } from '../mcp/capabilities'
import { useEffectiveConfig } from '../config/DeskConfigProvider'
import { usePromptNames, TEST_PACK_PROMPT } from '../mcp/prompts'
import { EXPLORE_WEBSITE, READ_LINK } from './linkTools'
import { TOOL_LABELS } from './toolLabels'
import type { ThinkingTier } from '../config/deskConfig'
import { IconGear } from '../shell/icons'
import {ChatAction} from './ChatActions'
import { Popover } from '../ui/Popover'
import { Tooltip } from '../ui/Tooltip'
import { ConfigureAssistant } from './ConfigureAssistant'
import styles from './ChatWorkspace.module.css'

/** One compact settings summary, shared by the home and pack conversations. */
export function AssistantOptions({ reasoning, researchMode, searchConnection, searchOptions, thinking, tools, mode = 'draft', review, onReview, criticReasoning, onCriticReasoning, criticModel, connectionId, codex = false, disabled, notice, linkReading = false, websiteExploration=false }: {
  reasoning?: string; researchMode?:'auto'|'provided'; searchConnection?:string; searchOptions?:ReactNode; thinking: ThinkingTier; tools: readonly string[]; mode?: AuthoringMode; review?: CriticMode
  criticReasoning?:CriticTier; onCriticReasoning?:(value:CriticTier|undefined)=>void; criticModel?:string; connectionId?:string; codex?:boolean
  onReview?: (value: CriticMode | undefined) => void; disabled?: boolean; notice?: string
  /** Whether an ordinary chat may read links the person gives (the gateway offers the web source). */
  linkReading?: boolean; websiteExploration?:boolean
}) {
  useLocale()
  const [open, setOpen] = useState(false)
  const [configure, setConfigure] = useState(false)
  const mcp = useMcp()
  const project = useFileListing().data?.root
  const { config } = useEffectiveConfig()
  const criticCatalog=useProviderModels(codex,connectionId)
  const criticTiers=codex ? criticCatalog.data?.models.find(model=>model.id===criticModel)?.efforts ?? [] : MODEL_TIERS
  const prompts = usePromptNames()
  const reviewAvailable = mcp.status === 'ready' && (prompts.data ?? []).includes(TEST_PACK_PROMPT)
  const listed = useQuery({ queryKey: ['assistant-tool-options', project, mcp.connectionEpoch], enabled: open && mcp.status === 'ready' && !!mcp.client,
    queryFn: () => listAllTools(mcp.client!), staleTime: 60_000 })
  const available = mcp.status === 'ready' ? tools.filter(name => listed.data?.some(tool => tool.name === name)) : []
  const research = config.research
  const searchConnections=useSearchConnections(),searchPreference=useSearchPreference()
  const auto=!!searchPreference.data&&!searchPreference.isError&&(researchMode??searchPreference.data.value.mode)==='auto'
  const webSearch=auto&&searchConnections.available&&!searchConnections.isError&&searchConnections.data?.connections.some(c=>c.id===(searchConnection??searchPreference.data?.value.connection))
  const host = mode === 'research' && research.gateway ? [
    ...(research.sources.search ? ['search_sources'] : []), ...(research.sources.read ? ['read_source', 'cite_excerpt'] : [])]
    : conversationMode(mode) && linkReading ? [...(webSearch?['search_sources']:[]),READ_LINK,...(auto&&websiteExploration?[EXPLORE_WEBSITE]:[])] : []
  const trigger = useRef<HTMLButtonElement>(null)
  const configureButton = useRef<HTMLButtonElement>(null)
  return <>
    <Popover title={msg("Assistant settings")} variant="list" size="small" open={open} onOpenChange={setOpen}
      triggerTooltip={msg("Assistant settings")} onEscapeKeyDown={event => event.stopPropagation()}
      onOpenAutoFocus={event => { event.preventDefault(); configureButton.current?.focus() }}
      onCloseAutoFocus={event => { if (configure) event.preventDefault() }}
      trigger={<ChatAction variant="quiet" size="icon" ref={trigger} type="button" aria-label={msg("Assistant settings")}><IconGear /></ChatAction>}>
      <div className={styles.settingsBody}>
        {searchOptions}
        <dl className={styles.setting}><dt>{config.assistant.engine === "codex" ? msg("Codex reasoning") : msg("Thinking requested")}</dt><dd>{config.assistant.engine === "codex" ? reasoning ?? msg("Model default") : thinking === 'off' ? msg("Off") : thinking === 'ultra' ? msg("Ultra") : msg("On")}</dd></dl>
        <p className={styles.caption}>{(notice ? systemMessage(notice) : undefined) || msg("Reasoning support depends on the selected model.")}</p>
        {onReview && <label className={styles.reviewOption}>{msg('Adversarial review')}<select aria-label={msg('Adversarial review')} value={review ?? ''} disabled={disabled} onChange={event => onReview(event.target.value === '' ? undefined : event.target.value as CriticMode)}>
          <option value="">{msg('Project default')}</option><option value="off">{msg('Off')}</option><option value="auto">{msg('Auto')}</option><option value="on">{msg('On')}</option>
        </select></label>}
        {onCriticReasoning && <label className={styles.reviewOption}>{msg('Critic reasoning')}<select aria-label={msg('Critic reasoning')} value={criticReasoning??''} disabled={disabled||codex&&(criticCatalog.isPending||criticCatalog.isError)} onChange={event=>onCriticReasoning(event.target.value===''?undefined:event.target.value as CriticTier)}><option value="">{msg('Author reasoning')}</option>{criticTiers.map(tier=><option key={tier} value={tier}>{tier}</option>)}</select></label>}
        {review === 'auto'  && <p className={styles.caption}>{msg('A review runs when the run proposes a pack with saved tests, when the proposal changes an existing pack, or when the assistant asks.')}</p>}
        <p className={styles.caption}>{reviewAvailable ? msg("Optional model review of proposed changes. Runtime validation remains required.") : msg("Connect a runtime with test_pack to enable adversarial review.")}</p>
        <section className={styles.toolSection} aria-label={msg("Allowed tools")}>
          <h3><Message text={"Available tools <0/>"} slots={[<span>{available.length + host.length}</span>]} /></h3>
          {available.length + host.length ? <ul>{[...available, ...host].map(tool => <li key={tool}><Tooltip content={tool}><span tabIndex={0}>{TOOL_LABELS[tool] ?? tool}</span></Tooltip></li>)}</ul> : mcp.status === 'ready' && listed.isPending ? <LoadingState>{msg('Checking runtime tools…')}</LoadingState> : <p>{mcp.status !== 'ready' ? msg("Runtime tools are unavailable while disconnected. You can still chat.") : msg("No enabled tools are available.")}</p>}
          <p className={styles.caption}>{msg("Authoring instructions are loaded when needed. Tool access follows Admin settings.")}</p>
        </section>
      </div>
      <div className={styles.settingsFooter}><ChatAction ref={configureButton} variant="quiet" onClick={() => { setOpen(false); setConfigure(true) }}>{msg("Configure Assistant…")}</ChatAction></div>
    </Popover>
    <ConfigureAssistant open={configure} onOpenChange={setConfigure} openerRef={trigger} />
  </>
}
