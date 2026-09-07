/**
 * The fields on the four cards that write the project's own configuration
 * file, and nothing else about them.
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
 * **The values here can be written and the ones beside them cannot.** Panes
 * keeps its configured-and-rendered rows: those report what is on screen
 * against what the file asked for, which is a different fact from the number a
 * reader is about to write, and collapsing the two is how an accepted 720px
 * Inspector was reported as 720px while rendering 440px.
 */
import { useEffectiveConfig } from '../config/DeskConfigProvider'
import {
  ID_BASE_NORMALISES,
  ID_BASE_SAYS,
  NO_CONTROL_CHARACTERS,
  ORGANIZATION_MARK_SAYS,
  PANE_BOUNDS,
  STORAGE_KIND_SAYS,
  type Density,
  type ThemeChoice
} from '../config/deskConfig'
import { Field } from '../ui/Field'
import { Input } from '../ui/Input'
import { Select } from '../ui/Select'
import { TextArea } from '../ui/TextArea'
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

/**
 * A number field's value as the file takes it.
 *
 * A number where it is one, and **the text itself where it is not** — never a
 * repaired value and never a silently skipped edit. `NaN` written as a number
 * would reach the decoder as `null` and be refused for being the wrong thing;
 * the bytes the reader typed are refused for being what they are, at the field
 * they are in.
 */
function orText(value: string): number | string {
  const trimmed = value.trim()
  const number = Number(trimmed)
  return trimmed !== '' && Number.isFinite(number) ? number : value
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
 * The two appearance choices, as the decoder's own unions.
 *
 * The value is the label, as it is for the engine and the tier: these are the
 * words the file carries, and a second vocabulary on the page would be a name
 * for a setting that is not the name in the file somebody has to repair.
 */
const THEME_OPTIONS = [
  { value: 'system', label: 'system' },
  { value: 'light', label: 'light' },
  { value: 'dark', label: 'dark' }
] as const
const DENSITY_OPTIONS = [
  { value: 'comfortable', label: 'comfortable' },
  { value: 'compact', label: 'compact' }
] as const

interface AppearanceDraft {
  theme: ThemeChoice
  density: Density
}

export function AppearanceForm() {
  const { config } = useEffectiveConfig()
  const seed: AppearanceDraft = {
    theme: config.appearance.theme,
    density: config.appearance.density
  }
  const state = useProjectFileDraft('/appearance', seed, (draft, from) => {
    const edits: MemberEdit[] = []
    if (draft.theme !== from.theme) edits.push({ path: ['theme'], value: draft.theme })
    if (draft.density !== from.density) edits.push({ path: ['density'], value: draft.density })
    return edits
  })
  const { draft, set, save } = state
  return (
    <ProjectFileForm state={state} placed={['appearance.theme', 'appearance.density']}>
      <Field
        label="Theme"
        hint="Applied. The palette it selects is the light one."
        error={problemAt(save, 'appearance.theme')}
      >
        {(wiring) => (
          <Select
            {...wiring}
            value={draft.theme}
            onValueChange={(value) => set({ ...draft, theme: value as ThemeChoice })}
            options={THEME_OPTIONS}
          />
        )}
      </Field>
      <Field
        label="Density"
        hint="Accepted and read by nothing yet."
        error={problemAt(save, 'appearance.density')}
      >
        {(wiring) => (
          <Select
            {...wiring}
            value={draft.density}
            onValueChange={(value) => set({ ...draft, density: value as Density })}
            options={DENSITY_OPTIONS}
          />
        )}
      </Field>
    </ProjectFileForm>
  )
}

/** The three bounded dimensions, each with the label its card prints. */
const DIMENSIONS = [
  { key: 'panes.left.width', label: 'Rail width', path: ['left', 'width'] },
  { key: 'panes.inspector.width', label: 'Inspector width', path: ['inspector', 'width'] },
  { key: 'panes.console.height', label: 'Console height', path: ['console', 'height'] }
] as const

type PanesDraft = Record<string, string>

export function PanesForm() {
  const { config } = useEffectiveConfig()
  const seed: PanesDraft = {
    'panes.left.width': String(config.panes.left.width),
    'panes.inspector.width': String(config.panes.inspector.width),
    'panes.console.height': String(config.panes.console.height)
  }
  const state = useProjectFileDraft('/panes', seed, (draft, from) =>
    DIMENSIONS.filter((dimension) => draft[dimension.key] !== from[dimension.key]).map(
      (dimension) => ({ path: dimension.path, value: orText(draft[dimension.key] ?? '') })
    )
  )
  const { draft, set, save } = state
  return (
    <ProjectFileForm state={state} placed={DIMENSIONS.map((dimension) => dimension.key)}>
      {DIMENSIONS.map((dimension) => (
        <Field
          key={dimension.key}
          label={dimension.label}
          error={problemAt(save, dimension.key)}
        >
          {(wiring) => (
            <Input
              {...wiring}
              type="number"
              // The decoder's own bounds, so the control and the refusal cannot
              // disagree about what is accepted.
              min={PANE_BOUNDS[dimension.key]!.min}
              max={PANE_BOUNDS[dimension.key]!.max}
              step={1}
              value={draft[dimension.key] ?? ''}
              onChange={(event) => set({ ...draft, [dimension.key]: event.target.value })}
            />
          )}
        </Field>
      ))}
    </ProjectFileForm>
  )
}

const KIND_OPTIONS = [{ value: 'filesystem', label: 'filesystem' }] as const

interface StorageDraft {
  kind: string
  dir: string
  idBase: string
}

export function StorageForm({ dirSays }: { dirSays: string }) {
  const { config } = useEffectiveConfig()
  const packs = config.storage.packs
  const seed: StorageDraft = { kind: packs.kind, dir: packs.dir, idBase: packs.idBase }
  const state = useProjectFileDraft('/storage', seed, (draft, from) => {
    const edits: MemberEdit[] = []
    if (draft.kind !== from.kind) edits.push({ path: ['packs', 'kind'], value: draft.kind })
    if (draft.dir !== from.dir) edits.push({ path: ['packs', 'dir'], value: draft.dir })
    if (draft.idBase !== from.idBase) {
      edits.push({ path: ['packs', 'idBase'], value: draft.idBase })
    }
    return edits
  })
  const { draft, set, save } = state
  return (
    <ProjectFileForm
      state={state}
      placed={['storage.packs.kind', 'storage.packs.dir', 'storage.packs.idBase']}
    >
      <Field label="Kind" hint={STORAGE_KIND_SAYS} error={problemAt(save, 'storage.packs.kind')}>
        {(wiring) => (
          <Select
            {...wiring}
            value={draft.kind}
            onValueChange={(value) => set({ ...draft, kind: value })}
            options={KIND_OPTIONS}
          />
        )}
      </Field>
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
