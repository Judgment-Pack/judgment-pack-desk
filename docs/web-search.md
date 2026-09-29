# Web search in Chat

Open **Admin → Storage & data → Web search**, add a named connection, save it,
then select it as this desk's default. Existing connections can be opened in the
resizable detail pane to edit, test or remove them. Removal requires typing the
connection name. Editing credentials uses the same dirty-state and navigation
confirmation as other settings. Leaving a saved credential blank preserves it.

The initial providers are Tavily (API key) and Google Cloud Search grounding
(project, location, model and service-account JSON). The Google service account
must have model access in the configured billing-enabled project. This is
separate from Google Drive authorization. **Test connection** makes one real,
metered request; configuring or selecting a connection does not make a search.

The desk file `jpack-search.json` stores only the default connection ID and
`auto`/`provided` policy. Credentials live in Gateway's private custody directory,
not in that file, chats, pack files or chat backups. Connections are shared by
local desks; the selected default belongs to each desk. Save conflicts require
reload rather than overwriting another window's settings.

## Conversation behavior

In Chat and Research, **Assistant settings → Research** offers:

- **Desk default**: follow this desk's policy and selected connection.
- **Auto**: the assistant may search for current information, verification or
  research, read supplied/discovered pages, and explore a relevant website.
- **Provided sources only**: read sources already supplied in the conversation;
  do not discover new sources using search or website exploration.

The search connection can also be selected for one conversation. This does not
change the assistant model. In **Chat**, Auto lets the assistant choose available web tools from the request.
Selecting **Research** explicitly asks it to research substantive questions using
those same tools and verified sources. Both modes honor Provided sources only.
Neither requires the old `research.sources.search/read` fields or automatically
establishes a test suite. Pack proposals still receive runtime structure checks;
research and behavioral test results are reported separately. Stable explanations
and greetings do not require browsing.
An explicit request not to browse remains part of the assistant's instructions.

Without a search connection, supplied URLs can still be read. In Auto, a supplied
website can still be explored through Gateway's existing crawler. The assistant
must describe the actual unavailable capability rather than claiming that all
navigation beyond user-pasted URLs is prohibited.

## Sources and limits

Search leads have a separate signed manifest. Desk verifies its sealed receipt,
gateway key, exact arguments, connection revision and retained result digest
before accepting URLs for reading or discovery. Provider changes during a search
reject the late response. Provider errors are returned to the assistant as failed
tool results; they never become successful research claims.

Search records remain attached to their assistant response through reload and
chat backup/restore. Open **Sources → Web search** to inspect the query, provider,
time and links. Actual page reads create the existing verified documents and
citations. Google-generated text is labeled separately; returned provider
attribution is rendered in an isolated, script-free frame beside the response.
It is never inserted as trusted Desk HTML or treated as a fetched excerpt.

Each message permits at most three search attempts, one website exploration and
three ordinary new page reads (up to ten for discovered website pages). A chat
retains at most 64 search manifests. Gateway additionally enforces each
connection's durable daily request limit, reset at midnight UTC. Failed attempts
and connection tests count. This bounds requests, not a cloud billing amount.
There is no silent retry or fallback to another provider.

This first configuration UI requires the updated managed local Gateway. Existing
source-led Research runs and the dedicated research-authoring page continue to
use their declared provider dialects and source-grounded case validation. They are
not migrated or granted weaker readiness checks. An unsubmitted old Research
selection upgrades to the current web Research mode on reload.
The new settings do not silently override an external gateway. New providers are
added behind Gateway's versioned search contract; the browser never holds a
provider API endpoint implementation.
