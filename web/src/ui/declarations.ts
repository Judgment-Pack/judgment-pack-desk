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
  const value = declaration.value.trim()
  if (value === '') return undefined
  if (COLOURLESS.has(value.toLowerCase())) return undefined
  if (/#[0-9a-fA-F]{3,8}\b/.test(value)) return 'a hex colour'
  if (COLOUR_FUNCTIONS.test(value)) return 'a colour function'
  // Words outside `var(…)`, so a token's own name cannot be mistaken for one.
  const outside = value.replace(/var\([^)]*\)/g, ' ')
  const named = outside
    .split(/[^a-zA-Z-]+/)
    .filter(Boolean)
    .find((word) => NAMED.has(word.toLowerCase()))
  if (named !== undefined) return `the named colour ${named}`
  // Everything colour-bearing has to *have* a colour from somewhere, and the
  // only place it may come from is a token.
  return value.includes('var(--') ? undefined : 'no token'
}
