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
import { parsePointer } from '../pointers'
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
  checkedWhat: string
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
              checkedWhat={checkedWhat}
              unavailable={unavailable}
            />
          )
        }
      ]}
    />
  )
}

/** The value one pointer names inside the served document, or undefined. */
export function subtreeAt(document: unknown, pointer: string): unknown {
  const parts = parsePointer(pointer)
  if (parts === undefined) return undefined
  let value: unknown = document
  for (const part of parts) {
    if (Array.isArray(value)) {
      const index = Number(part)
      if (!Number.isInteger(index)) return undefined
      value = value[index]
      continue
    }
    if (typeof value === 'object' && value !== null && part in (value as object)) {
      value = (value as Record<string, unknown>)[part]
      continue
    }
    return undefined
  }
  return value
}
