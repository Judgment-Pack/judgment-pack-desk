import { Message } from '../i18n/Message'
import { msg, useLocale } from '../i18n'
import { useRef, useState } from 'react'
import { useQuery } from '@tanstack/react-query'
import { useFileListing } from '../files/queries'
import { useMcp } from '../mcp/McpProvider'
import { listAllTools } from '../mcp/capabilities'
import { useEffectiveConfig } from '../config/DeskConfigProvider'
import { usePromptNames, TEST_PACK_PROMPT } from '../mcp/prompts'
import { TOOL_LABELS } from './toolLabels'
import type { ThinkingTier } from '../config/deskConfig'
import { IconGear } from '../shell/icons'
import { Button } from '../ui/Button'
import { Popover } from '../ui/Popover'
import { Tooltip } from '../ui/Tooltip'
import { ConfigureAssistant } from './ConfigureAssistant'
import styles from './ChatWorkspace.module.css'

/** One compact settings summary, shared by the home and pack conversations. */
export function AssistantOptions({ thinking, tools, mode = 'draft', review = false, onReview, disabled, notice }: {
  thinking: ThinkingTier; tools: readonly string[]; mode?: 'draft' | 'research'; review?: boolean
  onReview?: (value: boolean) => void; disabled?: boolean; notice?: string
}) {
  useLocale()
  const [open, setOpen] = useState(false)
  const [configure, setConfigure] = useState(false)
  const mcp = useMcp()
  const project = useFileListing().data?.root
  const { config } = useEffectiveConfig()
  const prompts = usePromptNames()
  const reviewAvailable = mcp.status === 'ready' && (prompts.data ?? []).includes(TEST_PACK_PROMPT)
  const listed = useQuery({ queryKey: ['assistant-tool-options', project, mcp.connectionEpoch], enabled: open && mcp.status === 'ready' && !!mcp.client,
    queryFn: () => listAllTools(mcp.client!), staleTime: 60_000 })
  const available = mcp.status === 'ready' ? tools.filter(name => listed.data?.some(tool => tool.name === name)) : []
  const research = config.research
  const host = mode === 'research' && research.gateway ? [
    ...(research.sources.search ? ['search_sources'] : []), ...(research.sources.read ? ['read_source', 'cite_excerpt'] : [])] : []
  const trigger = useRef<HTMLButtonElement>(null)
  const configureButton = useRef<HTMLButtonElement>(null)
  return <>
    <Popover title={msg("Assistant settings")} variant="list" size="small" open={open} onOpenChange={setOpen}
      triggerTooltip={msg("Assistant settings")} onEscapeKeyDown={event => event.stopPropagation()}
      onOpenAutoFocus={event => { event.preventDefault(); configureButton.current?.focus() }}
      onCloseAutoFocus={event => { if (configure) event.preventDefault() }}
      trigger={<button ref={trigger} className="desk-icon-button" type="button" aria-label={msg("Assistant settings")}><IconGear /></button>}>
      <div className={styles.settingsBody}>
        <dl className={styles.setting}><dt>{msg("Thinking requested")}</dt><dd>{thinking === 'off' ? msg("Off") : thinking === 'ultra' ? msg("Ultra") : msg("On")}</dd></dl>
        <p className={styles.caption}>{notice || msg("Reasoning support depends on the selected model.")}</p>
        {onReview && <label className={styles.reviewOption}><Message text={"<0/> Adversarial review"} slots={[<input type="checkbox" checked={review} disabled={disabled || !reviewAvailable} onChange={event => onReview(event.target.checked)} />]} /></label>}
        <p className={styles.caption}>{reviewAvailable ? msg("Optional model review of proposed changes. Runtime validation remains required.") : msg("Connect a runtime with test_pack to enable adversarial review.")}</p>
        <section className={styles.toolSection} aria-label={msg("Allowed tools")}>
          <h3><Message text={"Available tools <0/>"} slots={[<span>{available.length + host.length}</span>]} /></h3>
          {available.length + host.length ? <ul>{[...available, ...host].map(tool => <li key={tool}><Tooltip content={tool}><span tabIndex={0}>{TOOL_LABELS[tool] ?? tool}</span></Tooltip></li>)}</ul> : <p>{mcp.status !== 'ready' ? msg("Runtime tools are unavailable while disconnected. You can still chat.") : listed.isPending ? msg("Checking runtime tools…") : msg("No enabled tools are available.")}</p>}
          <p className={styles.caption}>{msg("Authoring instructions are loaded when needed. Tool access follows Admin settings.")}</p>
        </section>
      </div>
      <div className={styles.settingsFooter}><Button ref={configureButton} variant="quiet" onClick={() => { setOpen(false); setConfigure(true) }}>{msg("Configure Assistant…")}</Button></div>
    </Popover>
    <ConfigureAssistant open={configure} onOpenChange={setConfigure} openerRef={trigger} />
  </>
}
