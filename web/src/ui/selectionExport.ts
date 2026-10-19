/** Export exactly the selected, loaded rows. This is a table extract, not a signed verification record. */
export function exportSelectedRows(table: string, rows: readonly unknown[]) {
  const blob = new Blob([JSON.stringify({format:'desk-table-selection',version:1,table,exportedAt:new Date().toISOString(),rows},null,2)], {type:'application/json'})
  const url = URL.createObjectURL(blob), link = document.createElement('a')
  link.href = url; link.download = `${table}-selected.json`; link.click()
  setTimeout(() => URL.revokeObjectURL(url), 1000)
}
