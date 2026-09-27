// Native date/time fields edit the chosen schedule zone, independent of the
// browser's zone. Match Runner: skip gaps and select the first repeated time.
export function scheduleWallTime(instant:string,zone:string):string {
 try {
  const parts=new Intl.DateTimeFormat('en-CA',{timeZone:zone,year:'numeric',month:'2-digit',day:'2-digit',hour:'2-digit',minute:'2-digit',hourCycle:'h23'}).formatToParts(new Date(instant))
  const p=Object.fromEntries(parts.map(p=>[p.type,p.value]))
  return `${p.year}-${p.month}-${p.day}T${p.hour}:${p.minute}`
 } catch { return '' }
}
export function scheduleInstant(wall:string,zone:string):string|undefined {
 if(!/^\d{4}-\d{2}-\d{2}T\d{2}:\d{2}$/.test(wall))return
 const utc=Date.parse(wall+'Z');if(!Number.isFinite(utc))return
 const candidates=new Set<number>()
 for(let h=-36;h<=36;h+=6){const at=utc+h*3600000,local=scheduleWallTime(new Date(at).toISOString(),zone),offset=Date.parse(local+'Z')-at;if(Number.isFinite(offset))candidates.add(utc-offset)}
 const matches=[...candidates].filter(at=>scheduleWallTime(new Date(at).toISOString(),zone)===wall).sort((a,b)=>a-b)
 return matches[0]===undefined?undefined:new Date(matches[0]).toISOString()
}
