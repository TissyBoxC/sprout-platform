import assert from 'node:assert/strict'
import { readFile } from 'node:fs/promises'
import path from 'node:path'

const workspaceRoot = path.resolve(import.meta.dirname, '..', '..')
const scriptPath = path.join(
  workspaceRoot,
  'deploy',
  'local_version_worker',
  'snapshot-worker.sh',
)
const composePath = path.join(workspaceRoot, 'deploy', 'docker-compose.yml')

const script = await readFile(scriptPath, 'utf8')
const compose = await readFile(composePath, 'utf8')

for (const required of [
  'status.json',
  'releases.json',
  'SPROUT_PLATFORM_VERSION',
  'SPROUT_SUB2API_VERSION',
  'SPROUT_LOCAL_VERSION_CHECK_INTERVAL_SECONDS',
]) {
  assert.ok(script.includes(required), `snapshot worker must support ${required}`)
}

assert.ok(
  script.includes('write_json_atomic "$root_dir/status.json"'),
  'snapshot worker must atomically publish status.json',
)
assert.ok(
  script.includes('write_json_atomic "$root_dir/releases.json"'),
  'snapshot worker must atomically publish releases.json',
)
assert.ok(
  script.includes('rm -f "$request_file"'),
  'snapshot worker must consume check requests',
)
const localWorkerBlock = compose.slice(
  compose.indexOf('  sprout_local_version_worker:'),
  compose.indexOf('\nvolumes:', compose.indexOf('  sprout_local_version_worker:')),
)
assert.ok(localWorkerBlock.includes('sprout_local_version_worker:'))
assert.ok(localWorkerBlock.includes('user: "65532:65532"'))
assert.ok(localWorkerBlock.includes('sprout_local_upgrades:/var/lib/sprout-upgrades'))
assert.ok(!localWorkerBlock.includes('docker.sock'))

const deviceBlock = compose.slice(
  compose.indexOf('  device_platform:'),
  compose.indexOf('  voice_gateway:', compose.indexOf('  device_platform:')),
)
assert.ok(
  deviceBlock.includes('DEVICE_PLATFORM_SERVICE_VERSION_UPGRADE_EXECUTION: "false"'),
  'local backend must keep upgrade execution disabled',
)
assert.ok(
  deviceBlock.includes('sprout_local_version_worker:'),
  'device platform must depend on the local snapshot worker',
)

console.log('local version worker contract test passed')
