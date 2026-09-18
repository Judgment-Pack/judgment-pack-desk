import { msg, useLocale } from '../../i18n'
/** Secondary draft controls. Save and exit belong to the pinned editor header. */
import { Button } from '../../ui/Button'
import { SegmentedControl } from '../../ui/SegmentedControl'
import { Toolbar, ToolbarItem, ToolbarSeparator, ToolbarSlot, ToolbarSpacer } from '../../ui/Toolbar'
import type { EditShape } from './editMode'

export function EditToolbar({
  shape, shapeAvailable, discardable, saving, checking, tryingIt, canUndo,
  onShape, onCheck, onTryIt, onUndo, onDiscard
}: {
  shape: EditShape
  /** False where the bytes cannot be read as a document: raw only. */
  shapeAvailable: boolean
  /** Includes unfinished operand text as well as changed document bytes. */
  discardable: boolean
  saving: boolean
  checking: boolean
  tryingIt: boolean
  canUndo: boolean
  onShape: (shape: EditShape) => void
  onCheck: () => void
  onTryIt: () => void
  onUndo: () => void
  onDiscard: () => void
}) {
  useLocale()
  return <Toolbar label={msg("Editing")}>
    <ToolbarSlot>
      <SegmentedControl label={msg("Shape")} value={shape}
        onValueChange={next => onShape(next === 'json' ? 'json' : 'form')}
        segments={[
          { value: 'form', label: msg("Form"), disabled: !shapeAvailable,
            description: shapeAvailable ? undefined : msg('These bytes cannot be read as a document.') },
          { value: 'json', label: "JSON" }
        ]} />
    </ToolbarSlot>
    <ToolbarSeparator />
    <ToolbarItem>
      <Button variant="quiet" onClick={onCheck} disabled={checking} aria-busy={checking}>{msg("Check")}</Button>
    </ToolbarItem>
    <ToolbarItem>
      <Button variant="quiet" onClick={onTryIt} aria-pressed={tryingIt}>{msg("Test draft")}</Button>
    </ToolbarItem>
    <ToolbarSpacer />
    <ToolbarSlot>
      <ToolbarItem>
        <Button variant="quiet" onClick={onUndo} disabled={!canUndo || saving}>{msg("Undo")}</Button>
      </ToolbarItem>
      <ToolbarItem>
        <Button variant="quiet" onClick={onDiscard} disabled={!discardable || saving}>{msg("Discard")}</Button>
      </ToolbarItem>
    </ToolbarSlot>
  </Toolbar>
}
