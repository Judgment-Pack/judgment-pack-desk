/**
 * The desk's glyphs, hand-drawn and kept in the repo.
 *
 * Not a package: an icon dependency is bytes in a go:embed'd binary and a
 * second visual vocabulary to keep in step with a hand-written sheet, for
 * fifteen shapes. One wrapper carries the geometry — a 16px grid, stroke 1.75,
 * round caps and joins — so no glyph can drift from the others by being drawn
 * a different way.
 *
 * **No icon is ever an accessible name here.** None carries a `<title>` and
 * none is `role="img"`: every one is `aria-hidden`, and every icon-only control
 * carries its own `aria-label` and, in the collapsed rail, a `Tooltip` as well.
 * A tooltip is a hint; the label is the name.
 */
import type { ReactNode } from 'react'

function Glyph({ children }: { children: ReactNode }) {
  return (
    <svg
      width={16}
      height={16}
      viewBox="0 0 16 16"
      fill="none"
      stroke="currentColor"
      strokeWidth={1.75}
      strokeLinecap="round"
      strokeLinejoin="round"
      aria-hidden="true"
      focusable={false}
    >
      {children}
    </svg>
  )
}

export function IconPlus() {
  return (
    <Glyph>
      <path d="M8 3v10M3 8h10" />
    </Glyph>
  )
}

export function IconChevronLeft() {
  return (
    <Glyph>
      <path d="M10 3.5 5.5 8l4.5 4.5" />
    </Glyph>
  )
}

export function IconChevronRight() {
  return (
    <Glyph>
      <path d="M6 3.5 10.5 8 6 12.5" />
    </Glyph>
  )
}

export function IconChevronDown() {
  return (
    <Glyph>
      <path d="M3.5 6 8 10.5 12.5 6" />
    </Glyph>
  )
}

export function IconChevronUp() {
  return (
    <Glyph>
      <path d="M3.5 10 8 5.5 12.5 10" />
    </Glyph>
  )
}

export function IconGear() {
  return (
    <Glyph>
      <circle cx="8" cy="8" r="2.25" />
      <path d="M8 1.5v1.75M8 12.75v1.75M14.5 8h-1.75M3.25 8H1.5M12.6 3.4l-1.24 1.24M4.64 11.36 3.4 12.6M12.6 12.6l-1.24-1.24M4.64 4.64 3.4 3.4" />
    </Glyph>
  )
}

export function IconHelp() {
  return (
    <Glyph>
      <circle cx="8" cy="8" r="6.25" />
      <path d="M6.25 6.1a1.85 1.85 0 1 1 2.2 2.2c-.35.09-.45.4-.45.75v.4" />
      <path d="M8 12.05h.01" />
    </Glyph>
  )
}

/** A pack: the cube. */
export function IconPack() {
  return (
    <Glyph>
      <path d="M8 1.75 13.75 5v6L8 14.25 2.25 11V5z" />
      <path d="M2.25 5 8 8.25 13.75 5M8 8.25v6" />
    </Glyph>
  )
}

/** A matrix: the 2×2 grid. */
export function IconMatrix() {
  return (
    <Glyph>
      <rect x="2.25" y="2.25" width="11.5" height="11.5" rx="1.5" />
      <path d="M8 2.25v11.5M2.25 8h11.5" />
    </Glyph>
  )
}

/** A graph: three nodes, two edges. */
export function IconGraph() {
  return (
    <Glyph>
      <circle cx="3.75" cy="8" r="1.75" />
      <circle cx="12.25" cy="3.9" r="1.75" />
      <circle cx="12.25" cy="12.1" r="1.75" />
      <path d="M5.3 7.2 10.7 4.6M5.3 8.8l5.4 2.6" />
    </Glyph>
  )
}

export function IconPencil() {
  return (
    <Glyph>
      <path d="M11.1 2.4a1.6 1.6 0 0 1 2.5 2.5L5.4 13.1l-3.15.65.65-3.15z" />
      <path d="M10.1 3.4l2.5 2.5" />
    </Glyph>
  )
}

export function IconPanelRight() {
  return (
    <Glyph>
      <rect x="2.25" y="2.75" width="11.5" height="10.5" rx="1.5" />
      <path d="M10 2.75v10.5" />
    </Glyph>
  )
}

export function IconPanelBottom() {
  return (
    <Glyph>
      <rect x="2.25" y="2.75" width="11.5" height="10.5" rx="1.5" />
      <path d="M2.25 10h11.5" />
    </Glyph>
  )
}

export function IconClose() {
  return (
    <Glyph>
      <path d="M4 4l8 8M12 4l-8 8" />
    </Glyph>
  )
}

export function IconCopy() {
  return (
    <Glyph>
      <rect x="5.75" y="5.75" width="8" height="8" rx="1.5" />
      <path d="M10.25 5.75v-1.5a1.5 1.5 0 0 0-1.5-1.5h-4.5a1.5 1.5 0 0 0-1.5 1.5v4.5a1.5 1.5 0 0 0 1.5 1.5h1.5" />
    </Glyph>
  )
}
