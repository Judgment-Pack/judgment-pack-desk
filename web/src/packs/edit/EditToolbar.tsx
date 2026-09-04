/**
 * The editing row: how the document is being looked at, what to ask of it, and
 * what to do with what has changed.
 *
 * Two segmented controls rather than two checkboxes, because Edit|Read and
 * Form|JSON are each one choice out of a closed pair and seeing the
 * alternative is the point. `Check` and `Try it` ask the runtime; `Discard`
 * and `Save` are about the bytes. The unsaved dot is beside Save because that
 * is the control it is about.
 *
 * **Save is never disabled by a diagnostic.** The chassis writes bytes and the
 * runtime judges them, in that order: a save gated on the check would make
 * this desk the thing that decides what may exist on disk, and an author
 * cannot save a draft they are halfway through fixing. It is disabled only for
 * the two states where there is nothing to send — a clean buffer, and a write
 * already in flight.
 */
import { Button } from '../../ui/Button'
import { SegmentedControl } from '../../ui/SegmentedControl'
import { Toolbar, ToolbarItem, ToolbarSeparator, ToolbarSlot, ToolbarSpacer } from '../../ui/Toolbar'
import type { EditShape } from './editMode'
import styles from './EditToolbar.module.css'

export function EditToolbar({
  editing,
  shape,
  shapeAvailable,
  dirty,
  saving,
  checking,
  tryingIt,
  canUndo,
  unwritten = 0,
  onEditing,
  onShape,
  onCheck,
  onTryIt,
  onUndo,
  onDiscard,
  onSave
}: {
  editing: boolean
  shape: EditShape
  /** False where the bytes cannot be read as a document: raw only. */
  shapeAvailable: boolean
  dirty: boolean
  saving: boolean
  checking: boolean
  tryingIt: boolean
  /** False where the stack is empty, which is what it says rather than lying. */
  canUndo: boolean
  /**
   * How many operands hold text that is not JSON yet, and so is not in the
   * buffer a save would send.
   *
   * Said beside the unsaved dot rather than used to refuse anything. Nothing
   * here is a gate: an author may save a document with an operand half typed,
   * and the only failure is that the text goes without being mentioned.
   */
  unwritten?: number
  onEditing: (editing: boolean) => void
  onShape: (shape: EditShape) => void
  onCheck: () => void
  onTryIt: () => void
  onUndo: () => void
  onDiscard: () => void
  onSave: () => void
}) {
  return (
    <Toolbar label="Editing">
      <ToolbarSlot>
        <SegmentedControl
          label="Mode"
          value={editing ? 'edit' : 'read'}
          onValueChange={(next) => onEditing(next === 'edit')}
          segments={[
            { value: 'edit', label: 'Edit' },
            { value: 'read', label: 'Read' }
          ]}
        />
      </ToolbarSlot>
      {editing && (
        <ToolbarSlot>
          <SegmentedControl
            label="Shape"
            value={shape}
            onValueChange={(next) => onShape(next === 'json' ? 'json' : 'form')}
            segments={[
              {
                value: 'form',
                label: 'Form',
                // Withheld rather than hidden: a control that vanished would
                // leave a viewer wondering whether the desk has forms at all.
                disabled: !shapeAvailable,
                title: shapeAvailable ? undefined : 'These bytes cannot be read as a document.'
              },
              { value: 'json', label: 'JSON' }
            ]}
          />
        </ToolbarSlot>
      )}
      <ToolbarSeparator />
      <ToolbarItem>
        {/*
          The word stays `Check` while one is in flight, and the strip under
          this row is what says "Checking…". Two elements carrying that
          sentence is two places a reader has to reconcile — and the one that
          is a *button* would be saying it about itself rather than about the
          document.
        */}
        <Button variant="quiet" onClick={onCheck} disabled={checking} aria-busy={checking}>
          Check
        </Button>
      </ToolbarItem>
      {editing && (
        <ToolbarItem>
          <Button variant="quiet" onClick={onTryIt} aria-pressed={tryingIt}>
            Try it
          </Button>
        </ToolbarItem>
      )}
      <ToolbarSpacer />
      {editing && (
        <>
          {/*
            The buffer keeps a capped stack of snapshots, one per committed
            action with typing coalesced per field. A control is what makes it
            a feature: a stack nothing can reach is a claim the surface does
            not deliver. It is a **button** and not a chord, because `Mod+Z`
            inside a text field is the field's own undo and taking it away
            would trade per-character undo for per-action undo without asking.
          */}
          <ToolbarItem>
            <Button variant="quiet" onClick={onUndo} disabled={!canUndo || saving}>
              Undo
            </Button>
          </ToolbarItem>
          <ToolbarItem>
            <Button variant="quiet" onClick={onDiscard} disabled={!dirty || saving}>
              Discard
            </Button>
          </ToolbarItem>
          <ToolbarItem>
            <Button variant="primary" onClick={onSave} disabled={!dirty || saving}>
              {saving ? 'Saving…' : 'Save'}
            </Button>
          </ToolbarItem>
          {dirty && <span className={styles.dot} aria-label="unsaved changes" role="img" />}
          {unwritten > 0 && (
            <span className={styles.unwritten}>
              {unwritten === 1 ? '1 field is not written yet' : `${unwritten} fields are not written yet`}
            </span>
          )}
        </>
      )}
    </Toolbar>
  )
}
