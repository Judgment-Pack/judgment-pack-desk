/**
 * The Inspector's three panels: Member, References, Checks.
 *
 * **Selection is the pointer in `?at` and never pane state.** `RightPane`
 * swaps its wrapper at 1100px and remounts the subtree, so anything held here
 * is lost at that breakpoint — and a selection in the address is also a link
 * someone can send.
 *
 * The tab itself is pane-shaped state and is held by the shell's slot, which
 * survives the same swap because the frame holds it above `<main>`.
 *
 * Board 1 draws a fourth tab, Document, while also drawing the provenance
 * group inside Member. There are three here and the provenance is in Member,
 * exactly as drawn; a fourth tab is additive and would duplicate that group
 * unless Member gave it up.
 */
import { Tabs } from '../../ui/Tabs'
import type { PackDocument, PackFileMeta } from '../../mcp/types'
import type { AnchoredDiagnostic } from '../checks'
import { diagnosticsFor } from '../checks'
import { valueAt } from '../pointers'
import { referencesFor } from '../references'
import { ChecksTab } from './ChecksTab'
import { MemberTab } from './MemberTab'
import { ReferencesTab } from './ReferencesTab'
import styles from './PackInspector.module.css'

const TABS = ['member', 'references', 'checks'] as const

export function PackInspector({
  packId,
  document: doc,
  at,
  meta,
  fileSha256,
  fileBytes,
  anchored,
  truncation,
  stale,
  pending,
  checkedWhat,
  unavailable,
  tab,
  onTabChange
}: {
  packId: string
  document: PackDocument | undefined
  /** The pointer `?at` holds, or null where nothing is selected. */
  at: string | null
  meta: PackFileMeta
  fileSha256: string | undefined
  fileBytes: number | undefined
  anchored: readonly AnchoredDiagnostic[]
  truncation: string | undefined
  stale: boolean
  /** True while the check is still in flight. */
  pending: boolean
  /**
   * Which bytes the check is about, where a check has happened or is happening.
   *
   * Optional, because "checked against the bytes of x" printed under "this
   * document is unchecked" is a claim about a check that did not run.
   */
  checkedWhat?: string
  unavailable?: string
  tab: string | null
  onTabChange: (tab: string) => void
}) {
  if (at === null || doc === undefined) {
    return (
      <p className={styles.empty}>
        Select a member of the document to inspect it here.
      </p>
    )
  }
  const current = (TABS as readonly string[]).includes(tab ?? '') ? tab! : 'member'

  return (
    <Tabs
      label="Inspector panels"
      value={current}
      onValueChange={onTabChange}
      tabs={[
        {
          value: 'member',
          label: 'Member',
          panel: (
            <MemberTab
              pointer={at}
              subtree={subtreeAt(doc, at)}
              meta={meta}
              fileSha256={fileSha256}
              fileBytes={fileBytes}
            />
          )
        },
        {
          value: 'references',
          label: 'References',
          panel: <ReferencesTab references={referencesFor(doc, at)} packId={packId} />
        },
        {
          value: 'checks',
          label: 'Checks',
          panel: (
            <ChecksTab
              diagnostics={diagnosticsFor(anchored, at)}
              truncation={truncation}
              stale={stale}
              pending={pending}
              checkedWhat={checkedWhat}
              unavailable={unavailable}
            />
          )
        }
      ]}
    />
  )
}

/**
 * The value one pointer names inside the served document, or undefined.
 *
 * Re-exported rather than reimplemented. This file had its own walk, and it
 * differed from the other two in ways that showed a subtree the address did not
 * name — `Number(part)` took `01` and `1e0` for indices, and `in` consults the
 * prototype chain, so `/constructor` selected something no JSON document has.
 * There is one evaluator now and this is a name for it.
 */
export const subtreeAt = valueAt
