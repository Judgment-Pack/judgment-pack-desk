/** Shared geometry for React icons and DOM-owned widgets such as CodeMirror. */
export const glyphProps = {
  width: 16, height: 16, viewBox: '0 0 16 16', fill: 'none', stroke: 'currentColor',
  strokeWidth: 1.75, strokeLinecap: 'round', strokeLinejoin: 'round',
  'aria-hidden': true, focusable: false
} as const

export const glyphPaths = {
  down: 'M3.5 6 8 10.5 12.5 6',
  right: 'M6 3.5 10.5 8 6 12.5',
  up: 'M3.5 10 8 5.5 12.5 10',
  close: 'M4 4l8 8M12 4l-8 8'
} as const

export function glyphDOM(name: keyof typeof glyphPaths, owner: Document = document): SVGSVGElement {
  const svg = owner.createElementNS('http://www.w3.org/2000/svg', 'svg')
  const attributes: Record<string, string> = { strokeWidth: 'stroke-width', strokeLinecap: 'stroke-linecap', strokeLinejoin: 'stroke-linejoin' }
  for (const [key, value] of Object.entries(glyphProps)) {
    const attribute = attributes[key] ?? key
    svg.setAttribute(attribute, String(value))
  }
  const path = owner.createElementNS(svg.namespaceURI, 'path')
  path.setAttribute('d', glyphPaths[name])
  svg.append(path)
  return svg
}
