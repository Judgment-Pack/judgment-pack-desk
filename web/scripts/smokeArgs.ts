export interface SmokeOptions {
  target: string
  facts?: string
  evidence?: string
  packId?: string
  traceHead: number
  expectKind?: string
  expectHandoff?: string
  matrix: boolean
  graphs: boolean
  secret?: string
  expectMatrixStatus?: string
  expectGraphStatus?: string
  graphDocument?: string
  graphFile?: string
}

/** Flags that stand alone; every other flag takes the argument after it. */
const FLAGS = new Set(['--matrix', '--graphs'])

export function parseSmokeArgs(argv: string[]): SmokeOptions {
  const [target, ...rest] = argv
  if (!target) throw new Error('the desk URL is required')
  const options: SmokeOptions = { target, traceHead: 5, matrix: false, graphs: false }
  for (let i = 0; i < rest.length; i += 1) {
    const flag = rest[i]!
    if (FLAGS.has(flag)) {
      if (flag === '--matrix') options.matrix = true
      if (flag === '--graphs') options.graphs = true
      continue
    }
    const value = rest[i + 1]
    if (value === undefined) throw new Error(`${flag} needs a value`)
    i += 1
    switch (flag) {
      case '--secret':
        options.secret = value
        break
      case '--facts':
        options.facts = value
        break
      case '--evidence':
        options.evidence = value
        break
      case '--pack':
        options.packId = value
        break
      case '--trace': {
        const traceHead = Number(value)
        if (value.trim() === '' || !Number.isInteger(traceHead) || traceHead < 0) {
          throw new Error('--trace requires a non-negative integer')
        }
        options.traceHead = traceHead
        break
      }
      case '--expect-kind':
        options.expectKind = value
        break
      case '--expect-handoff':
        options.expectHandoff = value
        break
      case '--expect-matrix-status':
        options.expectMatrixStatus = value
        options.matrix = true
        break
      case '--expect-graph-status':
        options.expectGraphStatus = value
        options.graphs = true
        break
      case '--graph-document':
        options.graphDocument = value
        break
      case '--graph-file':
        options.graphFile = value
        break
      default:
        throw new Error(`unknown flag ${flag}`)
    }
  }
  return options
}
