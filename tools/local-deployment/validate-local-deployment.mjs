import { execFileSync } from 'node:child_process'
import { readFile } from 'node:fs/promises'
import { existsSync } from 'node:fs'
import path from 'node:path'
import YAML from 'yaml'

const workspaceRoot = path.resolve(import.meta.dirname, '..', '..')
const composePath = path.join(workspaceRoot, 'deploy', 'docker-compose.yml')
const examplePath = path.join(workspaceRoot, 'deploy', '.env.example')

const compose = YAML.parse(await readFile(composePath, 'utf8'))
const composeSource = await readFile(composePath, 'utf8')
const example = await readFile(examplePath, 'utf8')

const requiredServices = [
  'postgres',
  'redis',
  'mqtt',
  'sub2api',
  'device_platform',
  'voice_gateway',
  'admin_web',
  'sprout_local_volume_init',
]

for (const serviceName of requiredServices) {
  if (!compose.services?.[serviceName]) {
    throw new Error(`Missing required Compose service: ${serviceName}`)
  }
}

function requireVolume(serviceName, target) {
  const service = compose.services[serviceName]
  const volumes = Array.isArray(service?.volumes) ? service.volumes : []
  if (!volumes.some((volume) => String(volume).endsWith(`:${target}`))) {
    throw new Error(`${serviceName} must mount a persistent volume at ${target}`)
  }
}

function requireHealthcheck(serviceName, endpoint) {
  const test = compose.services[serviceName]?.healthcheck?.test
  if (!Array.isArray(test) || !test.some((part) => String(part).includes(endpoint))) {
    throw new Error(`${serviceName} healthcheck must probe ${endpoint}`)
  }
}

requireVolume('device_platform', '/srv/releases')
requireVolume('device_platform', '/var/lib/sprout-upgrades')
requireHealthcheck('device_platform', '/readyz')
requireHealthcheck('voice_gateway', '/readyz')

const volumeInit = compose.services.sprout_local_volume_init
if (
  volumeInit.user !== '0:0' ||
  !String(volumeInit.command?.at(-1)).includes('chown 65532:65532')
) {
  throw new Error('sprout_local_volume_init must prepare runtime-owned writable volumes')
}

const deviceDependsOn = compose.services.device_platform.depends_on ?? {}
if (
  deviceDependsOn.sprout_local_volume_init?.condition !==
  'service_completed_successfully'
) {
  throw new Error('device_platform must wait for volume initialization to complete')
}

for (const volumeName of ['sprout_local_releases', 'sprout_local_upgrades']) {
  if (!Object.hasOwn(compose.volumes ?? {}, volumeName)) {
    throw new Error(`Missing declared local volume: ${volumeName}`)
  }
}

const declaredVariables = new Set(
  example
    .split(/\r?\n/)
    .map((line) => line.match(/^([A-Z][A-Z0-9_]*)=/)?.[1])
    .filter(Boolean),
)

const referencedVariables = new Set(
  [...composeSource.matchAll(/\$\{(SPROUT_[A-Z0-9_]+)/g)].map((match) => match[1]),
)

const missingVariables = [...referencedVariables].filter(
  (variableName) => !declaredVariables.has(variableName),
)

if (missingVariables.length > 0) {
  throw new Error(`Undocumented local variables: ${missingVariables.join(', ')}`)
}

const localEnvironment = path.join(workspaceRoot, 'deploy', '.env')
if (existsSync(localEnvironment)) {
  if (process.env.CI === 'true') {
    throw new Error('deploy/.env must not be present in CI')
  }

  try {
    execFileSync('git', ['check-ignore', '--quiet', '--', 'deploy/.env'], {
      cwd: workspaceRoot,
      stdio: 'ignore',
    })
  } catch {
    throw new Error('deploy/.env must be ignored by Git')
  }
}
const certificateDirectory = path.join(workspaceRoot, 'deploy', 'mosquitto', 'certs')
if (process.env.CI === 'true' && existsSync(certificateDirectory)) {
  throw new Error('Generated MQTT certificates must not be committed')
}

console.log(
  `Validated ${requiredServices.length} local services and ${referencedVariables.size} variables`,
)
