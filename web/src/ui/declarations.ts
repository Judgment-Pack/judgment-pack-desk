/**
 * A CSS sheet's declarations, tokenized enough to ask about colour.
 *
 * **Why this is written out rather than imported.** The convention test needs
 * to know which declarations can carry a colour, and a regex over
 * `(background|color|border[a-z-]*):` cannot: it matched `border: 1px solid
 * red` and then skipped it, because the *property name* is `border` and the
 * filter asked whether the name contained `background` or `color`. So
 * `border: 1px solid red`, `outline: 2px solid red` and a named colour inside
 * a `box-shadow` all passed a rule whose whole purpose is to catch them.
 *
 * A real parser is the right answer and a dependency is not: this is a
 * declaration splitter that respects strings, comments, parentheses and nested
 * blocks, which is everything a `.module.css` in this repository contains. It
 * is exported on its own so the test can prove it on deliberately broken
 * fixtures rather than only on sheets that are already clean.
 */

export interface Declaration {
  property: string
  value: string
}

/**
 * Every declaration in a sheet, at any nesting depth.
 *
 * At-rule preludes and selectors are skipped; what comes back is the
 * `property: value` pairs, which is what a colour question is about. Comments
 * are removed first because a commented-out declaration is not one.
 */
export function declarationsIn(sheet: string): Declaration[] {
  const text = sheet.replace(/\/\*[\s\S]*?\*\//g, '')
  const found: Declaration[] = []
  let buffer = ''
  let depth = 0
  let quote: string | undefined
  let parens = 0

  const flush = () => {
    const chunk = buffer.trim()
    buffer = ''
    if (chunk === '') return
    // An at-rule with no block — `@import url(x);` — is not a declaration.
    if (chunk.startsWith('@')) return
    const colon = chunk.indexOf(':')
    if (colon === -1) return
    found.push({
      property: chunk.slice(0, colon).trim().toLowerCase(),
      value: chunk.slice(colon + 1).trim()
    })
  }

  for (const character of text) {
    if (quote !== undefined) {
      buffer += character
      if (character === quote) quote = undefined
      continue
    }
    if (character === '"' || character === "'") {
      quote = character
      buffer += character
      continue
    }
    if (character === '(') parens += 1
    if (character === ')') parens = Math.max(0, parens - 1)
    // A semicolon inside `var(--x, a, b)` is impossible, but a comma is not,
    // and a nested block inside a value cannot happen either — so parentheses
    // only ever need to hold the separators.
    if (parens === 0 && character === '{') {
      // Everything before a block is a selector or an at-rule prelude.
      buffer = ''
      depth += 1
      continue
    }
    if (parens === 0 && character === '}') {
      flush()
      depth = Math.max(0, depth - 1)
      continue
    }
    if (parens === 0 && character === ';') {
      flush()
      continue
    }
    buffer += character
  }
  return found
}

/**
 * The properties and shorthands whose value can carry a colour.
 *
 * Enumerated rather than pattern-matched, because the patterns are what got
 * this wrong: `border` does not contain "color" and carries one anyway. Every
 * shorthand that admits a `<color>` in its grammar is here, and the longhands
 * are matched by suffix so `border-top-color` needs no entry of its own.
 */
const COLOUR_BEARING = new Set([
  'color',
  'background',
  'background-color',
  'background-image',
  'border',
  'border-top',
  'border-right',
  'border-bottom',
  'border-left',
  'border-block',
  'border-inline',
  'border-block-start',
  'border-block-end',
  'border-inline-start',
  'border-inline-end',
  'outline',
  'box-shadow',
  'text-shadow',
  'text-decoration',
  'text-decoration-color',
  'text-emphasis',
  'text-emphasis-color',
  'caret-color',
  'column-rule',
  'column-rule-color',
  'fill',
  'stroke',
  'accent-color',
  'scrollbar-color',
  'stop-color',
  'flood-color',
  'lighting-color'
])

/** Whether this property's value is somewhere a colour can be written. */
export function carriesColour(property: string): boolean {
  return COLOUR_BEARING.has(property) || property.endsWith('-color')
}

/**
 * The CSS-wide and colourless keywords a value may be **entirely** made of.
 *
 * Only checked against the whole value: `border: 1px solid none` is not a
 * thing, and `none` inside a shorthand alongside a named colour must not
 * exempt the declaration.
 */
const COLOURLESS = new Set(['none', 'transparent', 'inherit', 'initial', 'unset', 'revert', 'currentcolor'])

/**
 * The named CSS colours, which is the half a `#rrggbb` scan misses entirely.
 *
 * The full set rather than a sample: a sample is a list of the colours somebody
 * thought of, and `rebeccapurple` is exactly as much a second palette as `red`.
 */
const NAMED = new Set(
  `aliceblue antiquewhite aqua aquamarine azure beige bisque black blanchedalmond blue blueviolet brown
   burlywood cadetblue chartreuse chocolate coral cornflowerblue cornsilk crimson cyan darkblue darkcyan
   darkgoldenrod darkgray darkgreen darkgrey darkkhaki darkmagenta darkolivegreen darkorange darkorchid
   darkred darksalmon darkseagreen darkslateblue darkslategray darkslategrey darkturquoise darkviolet
   deeppink deepskyblue dimgray dimgrey dodgerblue firebrick floralwhite forestgreen fuchsia gainsboro
   ghostwhite gold goldenrod gray green greenyellow grey honeydew hotpink indianred indigo ivory khaki
   lavender lavenderblush lawngreen lemonchiffon lightblue lightcoral lightcyan lightgoldenrodyellow
   lightgray lightgreen lightgrey lightpink lightsalmon lightseagreen lightskyblue lightslategray
   lightslategrey lightsteelblue lightyellow lime limegreen linen magenta maroon mediumaquamarine
   mediumblue mediumorchid mediumpurple mediumseagreen mediumslateblue mediumspringgreen mediumturquoise
   mediumvioletred midnightblue mintcream mistyrose moccasin navajowhite navy oldlace olive olivedrab
   orange orangered orchid palegoldenrod palegreen paleturquoise palevioletred papayawhip peachpuff peru
   pink plum powderblue purple rebeccapurple red rosybrown royalblue saddlebrown salmon sandybrown
   seagreen seashell sienna silver skyblue slateblue slategray slategrey snow springgreen steelblue tan
   teal thistle tomato turquoise violet wheat white whitesmoke yellow yellowgreen`
    .split(/\s+/)
    .filter(Boolean)
)

/** Every colour-function spelling a value must not carry. */
const COLOUR_FUNCTIONS = /\b(rgba?|hsla?|hwb|lab|lch|oklab|oklch|color|color-mix|light-dark)\s*\(/i

/**
 * Why this declaration's colour is not a token, if it is not.
 *
 * A colour-bearing declaration must take its colour from `var(--…)`. What is
 * refused is a literal in any of its three spellings: a hex triple, a colour
 * function, or a named colour — the last being the one a `#` scan cannot see
 * and the one `border: 1px solid red` was hiding behind.
 */
export function colourProblem(declaration: Declaration): string | undefined {
  if (!carriesColour(declaration.property)) return undefined
  return colourProblemIn(declaration.value.trim())
}

/**
 * The same question about one value, so a `var()` fallback can be asked it too.
 *
 * Recursion is the point. `var(--ink, red)` is a declaration that uses a token
 * *and* names a colour, and the check that removed the whole `var(…)` before
 * looking for names could not see the second half — so `color: var(--ink, red)`
 * passed a rule whose entire job is to catch a literal. A fallback is a colour
 * this sheet chose; that it is only used when the token is missing does not
 * make it not a second palette.
 */
function colourProblemIn(value: string): string | undefined {
  if (value === '') return undefined
  if (COLOURLESS.has(value.toLowerCase())) return undefined
  if (/#[0-9a-fA-F]{3,8}\b/.test(value)) return 'a hex colour'
  if (COLOUR_FUNCTIONS.test(value)) return 'a colour function'

  // Every `var()` in the value, with its own fallback asked the same question.
  const references = varReferences(value)
  for (const reference of references) {
    if (reference.fallback === undefined) continue
    const problem = colourProblemIn(reference.fallback.trim())
    if (problem !== undefined) return `${problem} in a var() fallback`
  }

  // Words outside every `var(…)`, so a token's own name cannot be mistaken
  // for one.
  const named = wordsOutsideVar(value).find((word) => NAMED.has(word.toLowerCase()))
  if (named !== undefined) return `the named colour ${named}`
  // Everything colour-bearing has to *have* a colour from somewhere, and the
  // only place it may come from is a token.
  return references.length > 0 ? undefined : 'no token'
}

/** One `var()` reference: the custom property it names, and its fallback. */
export interface VarReference {
  name: string
  fallback: string | undefined
}

/**
 * Every `var()` in a value, with balanced parentheses.
 *
 * `[^)]*` cannot do this: a fallback is allowed to contain another `var()`, and
 * `var(--a, var(--b, red))` ends at the *first* `)` under that pattern — which
 * hides the `red` from every check downstream.
 */
export function varReferences(value: string): VarReference[] {
  const found: VarReference[] = []
  for (let index = value.indexOf('var('); index !== -1; index = value.indexOf('var(', index + 1)) {
    let depth = 0
    let end = -1
    for (let scan = index + 3; scan < value.length; scan += 1) {
      if (value[scan] === '(') depth += 1
      if (value[scan] === ')') {
        depth -= 1
        if (depth === 0) {
          end = scan
          break
        }
      }
    }
    if (end === -1) break
    const inside = value.slice(index + 4, end)
    const comma = splitOnce(inside)
    found.push({ name: comma.name.trim(), fallback: comma.fallback })
  }
  return found
}

/** `--name, fallback` split on the first top-level comma. */
function splitOnce(inside: string): { name: string; fallback: string | undefined } {
  let depth = 0
  for (let index = 0; index < inside.length; index += 1) {
    if (inside[index] === '(') depth += 1
    if (inside[index] === ')') depth -= 1
    if (inside[index] === ',' && depth === 0) {
      return { name: inside.slice(0, index), fallback: inside.slice(index + 1) }
    }
  }
  return { name: inside, fallback: undefined }
}

/** The bare words of a value, with every balanced `var(…)` removed. */
function wordsOutsideVar(value: string): string[] {
  let stripped = ''
  let index = 0
  while (index < value.length) {
    if (value.startsWith('var(', index)) {
      let depth = 0
      let scan = index + 3
      for (; scan < value.length; scan += 1) {
        if (value[scan] === '(') depth += 1
        if (value[scan] === ')') {
          depth -= 1
          if (depth === 0) break
        }
      }
      stripped += ' '
      index = scan + 1
      continue
    }
    stripped += value[index]
    index += 1
  }
  return stripped.split(/[^a-zA-Z-]+/).filter(Boolean)
}

/**
 * The colour problems a whole sheet carries, custom properties included.
 *
 * **A module-local custom property is not a token.** `--local-ink: red` followed
 * by `color: var(--local-ink)` satisfies every per-declaration rule — the
 * declaration takes its colour from a `var()`, and the definition is not a
 * colour-bearing property — and is a second palette all the same, one the theme
 * attribute does not reach. So a custom property defined *in the module* is
 * inspected as though it were the value it stands for, and a colour-bearing
 * declaration that resolves only through one is reported at the definition.
 *
 * A `var(--x)` naming a property this sheet does **not** define is a token from
 * `styles.css`, which is the whole point of the convention and is left alone.
 */
export function colourProblemsIn(sheet: string): { where: string; problem: string }[] {
  const declarations = declarationsIn(sheet)
  const local = new Map<string, string>()
  for (const declaration of declarations) {
    if (declaration.property.startsWith('--')) local.set(declaration.property, declaration.value)
  }
  const problems: { where: string; problem: string }[] = []
  for (const declaration of declarations) {
    // A local custom property is judged by what it holds, whatever it is
    // called: `--local-ink: red` is a literal wherever it is later used.
    if (declaration.property.startsWith('--')) {
      const problem = colourProblemIn(declaration.value.trim())
      // Only a *colour* is a problem here. A local `--gap: 4px` is ordinary.
      if (problem !== undefined && problem !== 'no token') {
        problems.push({ where: declaration.property, problem })
      }
      continue
    }
    const direct = colourProblem(declaration)
    if (direct !== undefined) {
      problems.push({ where: declaration.property, problem: direct })
      continue
    }
    if (!carriesColour(declaration.property)) continue
    // And a colour-bearing declaration whose only source is a local property
    // is reported too, because the module decided that colour.
    for (const reference of varReferences(declaration.value)) {
      const held = local.get(reference.name)
      if (held === undefined) continue
      const problem = colourProblemIn(held.trim())
      if (problem !== undefined && problem !== 'no token') {
        problems.push({
          where: declaration.property,
          problem: `${problem} through the module-local ${reference.name}`
        })
      }
    }
  }
  return problems
}
