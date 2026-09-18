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
    if (node.type === 'CallExpression' && ['msg', 'sourceMessage'].includes(node.callee.name)) argument = node.arguments[0]
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
// These messages are selected from durable state rather than passed as literals.
for (const text of ['Copied', 'Select the response text to copy it.']) if (!messages.has(text)) messages.set(text, ['chat/MessageRenderer.tsx'])
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
  console.log(`${file}: ${sourceKeys.length - missing.length}/${sourceKeys.length} translated; ${invalid.length} placeholder errors`)
  if (missing.length || invalid.length) issues.push(`${file}: ${missing.length} missing, ${invalid.length} invalid`)
}
if (issues.length) { console.error(issues.join('\n')); process.exitCode = 1 }
