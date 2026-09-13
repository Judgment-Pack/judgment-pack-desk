/** Pointer-addressed Inspector details. Pane state remembers the last disclosure;
 * the selected document member remains in the route across drawer remounts. */
import type { PackDocument, PackFileMeta } from '../../mcp/types'
import type { AnchoredDiagnostic } from '../checks'
import { diagnosticsFor } from '../checks'
import { valueAt } from '../pointers'
import { isRecord } from '../document/MisshapenMember'
import { fieldLabel } from '../terminology'
import { referencesFor } from '../references'
import { ChecksTab } from './ChecksTab'
import { MemberTab } from './MemberTab'
import { ReferencesTab } from './ReferencesTab'
import styles from './PackInspector.module.css'

export function PackInspector({
  packId,
  document: doc,
  at,
  meta,
  fileSha256,
  baseSha256,
  fileBytes,
  dirty,
  anchored,
  truncation,
  stale,
  pending,
  checkedWhat,
  unavailable,
  tab,
  onTabChange,
  supplemental = false
}: {
  packId: string
  document: PackDocument | undefined
  /** The pointer `?at` holds, or null where nothing is selected. */
  at: string | null
  meta: PackFileMeta
  fileSha256: string | undefined
  /** The digest of the revision the editor loaded, where it holds one. */
  baseSha256?: string | undefined
  fileBytes: number | undefined
  /** True where the editor holds bytes that are on no disk yet. */
  dirty?: boolean
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
  supplemental?: boolean
}) {
  if (doc === undefined) {
    // **No fallback to the served pack.** The page is over the bytes the editor
    // holds, and where those are not a document there is no member to inspect —
    // showing the runtime's last good answer here would put members and
    // references on screen that the file no longer carries.
    return (
      <p className={styles.empty}>
        The bytes in the editor are not a document this desk can read, so there is no member to
        inspect. The JSON view holds them.
      </p>
    )
  }
  if (at === null) {
    return (
      <p className={styles.empty}>
        Select a member of the document to inspect it here.
      </p>
    )
  }
  const references = referencesFor(doc, at)
  const diagnostics = diagnosticsFor(anchored, at)
  const attention = stale || pending || unavailable !== undefined || truncation !== undefined || diagnostics.length > 0
  const value = subtreeAt(doc, at)
  const heading = isRecord(value) ? [value.label, value.title, value.id].find(candidate => typeof candidate === 'string') : undefined
  const key = at.split('/').filter(Boolean).at(-1) ?? 'Document'
  const name = fieldLabel(key, key.replace(/([a-z])([A-Z])/g, '$1 $2'))

  return <div className={supplemental ? styles.supplemental : styles.inspector}>
    {!supplemental && <h2>{typeof heading === 'string' ? heading : name.charAt(0).toUpperCase() + name.slice(1)}</h2>}
    <MemberTab pointer={at} subtree={subtreeAt(doc, at)} meta={meta}
      fileSha256={fileSha256} baseSha256={baseSha256} fileBytes={fileBytes}
      dirty={dirty} metadataOnly={supplemental} />
    <details className={styles.disclosure} open={tab === 'references'}
      onToggle={event => { if (event.currentTarget.open) onTabChange('references'); else if (tab === 'references') onTabChange('member') }}>
      <summary>References · {references.length}</summary>
      <ReferencesTab references={references} packId={packId} />
    </details>
    <details className={styles.disclosure} open={tab === 'checks' || attention}
      onToggle={event => { if (event.currentTarget.open && !attention) onTabChange('checks'); else if (!event.currentTarget.open && tab === 'checks') onTabChange('member') }}>
      <summary>Checks{pending ? ' · Checking…' : attention ? ' · Attention' : ` · ${diagnostics.length}`} </summary>
      <ChecksTab diagnostics={diagnostics} truncation={truncation} stale={stale}
        pending={pending} checkedWhat={checkedWhat} unavailable={unavailable} />
    </details>
  </div>
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
