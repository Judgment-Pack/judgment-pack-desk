/**
 * One line per Admin section, from the **decoded** configuration and nothing
 * else.
 *
 * The navigation column is a list, and a list row that said only its title
 * would be a menu: what an administrator is there to find out is what each
 * setting currently *is*. So each row carries one summary, and every summary is
 * drawn from a closed vocabulary — the decoded value, a member of a union the
 * decoder itself admits, or the one word this desk uses for "nothing is
 * configured".
 *
 * **Nothing here is composed.** A summary that fell back to the project's
 * directory name for an unset organization, or spelled `filesystem` as a
 * literal beside a `kind` the decoder had answered, would be the page asserting
 * a value nobody wrote — and a reader would have no way to tell it from one
 * that is in the file. Where the file says nothing the row says so, in one
 * word, and that word is the whole of the claim.
 *
 * It is a map keyed by section id rather than a `switch`, so a section added to
 * `adminSections.ts` without a summary is a missing key a test can name rather
 * than a row that silently renders nothing.
 */
import type { EffectiveConfig } from '../config/deskConfig'

/** The separator between the parts of a summary that has more than one. */
const JOIN = ' · '

/**
 * The three things the Project row may say, and there is no fourth.
 *
 * "The desk has not said" is **not** a spelling of "not the default project".
 * The comparison is against the path the chassis resolved, so before
 * `/api/desk-config` has answered — and for ever where it carries no chassis
 * facts — this page does not know which of the other two is true, and a row
 * that picked one would be answering a question nobody has answered.
 */
const IS_DEFAULT = 'the default project'
const NOT_DEFAULT = 'not the default project'
const NOT_SAID = 'the desk has not said'

/**
 * The summaries take the **effective** configuration rather than the decoded
 * file alone: the Project row is about the file itself, and whether the
 * configured default is *this* project is a comparison against the path the
 * chassis reported, which is not a member of any file.
 */
export const SECTION_SUMMARY: Record<
  string,
  ((effective: EffectiveConfig) => string) | undefined
> = {
  // **This project, as the chassis spells it** — the same comparison the
  // control under it makes, because that is the value the chassis will accept
  // and anything this page normalised would be a second spelling rule.
  project: ({ config, desk }) => {
    const chassis = desk?.chassis
    if (chassis === undefined) return NOT_SAID
    return config.project.file === chassis.projectFile ? IS_DEFAULT : NOT_DEFAULT
  },
  organization: ({ config }) => config.organization.name ?? 'none',
  // The kind is the decoder's answer and not the word `filesystem` written
  // here: the union has one member today and this row must say what the file
  // says on the day it has two.
  storage: ({ config }) => [config.storage.packs.kind, config.storage.packs.dir].join(JOIN),
  assistant: ({ config }) => {
    const endpoint = config.assistant.endpoint
    if (endpoint === null) return 'none'
    // The wire, the model and the tier — the three the assistant slot actually
    // has. The URL is deliberately not here: it is the one part of the endpoint
    // that is long, and it is in the file the pane is showing.
    return [endpoint.kind, endpoint.model, `thinking ${config.assistant.thinking}`].join(JOIN)
  },
  'identity-provider': ({ config }) =>
    config.identity.provider === null ? 'None' : config.identity.provider.issuer
}
