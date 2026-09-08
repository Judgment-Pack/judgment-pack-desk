/**
 * The palette, read off the stylesheet, and the arithmetic that judges it.
 *
 * **Why an instrument and not assertions in the test.** A dark palette is a
 * set of claims that can only be checked by computing something — that every
 * token the light palette defines has a dark value, that the two blocks which
 * select dark carry the same one, that a text colour and the background under
 * it are far enough apart to read. Each of those is arithmetic over the sheet's
 * own bytes, and arithmetic written inline in a test is arithmetic nothing
 * proves. So it lives here, on its own, and `palette.test.ts` proves it against
 * known answers — black on white is 21:1, a colour on itself is 1:1 — before it
 * asks it anything about the desk.
 *
 * **Contrast is measured, never eyeballed.** The ratio is WCAG 2.x's: relative
 * luminance from the sRGB channels, gamma-expanded, `(L1 + 0.05) / (L2 +
 * 0.05)`. It takes hex colours because every colour token that appears in an
 * asserted pair is written as one; a token that is not — the scrim and the
 * shadow, which are `rgb(… / …)` — is not a foreground on a background and is
 * in no pair.
 *
 * This reads the sheets as text for `convention.test.ts`'s reason: vitest runs
 * with `css: false`, so no stylesheet is processed and a palette that was
 * deleted would render exactly like one that is intact.
 */
import { declarationsIn } from './declarations'

/** One block's custom properties, in source order. */
export type Tokens = Map<string, string>

/**
 * The custom properties of the block a selector opens, by brace matching.
 *
 * The header is matched literally and must appear exactly once — a sheet with
 * two `:root {` blocks is a palette with two homes, and reading the first of
 * them would be this instrument quietly choosing which one counts.
 */
export function blockAt(sheet: string, header: string): Tokens {
  const [start, end] = blockRange(sheet, header)
  const tokens: Tokens = new Map()
  for (const declaration of declarationsIn(`x{${sheet.slice(start, end)}}`)) {
    if (declaration.property.startsWith('--')) tokens.set(declaration.property, declaration.value)
  }
  return tokens
}

/** The inner bounds of that block: the offsets between its braces. */
function blockRange(sheet: string, header: string): [number, number] {
  const opening = sheet.indexOf(header)
  if (opening === -1) throw new Error(`no block for ${header}`)
  if (sheet.indexOf(header, opening + 1) !== -1) throw new Error(`two blocks for ${header}`)
  const start = sheet.indexOf('{', opening + header.length - 1)
  let depth = 0
  for (let scan = start; scan < sheet.length; scan += 1) {
    if (sheet[scan] === '{') depth += 1
    if (sheet[scan] === '}') {
      depth -= 1
      if (depth === 0) return [start + 1, scan]
    }
  }
  throw new Error(`unterminated block for ${header}`)
}

/**
 * The sheet with those blocks taken out.
 *
 * What is left is everything the palette blocks are not, which is where a
 * colour literal is a defect: inside them, a literal is the palette.
 */
export function withoutBlocks(sheet: string, headers: readonly string[]): string {
  const cuts = headers
    .map((header) => blockRange(sheet, header))
    .sort((left, right) => right[0] - left[0])
  let text = sheet
  for (const [start, end] of cuts) text = text.slice(0, start) + text.slice(end)
  return text
}

/** Every colour spelling this sheet may not carry outside its token blocks. */
const HEX = /#[0-9a-fA-F]{3,8}\b/
const COLOUR_FUNCTION = /\b(rgba?|hsla?|hwb|lab|lch|oklab|oklch|color|color-mix|light-dark)\s*\(/i

/**
 * Whether a token's value is a colour, so that "every colour token has a dark
 * definition" is a question the sheet can answer about itself.
 *
 * Asked of the value and not the name: `--radius`, `--mono` and the density
 * scale are tokens too and have no business in a palette block, and a rule
 * that went by name would need a list of them kept in step by hand.
 */
export function isColourValue(value: string): boolean {
  return HEX.test(value) || COLOUR_FUNCTION.test(value)
}

/** The named CSS colours — the half a `#` scan cannot see. */
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

/**
 * Every colour literal a sheet spells, wherever it spells it.
 *
 * **A stricter question than the modules' rule, and a different one.**
 * `colourProblemsIn` asks whether each colour-*bearing* declaration took its
 * colour from a token, which is right for a module and wrong for a global
 * sheet: `border: 1px solid transparent` is a placeholder the shell writes in
 * five places and is not a palette, and a rule that reported it would be
 * answered by rewriting five correct declarations. What a global sheet must
 * not do is *spell a colour* — a hex, a colour function, or a named colour —
 * anywhere, in any property, including in a custom property of its own.
 *
 * Strings are removed before the scan, because `content: "red"` is a word on a
 * page and not a colour; comments are already gone, because a commented-out
 * literal is not one.
 */
export function literalColoursIn(sheet: string): { where: string; problem: string }[] {
  const found: { where: string; problem: string }[] = []
  for (const declaration of declarationsIn(sheet)) {
    const value = declaration.value.replace(/"[^"]*"|'[^']*'/g, ' ')
    if (HEX.test(value)) {
      found.push({ where: declaration.property, problem: `a hex colour: ${declaration.value}` })
      continue
    }
    if (COLOUR_FUNCTION.test(value)) {
      found.push({
        where: declaration.property,
        problem: `a colour function: ${declaration.value}`
      })
      continue
    }
    const named = value.split(/[^a-zA-Z-]+/).find((word) => NAMED.has(word.toLowerCase()))
    if (named !== undefined) {
      found.push({ where: declaration.property, problem: `the named colour ${named}` })
    }
  }
  return found
}

/** One channel, gamma-expanded to linear light. */
function channel(value: number): number {
  const unit = value / 255
  return unit <= 0.04045 ? unit / 12.92 : ((unit + 0.055) / 1.055) ** 2.4
}

/** WCAG relative luminance of a `#rgb` or `#rrggbb` colour. */
export function luminance(colour: string): number {
  const match = /^#([0-9a-fA-F]{3}|[0-9a-fA-F]{6})$/.exec(colour.trim())
  if (match === null) throw new Error(`not a hex colour: ${colour}`)
  const digits = match[1]!
  const full =
    digits.length === 3
      ? digits
          .split('')
          .map((digit) => digit + digit)
          .join('')
      : digits
  const [red, green, blue] = [0, 2, 4].map((at) => Number.parseInt(full.slice(at, at + 2), 16))
  return 0.2126 * channel(red!) + 0.7152 * channel(green!) + 0.0722 * channel(blue!)
}

/** The WCAG contrast ratio between two colours, from 1 to 21. */
export function contrastRatio(one: string, other: string): number {
  const a = luminance(one)
  const b = luminance(other)
  return (Math.max(a, b) + 0.05) / (Math.min(a, b) + 0.05)
}
