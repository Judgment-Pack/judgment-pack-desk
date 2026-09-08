/**
 * The fields on the cards that write the project's own configuration file, and
 * nothing else about them.
 *
 * Each form is the members its decoder knows, in the order the schema declares
 * them, with **a hint only where a rule exists** — and where one does, it is
 * the decoder's own sentence rather than a second one written here. A field
 * whose rules are several and specific carries none: the decoder names the one
 * that is broken, at the field it belongs to, at the moment it is broken, which
 * is more use than a paragraph guessing which it will be.
 *
 * **A field is not a declaration.** Every form seeds from the effective
 * configuration — which is the file's value, or the built-in one where the file
 * says nothing — and writes only the fields that differ from that seed. So a
 * pane dimension nobody touched stays undeclared, and the Inspector's drawer
 * keeps its own baseline.
 *
 * **There is no pane-dimension form here any more.** The Panes card offered
 * three of them and a reset of this browser's own record of the layout, and a
 * settings page editing the frame it is drawn in is a control looking for a
 * pane. The `panes` member is still decoded, still applied and still validated
 * — what left is the form, and its write path left with it, because a Save
 * with no control behind it is a write path nothing offers.
 *
 * **And there is no Appearance form either, for a different reason.** Theme and
 * density are a person's preference and not an organization's setting: this
 * card wrote them into a file in the project's repository, so one person
 * choosing dark chose it for everyone who ever cloned it. They are set in the
 * shell's user menu now, per viewer and per browser. `appearance` is still in
 * the schema, still decoded and still applied — as the **default** for everyone
 * who has not chosen — and, like `panes`, its write path left with its form.
 */
import { useEffectiveConfig } from '../config/DeskConfigProvider'
import {
  ID_BASE_NORMALISES,
  ID_BASE_SAYS,
  NO_CONTROL_CHARACTERS,
  ORGANIZATION_MARK_SAYS,
  STORAGE_KIND_SAYS
} from '../config/deskConfig'
import { Field } from '../ui/Field'
import { Input } from '../ui/Input'
import { TextArea } from '../ui/TextArea'
import { CardField } from './SourceCard'
import { ProjectFileForm, problemAt, useProjectFileDraft } from './ProjectFileForm'
import type { MemberEdit } from './useProjectFileSave'

/**
 * A text field's value as the file takes it: the string, or `null` for none.
 *
 * `organization.name` and `organization.mark` are each "a value or null", and a
 * text input has no null. Blank means none, which is the reading a reader who
 * cleared the field intends — and the decoder refuses `""` by name, so the
 * alternative is a form whose only way to say "none" is refused.
 */
function orNull(value: string): string | null {
  return value.trim() === '' ? null : value
}

interface OrganizationDraft {
  name: string
  mark: string
}

export function OrganizationForm() {
  const { config } = useEffectiveConfig()
  const seed: OrganizationDraft = {
    name: config.organization.name ?? '',
    mark: config.organization.mark ?? ''
  }
  const state = useProjectFileDraft('/organization', seed, (draft, from) => {
    const edits: MemberEdit[] = []
    if (draft.name !== from.name) edits.push({ path: ['name'], value: orNull(draft.name) })
    if (draft.mark !== from.mark) edits.push({ path: ['mark'], value: orNull(draft.mark) })
    return edits
  })
  const { draft, set, save } = state
  return (
    <ProjectFileForm state={state} placed={['organization.name', 'organization.mark']}>
      <Field
        label="Name"
        hint="Blank writes none, and the header then shows the desk's own name."
        error={problemAt(save, 'organization.name')}
      >
        {(wiring) => (
          <Input
            {...wiring}
            value={draft.name}
            spellCheck={false}
            onChange={(event) => set({ ...draft, name: event.target.value })}
          />
        )}
      </Field>
      <Field label="Mark" hint={ORGANIZATION_MARK_SAYS} error={problemAt(save, 'organization.mark')}>
        {(wiring) => (
          <TextArea
            {...wiring}
            value={draft.mark}
            rows={3}
            spellCheck={false}
            onChange={(event) => set({ ...draft, mark: event.target.value })}
          />
        )}
      </Field>
    </ProjectFileForm>
  )
}

/**
 * The one storage kind there is, as a value rather than as a control.
 *
 * **A Select with one option is a control that cannot be operated.** It looks
 * like a choice, reads like a choice to anything that enumerates the page's
 * controls, and offers none: `storage.packs.kind` admits `"filesystem"` and
 * refuses everything else by name. So while there is one kind this is what the
 * file says, with the decoder's own sentence about the two that are not
 * available yet under it — and the refusal of any other kind is unchanged and
 * still tested.
 *
 * The day a second kind exists this is a `Select` again, with the same hint and
 * the member back in the Storage form's draft.
 */
export function StorageKind() {
  const { config } = useEffectiveConfig()
  return (
    <CardField label="Kind" rule={STORAGE_KIND_SAYS}>
      <code>{config.storage.packs.kind}</code>
    </CardField>
  )
}

interface StorageDraft {
  dir: string
  idBase: string
}

export function StorageForm({ dirSays }: { dirSays: string }) {
  const { config } = useEffectiveConfig()
  const packs = config.storage.packs
  const seed: StorageDraft = { dir: packs.dir, idBase: packs.idBase }
  const state = useProjectFileDraft('/storage', seed, (draft, from) => {
    const edits: MemberEdit[] = []
    if (draft.dir !== from.dir) edits.push({ path: ['packs', 'dir'], value: draft.dir })
    if (draft.idBase !== from.idBase) {
      edits.push({ path: ['packs', 'idBase'], value: draft.idBase })
    }
    return edits
  })
  const { draft, set, save } = state
  return (
    // `storage.packs.kind` is deliberately not placed: this form has no field
    // for it any more, and a problem with the file a save would have made is
    // still a problem — placing a key beside a field that is not there would
    // drop the sentence rather than render it whole.
    <ProjectFileForm state={state} placed={['storage.packs.dir', 'storage.packs.idBase']}>
      {/* The hint is what the file listing established about this location, and
          it is the listing's sentence rather than a rule: the decoder's rules
          for this member are several and specific, and each names itself when
          it is the one that is broken. */}
      <Field
        label="Packs go to"
        hint={`${dirSays} — ${NO_CONTROL_CHARACTERS}`}
        error={problemAt(save, 'storage.packs.dir')}
      >
        {(wiring) => (
          <Input
            {...wiring}
            value={draft.dir}
            spellCheck={false}
            onChange={(event) => set({ ...draft, dir: event.target.value })}
          />
        )}
      </Field>
      <Field
        label="Id prefix"
        hint={`${ID_BASE_SAYS} — ${NO_CONTROL_CHARACTERS}. ${ID_BASE_NORMALISES}`}
        error={problemAt(save, 'storage.packs.idBase')}
      >
        {(wiring) => (
          <Input
            {...wiring}
            value={draft.idBase}
            spellCheck={false}
            onChange={(event) => set({ ...draft, idBase: event.target.value })}
          />
        )}
      </Field>
    </ProjectFileForm>
  )
}
