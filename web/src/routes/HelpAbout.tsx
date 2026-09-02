/**
 * Help & About: what this desk is, what it is connected to, and the runtime's
 * own authoring guidance.
 *
 * **The desk holds no model key, calls no model and executes no prompt.**
 * Where the runtime advertises `author_pack`, this page renders that prompt's
 * text verbatim for a person to carry to whatever agent they run —
 * `prompts.go` is explicit that the client's model executes it with the
 * client's key, and this desk is not that client. Nothing on this page runs.
 */
import { Json, Section } from '../components/primitives'
import { useMcp } from '../mcp/McpProvider'
import { AUTHOR_PACK_PROMPT, usePromptNames, usePromptText } from '../mcp/prompts'
import { usePacks } from '../mcp/queries'
import { TOKEN_SENTENCE } from '../identity/UserControl'
import { SHORTCUTS } from '../shell/shortcuts'

const REPO = 'https://github.com/Judgment-Pack/judgment-pack-desk'

export function HelpAbout() {
  const { server, known, rehearsalSupported, graphDocumentSupported, graphInventorySupported, graphTracesSupported, exampleSupported, schemaSupported } = useMcp()
  const { data } = usePacks()
  const prompts = usePromptNames()
  const advertised = (prompts.data ?? []).includes(AUTHOR_PACK_PROMPT)
  const authorPack = usePromptText(AUTHOR_PACK_PROMPT, advertised)

  return (
    <article className="detail">
      <header className="detail-head">
        <h1>Help &amp; About</h1>
        <p className="quiet">
          A local web desk for a Judgment Pack project. The browser is the MCP client; the Go
          program is a chassis with no per-feature endpoints that parses none of the traffic it
          carries.
        </p>
      </header>

      <Section title="This connection">
        <p>
          Runtime:{' '}
          {server ? (
            <>
              <code>{server.name}</code> {server.version}
            </>
          ) : (
            'not connected'
          )}
          <br />
          Tool listing: {known ? 'read' : 'not read — every capability below is unknown, not absent'}
          <br />
          {data?.configPath && (
            <>
              Project configuration: <code>{data.configPath}</code>
              <br />
            </>
          )}
        </p>
        <Json
          label="What this runtime advertises"
          value={{
            rehearsalSupported,
            graphDocumentSupported,
            graphInventorySupported,
            graphTracesSupported,
            exampleSupported,
            schemaSupported
          }}
        />
        <p className="quiet">
          Where a pack's evaluation reports a <code>conformanceClaimReference</code>, the desk
          renders it as what it is — a locator for the file that states the runtime's claim — and
          not as a claim the payload itself makes.
        </p>
      </Section>

      <Section title="Keyboard shortcuts">
        <ul id="shortcuts">
          {SHORTCUTS.map((shortcut) => (
            <li key={shortcut.keys}>
              <code>{shortcut.keys}</code> — {shortcut.label}
            </li>
          ))}
        </ul>
        <p className="quiet">
          <code>Mod</code> is Ctrl or Cmd. On macOS the browser claims Cmd+Alt+I and Cmd+Alt+J for
          its own developer tools before the page sees them, and Cmd+B is Firefox's bookmarks
          sidebar — use the Ctrl spelling there, or the buttons. Every shortcut has a visible
          button, so a chord the browser eats costs a click and not a feature. Shortcuts are
          suppressed while you are typing in a field or in the authoring editor.
        </p>
        <p className="quiet">
          A pane is not a dialog, so <code>Escape</code> does not close one — with one exception,
          stated rather than hidden: below 1100px the Inspector is rendered as a drawer, and a
          drawer <em>is</em> a dialog, so Escape closes it there.
        </p>
      </Section>

      <Section title="Authoring method">
        <p className="quiet">
          The runtime carries this guidance; the desk renders it and stops. <strong>The desk
          holds no model key, calls no model, and executes no prompt.</strong> Copy it into
          whatever agent you run.
        </p>
        {!advertised ? (
          <p className="empty" id="authoring-method">
            This runtime advertises no <code>{AUTHOR_PACK_PROMPT}</code> prompt.
          </p>
        ) : authorPack.error ? (
          <p className="note note-warn" role="status">
            The prompt could not be read — {authorPack.error.message}
          </p>
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
          <p className="loading">Loading the runtime's authoring prompt…</p>
        )}
      </Section>

      <Section title="Security">
        <p className="quiet">{TOKEN_SENTENCE}</p>
        <p className="quiet">
          The desk is authorized by three things and not by who you are: the loopback bind, that
          session token, and an origin check on every relay and file-API request. A configured
          identity provider changes what the header displays and nothing about who may reach the
          desk.
        </p>
      </Section>

      <Section title="Where to read more">
        <ul>
          <li>
            <a href={REPO}>{REPO}</a> — the desk, its README and its layout
          </li>
          <li>
            <a href={`${REPO}/blob/main/README.md`}>README</a> — the security model, the file API,
            and how the relay works
          </li>
          <li>
            <a href="https://github.com/Judgment-Pack/judgment-pack-runtime">
              judgment-pack-runtime
            </a>{' '}
            — the evaluator, its ADRs, and the conformance claim this desk only ever renders
          </li>
        </ul>
      </Section>
    </article>
  )
}
