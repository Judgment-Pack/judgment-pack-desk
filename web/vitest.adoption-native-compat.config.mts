// Derived from the existing reviewed web fixture config; canonical is unchanged.
import {defineConfig} from 'vitest/config'
import base from './vitest.config'
import {fileURLToPath} from 'node:url'
import {resolve} from 'node:path'
const root=fileURLToPath(new URL('.',import.meta.url))
const output=process.env.DESK_RECOVERY_GUARDED_OUTPUT
if(!output)throw Error('The reviewed outer guard must select the owned output directory')
export default defineConfig({
 ...base,root,cacheDir:resolve(output,'vite-cache'),
 test:{...base.test,
  include:["src/assistant/serverRuns/client.test.ts", "src/assistant/serverRuns/controller.test.ts", "src/assistant/serverRuns/decode.test.ts", "src/chat/ChatPanel.runtimeApproval.test.tsx", "src/assistant/runContext.test.ts", "src/assistant/runtimeIdentityParity.test.ts", "src/assistant/report.test.ts", "src/assistant/enforcement.test.ts", "src/chat/RunPresentation.test.tsx"],
  cache:false,fsModuleCache:false,fsModuleCachePath:resolve(output,'fs-module-cache'),
  attachmentsDir:resolve(output,'attachments'),coverage:{enabled:false},
  bail:1,maxWorkers:1,fileParallelism:false,
  reporters:['json'],outputFile:resolve(output,'vitest-report.json'),
 },
})
