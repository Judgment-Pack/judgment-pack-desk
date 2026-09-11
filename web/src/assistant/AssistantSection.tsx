/**
 * Admin › Assistant: the endpoint this desk is configured for, and the one key
 * it keeps — as one of Admin's cards.
 *
 * **The card is a heading, a location, a status and one form.** The rows of
 * facts that used to sit above the form said, in a second vocabulary, what the
 * fields below them already say: which endpoint, whether a key is stored, what
 * the probe answered. One order, one set of words, and the person setting this
 * up reads down it once. `EndpointForm` is that form.
 *
 * **A configuration this desk could not read is its own state here**, and it is
 * the card's Status line rather than a warning note: the form then holds the
 * built-in defaults rather than anything anybody configured, so it is shown and
 * not edited.
 *
 * **And a member this desk migrated is a Status line too.** A file naming the
 * withdrawn engine is accepted and decodes to the engine that runs; the decoder
 * says so, in its own words, and this is where that sentence is shown. It is a
 * notice and never a refusal — the file was read, and everything in it is in
 * use.
 *
 * Nothing here says chassis, bytes or path to the reader. The words are the
 * desk, this computer, and the file.
 */
import { SourceCard, type SourceStatus } from '../admin/SourceCard'
import { useEffectiveConfig } from '../config/DeskConfigProvider'
import { NO_MODEL_CHOSEN, type DeskLevelSummary } from '../config/deskConfig'
import { EndpointForm } from './EndpointForm'
import { useAssistantSlot } from './useAssistantSlot'

export function AssistantSection({
  id,
  title,
  under,
  level
}: {
  id: string
  title: string
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
      title={title}
      under={under}
      level={level}
      location={
        desk === undefined ? (
          <span className="quiet">nothing has asked for it</span>
        ) : (
          <code>{desk.path}</code>
        )
      }
      status={under !== undefined && setupOnly ? under : status}
      save={<EndpointForm unavailable={slot.state === 'unavailable'} />}
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
