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
 *
 * Beside those, the key this desk's Runner signs its runs with (ADR-0010,
 * section 5): its public key, or why its runs are not signed, as Desk
 * reported it in desk-config. Nothing where this desk has no Runner.
 */
import { Section } from '../components/primitives'
import { useEffectiveConfig } from '../config/DeskConfigProvider'
import type { RunnerKey, RunnerKeyReason } from '../config/deskConfig'
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

/**
 * Why this desk's Runner signs no run, for each reason the chassis names
 * (`internal/desk/runner_key.go`). The words of the check, the runtime or
 * Runner that say why are passed on as they were written.
 */
function runnerKeyReason(key: { reason: RunnerKeyReason; detail?: string }): string {
  const detail = key.detail ?? ''
  switch (key.reason) {
    case 'custody': return msg('Desk cannot keep a signing key for Runner here: {{detail}}.', { detail })
    case 'not-made': return msg('The runtime did not make Runner’s signing key: {{detail}}.', { detail })
    case 'unfinished': return msg('Making Runner’s signing key did not finish. Desk removes what was left at its next start, and makes the key again.')
    case 'lost': return msg('Desk keeps the public half of Runner’s signing key, but no longer the key itself, and does not make another in its place.')
    case 'not-read-now': return msg('Desk could not read Runner’s signing key just now, and left it as it is: {{detail}}.', { detail })
    case 'not-used': return msg('Desk does not name the signing key it keeps for Runner: {{detail}}.', { detail })
    case 'runtime-refused': return msg('The runtime refuses Runner’s signing key: {{detail}}.', { detail })
    case 'runner-refused': return msg('Runner refused its signing key when it started: {{detail}}.', { detail })
  }
}

/** The key this desk's Runner signs its runs with, or why it signs none. */
export function RunnerSignatures({ runnerKey }: { runnerKey: RunnerKey | undefined }) {
  useLocale()
  if (!runnerKey) return null
  const label = <strong>{msg('Jobs signatures.')}</strong>
  if (runnerKey.state === 'signed') return <>
    <p className="quiet" id="runner-signatures"><Message text={"<0/> Runner signs the record of each Jobs run on this desk with a key of its own, never this project’s. Desk keeps it in its own configuration folder, outside the project, and names it to Runner when Runner starts. A holder checks a run’s version-5 export with jpack-runner verify-run --public-key and this public key. A signature binds nothing against you, who hold the key, or against an agent that can read your files."} slots={[label]} /></p>
    <CodeBlock text={runnerKey.publicKey} label={msg('Runner’s public key, keyId {{keyId}}', { keyId: runnerKey.keyId })} />
  </>
  if (runnerKey.state === 'unsigned') return <p className="quiet" id="runner-signatures"><Message text={"<0/> Jobs runs on this desk are not signed, and run as before."} slots={[label]} /> {runnerKeyReason(runnerKey)}</p>
  return <p className="quiet" id="runner-signatures"><Message text={"<0/> Runner has not started on this desk yet, so Desk cannot say yet whether it signs this desk’s runs."} slots={[label]} /></p>
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
    <RunnerSignatures runnerKey={chassis?.runnerKey} />
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
