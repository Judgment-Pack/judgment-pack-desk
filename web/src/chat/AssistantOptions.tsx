import { useRef, useState } from 'react'
import type { ThinkingTier } from '../config/deskConfig'
import { IconGear } from '../shell/icons'
import { Button } from '../ui/Button'
import { Popover } from '../ui/Popover'
import { Tooltip } from '../ui/Tooltip'
import { ConfigureAssistant } from './ConfigureAssistant'
import styles from './ChatWorkspace.module.css'

const TOOL_LABELS: Record<string, string> = {
  get_schema: 'Read pack schema', list_examples: 'Browse examples', get_example: 'Read examples',
  validate: 'Validate pack structure', experimental_evaluate: 'Test a decision',
  experimental_test_packs: 'Run pack tests', experimental_validate_expectations: 'Validate test expectations',
  list_packs: 'List packs', get_pack: 'Read a pack', test_conformance: 'Check conformance'
}

/** One compact settings summary, shared by the home and pack conversations. */
export function AssistantOptions({ thinking, tools }: { thinking: ThinkingTier; tools: readonly string[] }) {
  const [open, setOpen] = useState(false)
  const [configure, setConfigure] = useState(false)
  const trigger = useRef<HTMLButtonElement>(null)
  const configureButton = useRef<HTMLButtonElement>(null)
  return <>
    <Popover title="Assistant settings" variant="list" size="small" open={open} onOpenChange={setOpen}
      triggerTooltip="Assistant settings" onEscapeKeyDown={event => event.stopPropagation()}
      onOpenAutoFocus={event => { event.preventDefault(); configureButton.current?.focus() }}
      onCloseAutoFocus={event => { if (configure) event.preventDefault() }}
      trigger={<button ref={trigger} className="desk-icon-button" type="button" aria-label="Assistant settings"><IconGear /></button>}>
      <div className={styles.settingsBody}>
        <dl className={styles.setting}><dt>Thinking</dt><dd>{thinking === 'off' ? 'Off' : thinking === 'ultra' ? 'Ultra' : 'On'}</dd></dl>
        <section className={styles.toolSection} aria-label="Allowed tools">
          <h3>Allowed tools <span>{tools.length}</span></h3>
          {tools.length ? <ul>{tools.map(tool => <li key={tool}><Tooltip content={tool}><span tabIndex={0}>{TOOL_LABELS[tool] ?? tool}</span></Tooltip></li>)}</ul> : <p>No tools configured.</p>}
        </section>
      </div>
      <div className={styles.settingsFooter}><Button ref={configureButton} variant="quiet" onClick={() => { setOpen(false); setConfigure(true) }}>Configure Assistant…</Button></div>
    </Popover>
    <ConfigureAssistant open={configure} onOpenChange={setConfigure} openerRef={trigger} />
  </>
}
