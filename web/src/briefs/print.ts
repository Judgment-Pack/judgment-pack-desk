/** Browser print keeps local documents local and supports Save as PDF. */
export function printBrief(element: HTMLElement, title: string) {
  const popup = window.open('', '_blank', 'width=900,height=900')
  if (!popup) throw Error('Allow this print window to export the brief.')
  popup.opener = null
  popup.document.title = title
  const style = popup.document.createElement('style')
  style.textContent = '@page{size:A4;margin:16mm}body{font:11pt/1.45 system-ui;color:#111;overflow-wrap:anywhere}h2{font-size:18pt}h3{font-size:11pt;margin-bottom:4pt}section{break-inside:avoid;margin:12pt 0}p{margin:0;white-space:pre-wrap}ul{padding-left:16pt}li{margin:5pt 0}li span:last-child{margin-left:12pt}button{display:none}footer{margin-top:12pt;font-size:9pt;color:#555}'
  popup.document.head.append(style)
  const copy = element.cloneNode(true) as HTMLElement
  copy.querySelectorAll('[data-no-print]').forEach(n => n.remove())
  copy.querySelectorAll('button').forEach(button => { const text = popup.document.createElement('p'); text.textContent = button.textContent; button.replaceWith(text) })
  popup.document.body.append(copy)
  popup.setTimeout(() => { popup.focus(); popup.print() }, 150)
}
