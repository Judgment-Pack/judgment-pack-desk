import { afterEach, expect, it } from 'vitest'
import { cleanup, render, screen } from '@testing-library/react'
import { AttributionFrame } from './AttributionFrame'
afterEach(cleanup)
// The provider's attribution markup is shown as it came, in a frame that can
// run nothing: no allow-scripts beside allow-same-origin, and a policy that
// forbids scripts, forms and any load but inline styles and data: images.
it('frames the provider markup unchanged, with scripts forbidden twice',()=>{
 const html='<div class="chip"><a href="https://www.google.com/search?q=x">x</a><script>parent.leak=1</script></div>'
 render(<AttributionFrame html={html}/>)
 const frame=screen.getByTitle('Search provider attribution') as HTMLIFrameElement
 expect(frame.getAttribute('sandbox')!.split(/\s+/).sort()).toEqual(['allow-popups','allow-popups-to-escape-sandbox','allow-same-origin'])
 expect(frame.getAttribute('referrerpolicy')).toBe('no-referrer')
 const doc=frame.getAttribute('srcdoc')!
 expect(doc).toContain(`<body>${html}</body>`)
 expect(doc).toContain(`content="default-src 'none'; script-src 'none'; style-src 'unsafe-inline'; img-src data:; base-uri 'none'; form-action 'none'"`)
 expect((window as {leak?:number}).leak).toBeUndefined()
})
