import { Message } from '../i18n/Message'
import { msg, useLocale } from '../i18n'
/**
 * Help & About: what this desk is, what it is connected to, and the runtime's
 * own authoring guidance.
 *
 * **The desk holds no model key, calls no model and executes no prompt.**
 * Where the runtime advertises `author_pack`, this page renders that prompt's
 * text verbatim for a person to carry to whatever agent they run —
 * `prompts.go` is explicit that the client's model executes it with the
 * client's key, and this desk is not that client. Nothing on this page runs.
 *
 * **This is where the Runtime card's content went.** Admin used to carry a
 * card whose four slots reported the binary the chassis was launched with, the
 * connection, whether the tool listing answered and what it said — none of
 * them a setting, and all of them the answer to "what am I connected to?",
 * which is the question this page exists for. Admin keeps a status line; the
 * facts live here.
 */
import { Json, Section } from '../components/primitives'
import { useEffectiveConfig } from '../config/DeskConfigProvider'
import { connectionSays, useMcp } from '../mcp/McpProvider'
import { AUTHOR_PACK_PROMPT, usePromptNames, usePromptText } from '../mcp/prompts'
import { usePacks } from '../mcp/queries'
import { SESSION_SENTENCE } from '../identity/UserControl'
import { SHORTCUTS } from '../shell/shortcuts'
import { useHashTarget } from '../shell/useHashTarget'

const REPO = 'https://github.com/Judgment-Pack/judgment-pack-desk'

