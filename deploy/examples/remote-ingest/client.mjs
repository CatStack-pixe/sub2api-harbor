import { createHash, generateKeyPairSync, sign } from 'node:crypto'
import { hostname } from 'node:os'

const baseUrl = process.env.INGEST_URL.replace(/\/$/, '')
const accessHeaders = {
  'CF-Access-Client-Id': process.env.CF_ACCESS_CLIENT_ID,
  'CF-Access-Client-Secret': process.env.CF_ACCESS_CLIENT_SECRET
}

async function post(path, body, headers = {}) {
  const response = await fetch(baseUrl + path, {
    method: 'POST',
    headers: { ...accessHeaders, 'Content-Type': 'application/json', ...headers },
    body
  })
  const payload = await response.json()
  if (!response.ok) throw new Error(payload.message ?? `HTTP ${response.status}`)
  return payload.data
}

const { privateKey, publicKey } = generateKeyPairSync('ed25519')
const publicDer = publicKey.export({ type: 'spki', format: 'der' })
const enrollment = await post('/api/v1/remote-ingest/enroll', JSON.stringify({
  registration_token: process.env.REGISTRATION_TOKEN,
  machine_name: hostname(),
  public_key: publicDer.subarray(-32).toString('base64')
}))
const challenge = await post('/api/v1/remote-ingest/handshakes', JSON.stringify({ client_id: enrollment.client_id }))
const body = JSON.stringify({
  external_id: `remote-${Math.floor(Date.now() / 1000)}`,
  name: 'remote-openai', platform: 'openai', base_url: process.env.REMOTE_BASE_URL,
  api_key: process.env.REMOTE_API_KEY, group_name: process.env.REMOTE_GROUP_NAME,
  test_model: 'gpt-4.1-mini', concurrency: 1, priority: 0, rate_multiplier: 1
})
const timestamp = String(Math.floor(Date.now() / 1000))
const canonical = [
  'sub2api-remote-ingest-v1', enrollment.client_id, challenge.challenge_id,
  challenge.nonce, timestamp, createHash('sha256').update(body).digest('hex')
].join('\n')
const signature = sign(null, Buffer.from(canonical), privateKey).toString('base64')
const delivery = await post('/api/v1/remote-ingest/accounts', body, {
  'X-Remote-Client-Id': enrollment.client_id,
  'X-Remote-Challenge-Id': challenge.challenge_id,
  'X-Remote-Timestamp': timestamp,
  'X-Remote-Signature': signature
})
console.log(JSON.stringify(delivery))
