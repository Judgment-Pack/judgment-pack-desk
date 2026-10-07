import { runScopeHandlers, type EditorView, type Panel } from '@codemirror/view'
import { SearchQuery, closeSearchPanel, findNext, findPrevious, getSearchQuery, replaceAll, replaceNext, selectMatches, setSearchQuery } from '@codemirror/search'
import { msg } from '../i18n'
import { glyphDOM, type glyphPaths } from '../shell/glyph'
import buttons from '../ui/Button.module.css'
import inputs from '../ui/Input.module.css'
import styles from './EditorSearch.module.css'

/** CodeMirror owns this DOM; controls share the same styles and glyphs as React. */
export function editorSearch(view: EditorView): Panel {
  const owner = view.dom.ownerDocument
  const node = <T extends keyof HTMLElementTagNameMap>(tag: T, className = '') => {
    const element = owner.createElement(tag)
    element.className = className
    return element
  }
  const labels: (() => void)[] = []
  const action = (label: () => string, run: () => void, icon?: keyof typeof glyphPaths) => {
    const button = node('button', [buttons.button, buttons.quiet, icon ? buttons.icon : ''].join(' '))
    button.type = 'button'
    if (icon) button.append(glyphDOM(icon, owner))
    labels.push(() => { button.setAttribute('aria-label', label()); if (icon) button.dataset.tooltip = label(); if (!icon) button.textContent = label() })
    button.addEventListener('click', run)
    return button
  }
  const field = (name: string, label: () => string) => {
    const input = node('input', inputs.input)
    input.type = 'text'; input.name = name; input.spellcheck = false; input.autocomplete = 'off'
    input.setAttribute('form', '')
    labels.push(() => { input.placeholder = label(); input.setAttribute('aria-label', label()) })
    return input
  }
  const dom = node('div', styles.panel)
  dom.dataset.editorSearch = 'true'
  dom.setAttribute('role', 'search')
  labels.push(() => dom.setAttribute('aria-label', msg('Find in file')))
  const findRow = node('div', styles.findRow), options = node('div', styles.options), replacement = node('div', styles.replaceRow)
  const searchField = field('search', () => msg('Find in file')), replaceField = field('replace', () => msg('Replace with'))
  searchField.setAttribute('main-field', 'true')
  let query = getSearchQuery(view.state), replacing = false
  const toggleReplace = action(() => replacing ? msg('Hide replacement controls') : msg('Show replacement controls'), () => {
    replacing = !replacing; sync()
    if (replacing) replaceField.focus()
  }, 'right')
  const previous = action(() => msg('Previous match'), () => { findPrevious(view) }, 'up')
  const next = action(() => msg('Next match'), () => { findNext(view) }, 'down')
  const close = action(() => msg('Close'), () => { closeSearchPanel(view) }, 'close')
  const navigation = node('div', styles.actions); navigation.append(previous, next, close)
  findRow.append(toggleReplace, searchField, navigation)
  const toggles = [
    ['caseSensitive', () => msg('Match case'), 'Aa'],
    ['wholeWord', () => msg('Whole word'), 'Ab'],
    ['regexp', () => msg('Regular expression'), '.*']
  ] as const
  const toggleButtons = toggles.map(([key, label, symbol]) => {
    const button = action(label, () => { view.dispatch({ effects: setSearchQuery.of(new SearchQuery({ ...query, [key]: !query[key] })) }) })
    labels.push(() => { button.textContent = symbol; button.dataset.tooltip = label() })
    button.classList.add(buttons.icon, styles.option)
    if (key === 'wholeWord') button.classList.add(styles.wholeWord)
    options.append(button)
    return button
  })
  const select = action(() => msg('Select all matches'), () => { selectMatches(view) })
  options.append(select)
  const replace = action(() => msg('Replace'), () => { replaceNext(view) })
  const replaceEvery = action(() => msg('Replace all'), () => { replaceAll(view) })
  const replaceActions = node('div', styles.actions); replaceActions.append(replace, replaceEvery)
  replacement.append(replaceField, replaceActions)
  replacement.hidden = true
  const error = node('span', styles.error)
  error.setAttribute('role', 'status')
  options.append(error)
  dom.append(findRow, replacement, options)
  function sync() {
    labels.forEach(label => label())
    if (searchField.value !== query.search) searchField.value = query.search
    if (replaceField.value !== query.replace) replaceField.value = query.replace
    const invalid = query.search.length > 0 && !query.valid
    searchField.setAttribute('aria-invalid', String(invalid))
    error.textContent = invalid ? msg('Invalid regular expression') : ''
    error.hidden = !invalid
    toggleButtons.forEach((button, index) => button.setAttribute('aria-pressed', String(query[toggles[index]![0]])))
    for (const button of [previous, next, select, replace, replaceEvery]) button.disabled = !query.valid
    replace.disabled ||= view.state.readOnly; replaceEvery.disabled ||= view.state.readOnly
    toggleReplace.hidden = view.state.readOnly
    toggleReplace.setAttribute('aria-expanded', String(replacing))
    toggleReplace.replaceChildren(glyphDOM(replacing ? 'down' : 'right', owner))
    replacement.hidden = !replacing || view.state.readOnly
  }
  function commit() {
    const next = new SearchQuery({ ...query, search: searchField.value, replace: replaceField.value })
    if (!next.eq(query)) view.dispatch({ effects: setSearchQuery.of(next) })
  }
  searchField.addEventListener('input', commit); replaceField.addEventListener('input', commit)
  dom.addEventListener('keydown', event => {
    // Escape first dismisses a tooltip; only a subsequent Escape closes Find.
    if (event.defaultPrevented) return
    if (runScopeHandlers(view, event, 'search-panel')) event.preventDefault()
    else if (event.key === 'Enter' && event.target === searchField) {
      event.preventDefault(); (event.shiftKey ? findPrevious : findNext)(view)
    } else if (event.key === 'Enter' && event.target === replaceField) {
      event.preventDefault(); replaceNext(view)
    }
  })
  sync()
  return {
    dom, top: true,
    mount() { searchField.focus(); searchField.select() },
    update() { query = getSearchQuery(view.state); sync() }
  }
}
