/** Extract authored messages and validate every locale without evaluating application code. */
import fs from 'node:fs'
import path from 'node:path'
import { fileURLToPath } from 'node:url'
import { parse } from '@babel/parser'
import { untranslatedUiMessages } from './ui-message-audit.mjs'
const sourceRoot = fileURLToPath(new URL('../src/', import.meta.url))
const localeRoot = path.join(sourceRoot, 'i18n/locales')
const messages = new Map()
const dynamic = []
const untranslated = []
const walk = (node, visit) => {
  visit(node)
  for (const [key, value] of Object.entries(node)) {
    if (['loc', 'comments', 'leadingComments', 'trailingComments', 'innerComments'].includes(key)) continue
    for (const child of Array.isArray(value) ? value : [value]) if (child?.type) walk(child, visit)
  }
}
function inspect(file) {
  const text = fs.readFileSync(file, 'utf8')
  for (const issue of untranslatedUiMessages(text)) untranslated.push(`${path.relative(sourceRoot, file)}:${issue.line}: ${issue.text}`)
  const ast = parse(text, { sourceType: 'module', plugins: ['typescript', 'jsx'] })
  const constants = new Map()
  walk(ast, node => { if (node.type === 'VariableDeclarator' && node.id.type === 'Identifier') constants.set(node.id.name, node.init) })
  function value(node, seen = new Set()) {
    if (node?.type === 'StringLiteral') return node.value
    if (node?.type === 'CallExpression' && node.callee.name === 'sourceMessage') return value(node.arguments[0], seen)
    if (node?.type === 'JSXExpressionContainer') return value(node.expression, seen)
    if (node?.type === 'Identifier' && !seen.has(node.name)) return value(constants.get(node.name), new Set([...seen, node.name]))
    if (node?.type === 'BinaryExpression' && node.operator === '+') {
      const left = value(node.left, seen), right = value(node.right, seen)
      if (left !== undefined && right !== undefined) return left + right
    }
  }
  walk(ast, node => {
    let argument
    if (node.type === 'CallExpression' && ['msg', 'sourceMessage', 'toolInputFailure'].includes(node.callee.name)) argument = node.arguments[0]
    if (node.type === 'CallExpression' && node.callee.name === 'authoredFailure') argument = node.arguments[1]
    if (node.type === 'JSXOpeningElement' && node.name.name === 'Message') argument = node.attributes.find(a => a.name?.name === 'text')?.value
    if (!argument) return
    const key = value(argument)
    if (key === undefined) dynamic.push(`${path.relative(sourceRoot, file)}:${node.loc.start.line}`)
    else if (key) messages.set(key, [...(messages.get(key) ?? []), path.relative(sourceRoot, file)])
  })
}
function scan(dir) {
  for (const entry of fs.readdirSync(dir, { withFileTypes: true })) {
    const file = path.join(dir, entry.name)
    if (entry.isDirectory()) { if (!['testing', '__fixtures__', 'conformance', 'design'].includes(entry.name)) scan(file) }
    else if (/\.tsx?$/.test(entry.name) && !entry.name.includes('.test.') && !file.endsWith('i18n/index.ts')) inspect(file)
  }
}
scan(sourceRoot)
// The assistant harness authors its messages in the package the page bundles
// (@judgment-pack/harness) and the Desk shows them translated, so they are read
// from its emitted modules as the Desk's own are read from src/.
function scanHarness(dir) {
  for (const entry of fs.readdirSync(dir, { withFileTypes: true })) {
    const file = path.join(dir, entry.name)
    if (entry.isDirectory()) { if (entry.name !== 'certification') scanHarness(file) }
    else if (entry.name.endsWith('.js')) inspect(file)
  }
}
scanHarness(fileURLToPath(new URL('../node_modules/@judgment-pack/harness/dist/', import.meta.url)))
// Shared Go/browser failure messages have one canonical catalogue.
for (const entry of Object.values(JSON.parse(fs.readFileSync(new URL('../../internal/aifailure/catalog.json', import.meta.url), 'utf8')).failures)) messages.set(entry.message, ['internal/aifailure/catalog.json'])
// These messages are selected from durable state rather than passed as literals.
for (const text of ['Copied', 'Select the response text to copy it.']) if (!messages.has(text)) messages.set(text, ['chat/MessageRenderer.tsx'])
// Retained checkpoints and authored note turns outlive their producing browser engine.
// These exact historical messages still render through systemMessage; extracting only
// current call sites would erase their translations. Setup UI and transient activity
// messages from the removed engine are intentionally excluded. The verification tuple
// remains the authored-prefix contract exercised by the language regression fixture.
const retainedAssistantMessages = [
  "A case with this id is already established; the correction was not applied.",
  "Approved the corrected expectation for {{value0}}: {{value1}}",
  "Asked for test cases written without the draft's rules.",
  "Checking revision {{value0}} through the runtime.",
  "Checking the proposal against the saved test suite.",
  "Choose an enabled model in Admin › Assistant.",
  "Completed work kept. Send a new instruction when ready.",
  "Configure Assistant before starting.",
  "Correcting the proposal against the saved tests.",
  "Establishing test cases from the sources.",
  "Establishing test cases without the draft's rules.",
  "Every established case agrees. Review the draft, its sources and the unknowns before creating the pack.",
  "Every test case written without the draft's rules agrees, and every citation is traced to a page read in this chat. Review the draft and the unknowns before creating the pack.",
  "Every test case written without the draft's rules agrees. This pack cites no source; it rests on what you told the assistant. Review the draft and the unknowns before creating the pack.",
  "No saved cases can be rechecked. Send a message to continue research.",
  "No test case could be grounded in a cited excerpt. Cite the requirements, or say what the cases should be.",
  "No test case could be grounded in your messages or in a page the draft cites. Tell the assistant how the pack should decide a case, or have it read and cite the pages the pack rests on.",
  "Proposed cases dropped: {{value0}}.",
  "Ready to continue",
  "Repairing the candidate against the established cases.",
  "Review the proposed expectation correction before approving it.",
  "Review the unresolved policy before continuing.",
  "Reviewing the expectation for {{value0}}.",
  "Running draft checks.",
  "Saved draft. Recheck its sources and tests before creating it.",
  "Saved test cases could not be grounded again. Start a new research chat; the original history is preserved.",
  "Sent the held case proposal back for validation.",
  "Some test cases failed. Review the results.",
  "Stopped. The last completed stage is kept.",
  "The assistant repeated an earlier candidate.",
  "The assistant repeated an earlier candidate. Review the disagreements, or send a message to steer it.",
  "The candidate needs review.",
  "The citation check was interrupted. Recheck the draft.",
  "The correction for {{value0}} was rolled back: its retest did not complete, so nothing was approved and the proposal is still on offer.",
  "The correction is no longer valid: {{value0}}",
  "The correction was not applied: {{value0}}",
  "The draft changed. Request a new correction before approving.",
  "The draft cites no source. A pack with no traced citation is an assumption; ask the assistant to cite what it read.",
  "The draft needs corrections before it can be created.",
  "The draft or test inputs changed. Run checks again.",
  "The expectation source is no longer verified. Restore its verification before reviewing the expectation.",
  "The expectation source is no longer verified; the correction was not applied.",
  "The expectation still needs a decision.",
  "The gateway pin or this chat's documents changed while the citations were checked. Recheck the draft.",
  "The last check did not finish. Run checks again.",
  "The reviewer did not explain a correction. The expectation remains blocked.",
  "The revision budget of {{value0}} is spent with disagreements remaining. Review the results, or send a message to continue.",
  "The runtime or source context changed during verification. Run checks again.",
  "The runtime version changed or cannot be verified. Run checks again.",
  "The saved test cases rest on messages or pages this conversation cannot ground again, so they were set aside. Establish new ones before creating the pack.",
  "The saved test suite changed during correction. Review the current results before continuing.",
  "The source context changed. Run checks again.",
  "The statements used by the tests changed. Run checks again.",
  "The test inputs changed during the checks. Run checks again.",
  "The test sources could not be verified. Run checks again.",
  "The time budget of {{value0}} seconds is spent. The last completed stage is kept.",
  "The turn's step budget was spent before a proposal was written; continuing ({{value0}} of {{value1}}).",
  "The unchanged draft disagrees with the reviewed expectations. Review the results or request a pack change.",
  "The unchanged draft is not a valid pack: {{count}} diagnostics. Review them, or send a message to repair it.",
  "This conversation exceeds the 200,000-character context limit. Start a new chat with the relevant text, or attach a smaller excerpt. Nothing was sent to the model.",
  "This response was interrupted. Send a message to continue; nothing restarted automatically.",
  "Validating the approved correction for {{value0}}.",
  "Validating the draft through the runtime.",
  "Validating the held case proposal again.",
  "Writing response…",
  "no candidate to check",
  "set aside with the saved cases that could not be grounded again",
  "the assistant ended without a proposal",
  "verify: {{value0}} — {{value1}} ({{value2}})",
  "{{count}} invalid expectations must be corrected and approved before testing or creating the pack.",
  "{{value0}} citation(s) in the draft could not be traced to a verified excerpt ({{value1}}). Ask the assistant to cite from what it read, or review the sources.",
  "{{value0}} cited source(s) failed receipt verification ({{value1}}), so their excerpts are withheld and the draft is not ready."
]
for (const text of retainedAssistantMessages) messages.set(text, ['retained assistant checkpoint / note contract'])
const enFile = path.join(localeRoot, 'en.json')
const plurals = JSON.parse(fs.readFileSync(path.join(sourceRoot, 'i18n/plurals.json'), 'utf8'))
for (const [other, one] of Object.entries(plurals)) {
  if (!messages.has(other)) throw new Error(`Unused plural message: ${other}`)
  messages.set(`${other}_one`, messages.get(other))
  messages.set(`${other}_other`, messages.get(other))
}
const prior = JSON.parse(fs.readFileSync(enFile, 'utf8'))
if (process.argv.includes('--extract')) {
  fs.writeFileSync(enFile, JSON.stringify(Object.fromEntries([...messages.keys()].sort().map(key => [key, key.endsWith('_one') ? plurals[key.slice(0, -4)] : key.endsWith('_other') ? key.slice(0, -6) : key])), null, 2) + '\n')
  console.log(`Extracted ${messages.size} messages. Dynamic call sites: ${dynamic.join(', ') || 'none'}`)
  process.exit(0)
}
const issues = []
const warnings = []
// A key missing from a translated catalogue falls back to English at run time, so it is a
// warning; the release's translation PR runs with --strict (or I18N_STRICT=1) to require it.
const strict = process.argv.includes('--strict') || process.env.I18N_STRICT === '1'
if (untranslated.length) issues.push(`Untranslated UI literals:\n${untranslated.join('\n')}`)
const tokens = value => ((value ?? '').match(/\{\{\w+\}\}|<\d+\/>/g) ?? []).sort().join('|')
const sourceKeys = [...messages.keys()]
const staleEnglish = Object.keys(prior).filter(key => !messages.has(key))
const missingEnglish = sourceKeys.filter(key => !Object.hasOwn(prior, key))
if (staleEnglish.length || missingEnglish.length) issues.push(`en: ${staleEnglish.length} unused, ${missingEnglish.length} unextracted messages (run npm run i18n:extract)`)
for (const file of fs.readdirSync(localeRoot).filter(file => file.endsWith('.json') && file !== 'en.json')) {
  const catalogue = JSON.parse(fs.readFileSync(path.join(localeRoot, file), 'utf8'))
  const missing = sourceKeys.filter(key => typeof catalogue[key] !== 'string' || !catalogue[key].trim())
  const language = file.replace('.json', '')
  const pluralKeys = []
  for (const base of Object.keys(plurals)) for (const category of new Intl.PluralRules(language).resolvedOptions().pluralCategories) {
    const key = `${base}_${category}`
    pluralKeys.push([key, base])
    if (!catalogue[key]?.trim() && !missing.includes(key)) missing.push(key)
  }
  const invalid = [...new Set([
    ...sourceKeys.filter(key => catalogue[key] && tokens(catalogue[key]) !== tokens(prior[key])),
    ...pluralKeys.filter(([key, base]) => catalogue[key] && tokens(catalogue[key]) !== tokens(base)).map(([key]) => key)
  ])]
  const known = new Set([...Object.keys(prior), ...pluralKeys.map(([key]) => key)])
  const unknown = Object.keys(catalogue).filter(key => !known.has(key))
  console.log(`${file}: ${sourceKeys.length - missing.length}/${sourceKeys.length} translated; ${invalid.length} placeholder errors`)
  if (missing.length) (strict ? issues : warnings).push(`${file}: ${missing.length} missing (English fallback): ${missing.join(' | ')}`)
  if (invalid.length || unknown.length) issues.push(`${file}: ${invalid.length} invalid, ${unknown.length} not in en.json${unknown.length ? `: ${unknown.join(' | ')}` : ''}`)
}
if (warnings.length) console.warn(`warning: ${warnings.join('\nwarning: ')}`)
if (issues.length) { console.error(issues.join('\n')); process.exitCode = 1 }
