/**
 * The narration sweep: what counts as a paragraph on a page that is not
 * supposed to have any.
 *
 * # Two measures, because one of them was defeatable
 *
 * The first version measured **single DOM text nodes**. That is the wrong unit
 * for the rule it stands for: `<p><span>{eighty}</span><span>{eighty}</span></p>`
 * is a hundred and sixty characters of prose to a reader and two compliant
 * nodes to the sweep, and JSX produces exactly that shape whenever a sentence
 * carries an inline element. So the sweep now measures **both**: no text node
 * over the bound, and no block element whose own prose exceeds it once its
 * descendants are added up.
 *
 * # What is exempt, and why the exemption is the definition
 *
 * Quoted material. A path, a decoder's own refusal sentence, a member of a
 * configuration file as it is written — none of those is narration, they are
 * the thing the page exists to show, and their length is whoever wrote them.
 * `<code>` and `<pre>` are where each of them goes, so text inside one is not
 * counted at either measure.
 */

/** A line. Anything longer is a paragraph. */
export const NARRATION_BOUND = 140

/**
 * The block elements a sentence lands in.
 *
 * Deliberately not `div` or `section`: those nest, so counting them would add
 * up a whole card and fail on a page of perfectly short lines. What is listed
 * is what a sentence is actually written into.
 */
const BLOCKS = 'p, li, dt, dd, figcaption, summary, legend, label, h1, h2, h3, h4, h5, h6'

/** One offence, named well enough to repair. */
export interface Narration {
  /** `text node` or the block's tag name. */
  where: string
  length: number
  /** The first of it, for a failure message somebody can act on. */
  says: string
}

/**
 * Every piece of this page's own prose that is longer than a line.
 *
 * Both measures, so that neither a long node nor a long paragraph split across
 * short ones survives.
 */
export function narrationIn(container: Element): Narration[] {
  const found: Narration[] = []
  const document = container.ownerDocument
  const walker = document.createTreeWalker(container, 4 /* SHOW_TEXT */)
  for (let node = walker.nextNode(); node !== null; node = walker.nextNode()) {
    if (quoted(node)) continue
    const text = node.textContent ?? ''
    if (text.trim() !== '' && text.length > NARRATION_BOUND) {
      found.push({ where: 'text node', length: text.length, says: text.slice(0, 90) })
    }
  }
  for (const block of container.querySelectorAll(BLOCKS)) {
    // A block inside another counted block is left to the inner one, so a
    // `<dd>` wrapping a `<p>` does not report the same sentence twice.
    if (block.parentElement?.closest(BLOCKS) !== null) continue
    const prose = proseOf(block)
    if (prose.length > NARRATION_BOUND) {
      found.push({ where: block.tagName.toLowerCase(), length: prose.length, says: prose.slice(0, 90) })
    }
  }
  return found
}

/** This element's own prose: every descendant text node outside a quotation. */
function proseOf(element: Element): string {
  const walker = element.ownerDocument.createTreeWalker(element, 4 /* SHOW_TEXT */)
  let prose = ''
  for (let node = walker.nextNode(); node !== null; node = walker.nextNode()) {
    if (quoted(node)) continue
    prose += node.textContent ?? ''
  }
  return prose
}

/** Whether a text node sits inside quoted material. */
function quoted(node: Node): boolean {
  return node.parentElement?.closest('code, pre') != null
}
