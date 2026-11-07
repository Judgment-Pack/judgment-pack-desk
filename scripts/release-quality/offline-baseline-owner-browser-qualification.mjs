// Offline measurement of the archived baseline plus its declared accounting/helper overlay.
import {fileURLToPath} from 'node:url'
import {join} from 'node:path'
import {qualifyOffline,baselineIdentity} from './offline-owner-browser-qualification.mjs'
export function baselineOptions({out,helper,browserBin,applicationRoot,publicRoot}) {
 return {out,helper,browserBin,applicationRoot,publicRoot,role:'baseline',baseCommit:baselineIdentity.commit,baseTree:baselineIdentity.tree,baselineReader:{path:join(applicationRoot,'scripts/release-quality/baseline-replay-report.mjs'),sha256:baselineIdentity.readerSha256}}
}
if(process.argv[1]===fileURLToPath(import.meta.url)){
 const [out,helper,browserBin,applicationRoot,publicRoot]=process.argv.slice(2)
 try{const result=await qualifyOffline(baselineOptions({out,helper,browserBin,applicationRoot,publicRoot}));process.stdout.write(JSON.stringify({provenance:result.provenance,state:result.state,stage:result.stage,reason:result.reason})+'\n');if(result.state!=='complete')process.exitCode=1}catch{process.stderr.write('offline baseline qualification custody/setup refused\n');process.exitCode=1}
}
