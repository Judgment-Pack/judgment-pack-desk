import { formatNumber, formattingLocale, msg } from '../i18n'
import type { CalculatorPin, CalculationLineage } from './mappingTypes'

export const calculatorLabel = (calculator: CalculatorPin) => msg('Calculator: {{name}} · {{version}}', {name: calculator.name, version: calculator.version})

export function calculationStatus(status: CalculationLineage['status']) {
 switch (status) {
  case 'computed': return msg('Calculated')
  case 'input-missing': return msg('Not calculated: an input was missing')
  case 'cannot-compute': return msg('Not calculated: the calculator could not compute it')
 }
}

/** Preserve the exact limit, including remainders, using localized unit names. */
export function calculationAge(seconds: number) {
 const units = [[86400, 'day'], [3600, 'hour'], [60, 'minute'], [1, 'second']] as const
 const parts: string[] = []
 for (const [size, unit] of units) {
  const value = Math.floor(seconds / size)
  if (value) parts.push(formatNumber(value, {style: 'unit', unit, unitDisplay: 'long'}))
  seconds %= size
 }
 return new Intl.ListFormat(formattingLocale(), {style: 'long', type: 'unit'}).format(parts)
}