export function HelpAbout() {
  useLocale()
  const mcp = useMcp()
  const { status, server, known } = mcp
  const { desk } = useEffectiveConfig()
  const { data } = usePacks()
  const prompts = usePromptNames()
  const advertised = (prompts.data ?? []).includes(AUTHOR_PACK_PROMPT)
  const authorPack = usePromptText(AUTHOR_PACK_PROMPT, advertised)
  // `#shortcuts` and `#authoring-method` are linked from the rail, the user
  // menu and the Create-pack dialog; nothing else scrolls to a fragment.
  useHashTarget()

  return (
    <article className="detail" data-measure="form">
      <header className="detail-head">
        <h1>{msg("Help & About")}</h1>
        <p className="quiet">{msg("A local web desk for a Judgment Pack project. The browser is the MCP client; the Go program is a chassis with no per-feature endpoints that parses none of the traffic it carries.")}</p>
      </header>

      <Section title={msg("This connection")}>
        <p><Message text={"Runtime:<0/><1/><2/>Runtime binary:<3/><4/><5/>Tool listing: <6/><7/><8/>"} slots={[' ', status === 'ready' && server ? (
            <>
              <code>{server.name}</code> {server.version}
            </>
          ) : (
            connectionSays(status)
          ), <br />, ' ', desk?.chassis === undefined ? (
            msg("the desk has not said")
          ) : (
            <code>{desk.chassis.runtimeBin}</code>
          ), <br />, known ? msg("read") : msg("not read — every capability below is unknown, not absent"), <br />, data?.configPath && (
            <><Message text={"Project configuration: <0/><1/>"} slots={[<code>{data.configPath}</code>, <br />]} /></>
          )]} /></p>
        <details className="disclosure">
          <summary>{msg("Connection capabilities")}</summary>
          <Json label={msg("This connection, and what this runtime advertises")} value={connectionSummary(mcp)} />
        </details>
        <p className="quiet"><Message text={"Where a pack's evaluation reports a <0/>, the desk renders it as what it is — a locator for the file that states the runtime's claim — and not as a claim the payload itself makes."} slots={[<code>conformanceClaimReference</code>]} /></p>
      </Section>

      <Section title={msg("Keyboard shortcuts")}>
        <ul id="shortcuts">
          {SHORTCUTS.map((shortcut) => (
            <li key={shortcut.keys}>
              <code>{shortcut.keys}</code> — {msg(shortcut.label)}
            </li>
          ))}
        </ul>
        <p className="quiet"><Message text={"<0/> is Ctrl or Cmd. On macOS the browser claims Cmd+Alt+I and Cmd+Alt+J for its own developer tools before the page sees them, and Cmd+B is Firefox's bookmarks sidebar — use the Ctrl spelling there, or the buttons. Every shortcut has a visible button, so a chord the browser eats costs a click and not a feature. Shortcuts are suppressed while you are typing in a field or in the authoring editor."} slots={[<code>Mod</code>]} /></p>
        <p className="quiet"><Message text={"Below 900px the rail is an overlay drawer and draws no collapse toggle of its own, so its button moves to the header: <0/>, at the left, next to the organization mark. A control inside a closed drawer opens nothing."} slots={[<strong>{msg("Project navigation")}</strong>]} /></p>
        <p className="quiet"><Message text={"<0/> closes the Inspector when it appears as a drawer below 1100px. It does not close a regular pane."} slots={[<code>Escape</code>]} /></p>
      </Section>

      <Section title={msg("Authoring method")}>
        <p className="quiet"><Message text={"The runtime carries this guidance; the desk renders it and stops. <0/> Copy it into whatever agent you run."} slots={[<strong>{msg("The desk holds no model key, calls no model, and executes no prompt.")}</strong>]} /></p>
        {!advertised ? (
          <p className="empty" id="authoring-method"><Message text={"This runtime advertises no <0/> prompt."} slots={[<code>{AUTHOR_PACK_PROMPT}</code>]} /></p>
        ) : authorPack.error ? (
          <p className="note note-warn" role="status" id="authoring-method"><Message text={"The prompt could not be read — <0/>"} slots={[authorPack.error.message]} /></p>
        ) : authorPack.data ? (
          <figure className="json" id="authoring-method">
            <figcaption>
              {AUTHOR_PACK_PROMPT}
              {authorPack.data.description ? ` — ${authorPack.data.description}` : ''}
            </figcaption>
            <pre>
              <code>{authorPack.data.text}</code>
            </pre>
          </figure>
        ) : (
          <p className="loading" id="authoring-method">{msg("Loading the runtime's authoring prompt…")}</p>
        )}
      </Section>

      <Section title={msg("Security")}>
        <p className="quiet" id="security">{SESSION_SENTENCE}</p>
        <p className="quiet">{msg("The desk is authorized by three things and not by who you are: the loopback bind, that session, and an origin check on every relay and file-API request. A configured identity provider changes what the header displays and nothing about who may reach the desk.")}</p>
      </Section>

      <Section title={msg("Where to read more")}>
        <ul>
          <li><Message text={"<0/> — the desk, its README and its layout"} slots={[<a href={REPO}>{REPO}</a>]} /></li>
          <li><Message text={"<0/> — the security model, the file API, and how the relay works"} slots={[<a href={`${REPO}/blob/main/README.md`}>{msg("README")}</a>]} /></li>
          <li><Message text={"<0/><1/>— the evaluator, its ADRs, and the conformance claim this desk only ever renders"} slots={[<a href="https://github.com/Judgment-Pack/judgment-pack-runtime">{msg("judgment-pack-runtime")}</a>, ' ']} /></li>
        </ul>
      </Section>
    </article>
  )
}

/**
 * What this page knows about the runtime it is connected to.
 *
 * **Moved from Admin's Runtime card, not rewritten.** The connection's own
 * summary — who answered `initialize`, whether the tool listing was read, and
 * what that listing said this runtime can do. `known` is carried rather than
 * folded in: a listing that never answered leaves every flag *unknown* rather
 * than absent, and a page that printed them as false would impersonate an
 * older runtime.
 *
 * The two errors are reduced to their messages, because an `Error` serialises
 * to `{}` and a disclosure showing an empty object where a socket failed would
 * be worse than showing nothing at all.
 */
function connectionSummary(mcp: ReturnType<typeof useMcp>) {
  const { client, retryNow, error, capabilitiesError, ...summary } = mcp
  void client
  void retryNow
  return {
    ...summary,
    error: error === null ? null : error.message,
    capabilitiesError: capabilitiesError === null ? null : capabilitiesError.message
  }
}
