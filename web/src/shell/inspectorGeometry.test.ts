import { describe, expect, it } from 'vitest'
import { inspectorGeometry } from './inspectorGeometry'

describe('readable inspector and main widths', () => {
  it('uses 360px by default and permits 640px on a wide desktop', () => {
    expect(inspectorGeometry(1664, 360, 768, false)).toEqual({ drawer: false, width: 360, min: 320, max: 640 })
  })
  it('keeps the map working area when the inspector reaches its laptop limit', () => {
    const layout = inspectorGeometry(1184, 640, 768, false)
    expect(layout.drawer).toBe(false)
    expect(layout.width).toBe(414)
    expect(1184 - 2 - layout.width).toBe(768)
  })
  it('uses a drawer if even the minimum would squeeze the map', () => {
    expect(inspectorGeometry(1024, 360, 768, false).drawer).toBe(true)
    expect(inspectorGeometry(1664, 360, 768, true).drawer).toBe(true)
  })
  it('never lets the inspector dominate main on routes without a width claim', () => {
    const layout = inspectorGeometry(1144, 640, 0, false)
    expect(layout.width).toBeLessThanOrEqual((1144 - 2) * .45)
    expect(1144 - 2 - layout.width).toBeGreaterThanOrEqual(480)
  })
  it('clamps configured extremes and restores a preference when room returns', () => {
    expect(inspectorGeometry(1664, 240, 0, false).width).toBe(320)
    expect(inspectorGeometry(1664, 720, 0, false).width).toBe(640)
    expect(inspectorGeometry(1184, 640, 768, false).width).toBe(414)
    expect(inspectorGeometry(1664, 640, 768, false).width).toBe(640)
  })
})
