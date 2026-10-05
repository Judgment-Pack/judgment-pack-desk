/**
 * Help & About's account of the gates (ADR-0009, section 6): what each one
 * holds, what it does not, whom `requireReviewed` binds, and how to give an
 * outside agent this project's tools without the access that would unbind it
 * (question 5's answer: documented, not hosted).
 *
 * It states the runtime's rules and this installation's one setting. It reads
 * nothing about this project's own `jpack.json`: Admin → Project says whether
 * its gates are on, and Review and lock says what the runtime finds.
 *
 * Beside the records: which records are signed, with whose key, and whom a
 * signature binds (ADR-0010, section 1); and, only where Desk says this
 * desk's runtime inherits a JPACK_SIGNING_KEY (the startup desk), that it
 * does. It offers no key action: while that key is set, the runtime acts on
 * it, not on anything Desk keeps.
 */
import { Section } from '../components/primitives'
import { useEffectiveConfig } from '../config/DeskConfigProvider'
import { Message } from '../i18n/Message'
import { msg, useLocale } from '../i18n'
import { CodeBlock } from '../ui/CodeBlock'

const ALLOW_UNTESTED = '--runner-require-tested-releases=false'

/** A word a POSIX shell reads as itself. */
function shellWord(word: string): string {
  return /^[A-Za-z0-9_./=:@%+-]+$/.test(word) ? word : `'${word.replaceAll("'", "'\\''")}'`
}

/**
 * The command that starts this project's `jpack mcp` for an outside agent:
 * the runtime this Desk runs, reading this desk's own `jpack.json`. Where
 * Desk has not said either, the words stand in for them.
 */
export function outsideAgentCommand(projectDir: string | undefined, runtimeBin: string | undefined): string {
  const config = projectDir ? `${projectDir.replace(/\/+$/, '')}/jpack.json` : '/absolute/path/to/the/desk/jpack.json'
  return `JPACK_CONFIG=${shellWord(config)} ${shellWord(runtimeBin || 'jpack')} mcp`
}

export function GatesHelp() {
  useLocale()
  const { desk } = useEffectiveConfig()
  const chassis = desk?.chassis
  const requireTested = chassis?.jobs?.requireTestedReleases
  const flag = <code>{ALLOW_UNTESTED}</code>
  return <Section title={msg('Gates')}>
    <p className="quiet" id="gates"><Message text={"<0/> Where this project’s jpack.json sets requireReviewed, the runtime refuses a deciding run of a pack whose bytes, or whose configuration, differ from what jpack.lock.json pins. Review and lock, on Packs, shows what the runtime finds and locks the set you confirm. A lock records that you confirmed those exact files. It does not say a pack is right, or that anyone else looked at it."} slots={[<strong>{msg('Reviewed set.')}</strong>]} /></p>
    <p className="quiet"><Message text={"<0/> Where jpack.json declares an audit directory (.desk-private/audit in a desk Desk made), each completed deciding run adds one record there. Rehearsals, tests and refusals add none. The folder is private to this desk, never committed and in no backup. Anyone who can write this project can change a record."} slots={[<strong>{msg('Records.')}</strong>]} /></p>
    <p className="quiet"><Message text={"<0/> A desk Desk makes with runtime 0.26.0 or later names a signing key in its jpack.json, unless Desk cannot keep one, which its creation says. Desk keeps the key for that desk in its own configuration folder, outside the project, and every runtime that reads that jpack.json signs each record it adds, if it accepts the key. A signature binds an agent given only this desk’s jpack mcp, which cannot read the key. It binds nothing against you, who hold the key, or against an agent that can read your files. Desk keeps no key for any other project: its records are signed only where something else names a key."} slots={[<strong>{msg('Signatures.')}</strong>]} /></p>
    {chassis?.runtimeInheritsSigningKey === true && <p className="quiet">{msg('But JPACK_SIGNING_KEY is set where Desk was started. Where this project’s audit trail is chained, as it is by default, its runtime signs each record with the key it names, if it accepts that key. Desks Desk made do not inherit it.')}</p>}
    <p className="quiet"><Message text={"<0/> Where jpack.json sets requireComparableFacts, the runtime refuses any evaluation, rehearsals included, that reads a fact of a JSON type no comparison in the pack can match, and names the fact. Saved tests and Jobs are not refused."} slots={[<strong>{msg('Comparable facts.')}</strong>]} /></p>
    <p className="quiet">{requireTested === true
      ? <Message text={"<0/> This installation refuses a job from a release whose saved tests were not run. Start Desk with <1/> to allow it; that applies to every desk."} slots={[<strong>{msg('Tested releases.')}</strong>, flag]} />
      : requireTested === false
        ? <Message text={"<0/> This installation allows a job from a release whose saved tests were not run, because Desk was started with <1/>."} slots={[<strong>{msg('Tested releases.')}</strong>, flag]} />
        : <Message text={"<0/> An installation refuses a job from a release whose saved tests were not run, unless Desk was started with <1/>."} slots={[<strong>{msg('Tested releases.')}</strong>, flag]} />}</p>
    <p className="quiet"><Message text={"<0/> Every evaluation Desk makes, the assistant’s included, is a rehearsal: it is never refused for a draft, and never recorded. The gates hold other callers. Jobs run each release under Runner’s own lock."} slots={[<strong>{msg('In Desk itself.')}</strong>]} /></p>
    <p className="quiet"><Message text={"<0/> requireReviewed holds a caller that can neither choose the configuration a run reads nor edit it or the lock. You, in Desk, are not bound, and neither is an agent that can edit this folder or run commands in it: it can change a pack and lock again."} slots={[<strong>{msg('Who is bound.')}</strong>]} /></p>
    <p className="quiet"><Message text={"<0/> To give an agent this project’s tools without that access, start jpack mcp for it with JPACK_CONFIG naming this desk’s jpack.json, and give the agent no file or shell tools:"} slots={[<strong>{msg('An outside agent.')}</strong>]} /></p>
    <CodeBlock text={outsideAgentCommand(chassis?.projectDir, chassis?.runtimeBin)} label={msg('Shell command')} />
    <p className="quiet">{msg('This holds only as far as the agent’s client really withholds those tools: the server runs as your user, and Desk does not host it.')}</p>
  </Section>
}
