/**
 * Ed25519 verification in TypeScript (RFC 8032 §5.1.7), for a browser whose
 * WebCrypto does not speak Ed25519 — Chrome before 137 among them — and as a
 * second implementation beside it. Verification only: no key is generated and
 * nothing is signed here. The group equation checked is the one Go's
 * `crypto/ed25519` checks, `[S]B = R + [k]A` without the cofactor, with the
 * encodings held canonical: a `y` at or past the field order and an `S` at or
 * past the group order are refused. SHA-512 comes from WebCrypto, which every
 * supported browser has.
 *
 * It answers to the corpus vectors in `fixtures/ed25519-vectors.json`, which
 * were made by an independent implementation, and to the gateway's own
 * receipts and seals, through the fake gateway the run tests sign under the
 * corpus test seed.
 */

const P = (1n << 255n) - 19n
const L = (1n << 252n) + 27742317777372353535851937790883648493n
const D = mod(-121665n * inverse(121666n))
const SQRT_M1 = pow(2n, (P - 1n) / 4n)
const BASE_Y = mod(4n * inverse(5n))

function mod(a: bigint): bigint {
  const r = a % P
  return r < 0n ? r + P : r
}

function pow(base: bigint, exponent: bigint): bigint {
  let result = 1n
  let b = mod(base)
  let e = exponent
  while (e > 0n) {
    if (e & 1n) result = (result * b) % P
    b = (b * b) % P
    e >>= 1n
  }
  return result
}

function inverse(a: bigint): bigint {
  return pow(a, P - 2n)
}

/** A point in extended twisted Edwards coordinates (X, Y, Z, T), a = -1. */
interface Point {
  x: bigint
  y: bigint
  z: bigint
  t: bigint
}

const IDENTITY: Point = { x: 0n, y: 1n, z: 1n, t: 0n }

function add(p: Point, q: Point): Point {
  // add-2008-hwcd-3
  const a = mod((p.y - p.x) * (q.y - q.x))
  const b = mod((p.y + p.x) * (q.y + q.x))
  const c = mod(2n * p.t * q.t * D)
  const d = mod(2n * p.z * q.z)
  const e = b - a
  const f = d - c
  const g = d + c
  const h = b + a
  return { x: mod(e * f), y: mod(g * h), z: mod(f * g), t: mod(e * h) }
}

function double(p: Point): Point {
  return add(p, p)
}

function multiply(p: Point, scalar: bigint): Point {
  let result = IDENTITY
  let addend = p
  let k = scalar
  while (k > 0n) {
    if (k & 1n) result = add(result, addend)
    addend = double(addend)
    k >>= 1n
  }
  return result
}

function recoverX(y: bigint, sign: bigint): bigint | null {
  const y2 = mod(y * y)
  const u = mod(y2 - 1n)
  const v = mod(D * y2 + 1n)
  // x = (u/v)^((p+3)/8), then corrected by sqrt(-1) where needed.
  let x = mod(pow(u * inverse(v), (P + 3n) / 8n))
  const vx2 = mod(v * x * x)
  if (vx2 !== u) {
    if (vx2 !== mod(-u)) return null
    x = mod(x * SQRT_M1)
  }
  if (x === 0n && sign === 1n) return null
  if ((x & 1n) !== sign) x = mod(-x)
  return x
}

function bytesToBigIntLE(bytes: Uint8Array): bigint {
  let value = 0n
  for (let i = bytes.length - 1; i >= 0; i -= 1) value = (value << 8n) | BigInt(bytes[i]!)
  return value
}

/** Decode a 32-byte point; null where the encoding is not a canonical point. */
function decodePoint(bytes: Uint8Array): Point | null {
  if (bytes.length !== 32) return null
  const raw = bytesToBigIntLE(bytes)
  const sign = raw >> 255n
  const y = raw & ((1n << 255n) - 1n)
  if (y >= P) return null
  const x = recoverX(y, sign)
  if (x === null) return null
  return { x, y, z: 1n, t: mod(x * y) }
}

function encodePoint(p: Point): Uint8Array {
  const zi = inverse(p.z)
  const x = mod(p.x * zi)
  const y = mod(p.y * zi)
  const out = new Uint8Array(32)
  let v = y
  for (let i = 0; i < 32; i += 1) {
    out[i] = Number(v & 0xffn)
    v >>= 8n
  }
  out[31] = out[31]! | Number((x & 1n) << 7n)
  return out
}

let base: Point | null = null
function basePoint(): Point {
  if (base === null) {
    const x = recoverX(BASE_Y, 0n)
    if (x === null) throw new Error('the Ed25519 base point could not be recovered')
    base = { x, y: BASE_Y, z: 1n, t: mod(x * BASE_Y) }
  }
  return base
}

function equal(a: Uint8Array, b: Uint8Array): boolean {
  if (a.length !== b.length) return false
  let diff = 0
  for (let i = 0; i < a.length; i += 1) diff |= a[i]! ^ b[i]!
  return diff === 0
}

/** Verify `signature` (64 bytes) over `message` under `publicKey` (32 bytes). */
export async function verifyEd25519Pure(publicKey: Uint8Array, message: Uint8Array, signature: Uint8Array): Promise<boolean> {
  if (publicKey.length !== 32 || signature.length !== 64) return false
  const a = decodePoint(publicKey)
  const r = decodePoint(signature.subarray(0, 32))
  if (a === null || r === null) return false
  const s = bytesToBigIntLE(signature.subarray(32, 64))
  if (s >= L) return false
  const input = new Uint8Array(64 + message.length)
  input.set(signature.subarray(0, 32), 0)
  input.set(publicKey, 32)
  input.set(message, 64)
  const hash = new Uint8Array(await crypto.subtle.digest('SHA-512', input as BufferSource))
  const k = bytesToBigIntLE(hash) % L
  const left = multiply(basePoint(), s)
  const right = add(r, multiply(a, k))
  return equal(encodePoint(left), encodePoint(right))
}
