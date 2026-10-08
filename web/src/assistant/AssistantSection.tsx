import { msg, useLocale } from '../i18n'
/** Per-desk model preferences. Shared accounts and defaults live in Connections.
 * Keep configuration failures visible while hiding healthy storage metadata. */
import { SourceCard, type SourceStatus } from '../admin/SourceCard'
import { useEffectiveConfig } from '../config/DeskConfigProvider'
import { NO_MODEL_CHOSEN, type DeskLevelSummary } from '../config/deskConfig'
import { AssistantSettings } from './AssistantSettings'
import { useAssistantSlot } from './useAssistantSlot'

export function AssistantSection({
  id,
  title,
  under,
  level, blocked = false, onDirtyChange
}: {
  id: string
  title: string
  blocked?: boolean
  onDirtyChange?: (dirty:boolean)=>void
  /**
   * The status of the group header this card sits under, where there is one.
   *
   * Passed straight through. This section is about a member of the desk-level
   * file, so under the group that names that file it repeats neither the
   * location nor a status the header has already given — but it still computes
   * its own, because a member migrated or refused inside an accepted file is a
   * sentence only this card has.
   */
  under?: SourceStatus
  /** The heading level, where the page's outline is not the card's nesting. */
  level?: 2 | 3
}) {
  useLocale()
  const { desk } = useEffectiveConfig()
  // **The same reading the tab and Describe it take.** A read that did not
  // produce a file establishes nothing about what is in it, and this section is
  // where a reader would go to find that out — so it must not be the one surface
  // still asserting an absence.
  const slot = useAssistantSlot()
  const status = assistantStatus(desk)
  // The form already explains model setup. Keep decoder failures and other
  // migration notices visible, without repeating this one above the form.
  const setupOnly = status.state === 'migrated' && status.notices.every(
    (notice) => notice.says === NO_MODEL_CHOSEN
  )

  return (
    <SourceCard
      id={id}
      presentation="settings"
      title={title}
      under={under}
      level={level}
      location={
        desk === undefined ? (
          <span className="quiet">{msg("nothing has asked for it")}</span>
        ) : (
          <code>{desk.path}</code>
        )
      }
      status={under !== undefined && setupOnly ? under : status}
      save={<AssistantSettings unavailable={blocked || slot.state === 'unavailable'} onDirtyChange={onDirtyChange}/>}
    />
  )
}

/**
 * The desk-level file's state, which is this card's state: `assistant` may be
 * configured nowhere else.
 *
 * The migration comes last, because it is a statement about a file that was
 * **read**: a refused file decoded to nothing and migrated nothing, and an
 * absent one has no member to migrate.
 */
function assistantStatus(desk: DeskLevelSummary | undefined): SourceStatus {
  if (desk === undefined) return { state: 'pending' }
  if (desk.problems.length > 0) return { state: 'refused', problems: desk.problems }
  if (desk.readFailure !== undefined) return { state: 'unread', failure: desk.readFailure }
  if (!desk.present) return { state: 'absent' }
  // Only this section's own members. A notice about another section's would be
  // this card reporting a migration it is not the place to repair.
  const migrated = (desk.decoded?.notices ?? []).filter((notice) =>
    notice.key.startsWith('assistant.')
  )
  if (migrated.length > 0) return { state: 'migrated', notices: migrated }
  return { state: 'read' }
}
