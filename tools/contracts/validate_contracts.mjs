import { readFile, readdir } from 'node:fs/promises';
import { dirname, extname, join, resolve } from 'node:path';
import { fileURLToPath } from 'node:url';
import Ajv2020 from 'ajv/dist/2020.js';
import addFormats from 'ajv-formats';
import { parse as parseYaml } from 'yaml';

const toolsRoot = dirname(fileURLToPath(import.meta.url));
const repositoryRoot = resolve(toolsRoot, '..', '..');
const contractsRoot = resolve(repositoryRoot, 'packages', 'contracts');
const mqttTopicsPath = resolve(contractsRoot, 'mqtt', 'topics.example.json');
const mqttReadmePath = resolve(contractsRoot, 'mqtt', 'README.md');

const contractNamespace = 'https://sprout.example/contracts/';
const expectedSchemaVersion = '1.0.0';
const schemaFiles = [];
const fixtureFiles = [];
const responseSchemaPaths = [
  resolve(contractsRoot, 'schemas', 'envelope.schema.json'),
  resolve(contractsRoot, 'errors', 'error_response.schema.json'),
];
const responseFixturePaths = [
  resolve(contractsRoot, 'schemas', 'envelope.example.json'),
  resolve(contractsRoot, 'errors', 'error_response.example.json'),
];
const openAPIpaths = [
  resolve(
    repositoryRoot,
    'services',
    'device_platform',
    'internal',
    'contracts',
    'http',
    'openapi.yaml',
  ),
  resolve(
    repositoryRoot,
    'services',
    'voice_gateway',
    'contracts',
    'internal-api',
    'openapi.yaml',
  ),
];

async function collectJsonFiles(directory) {
  const entries = await readdir(directory, { withFileTypes: true });
  for (const entry of entries) {
    const fullPath = join(directory, entry.name);
    if (entry.isDirectory()) {
      await collectJsonFiles(fullPath);
      continue;
    }
    if (extname(entry.name) !== '.json') {
      continue;
    }
    if (entry.name.endsWith('.schema.json')) {
      schemaFiles.push(fullPath);
      continue;
    }
    if (entry.name.endsWith('.example.json') || entry.name === 'device_capabilities.json') {
      fixtureFiles.push(fullPath);
      continue;
    }
    throw new Error(`${fullPath}: contract JSON files must be schemas or examples`);
  }
}

await collectJsonFiles(contractsRoot);

const schemas = [];

for (const filePath of schemaFiles.sort()) {
  const content = await readFile(filePath, 'utf8');
  let document;
  try {
    document = JSON.parse(content);
  } catch (error) {
    throw new Error(`${filePath}: invalid JSON: ${error.message}`);
  }

  if (
    document.$schema !== 'https://json-schema.org/draft/2020-12/schema' ||
    typeof document.$id !== 'string' ||
    !document.$id.startsWith(contractNamespace)
  ) {
    throw new Error(`${filePath}: expected a draft 2020-12 schema in the contract namespace`);
  }

  schemas.push({ filePath, schema: document });
}

for (const filePath of fixtureFiles.sort()) {
  const content = await readFile(filePath, 'utf8');
  try {
    JSON.parse(content);
  } catch (error) {
    throw new Error(`${filePath}: invalid JSON: ${error.message}`);
  }
}

const ajv = new Ajv2020({
  allErrors: true,
  strict: true,
});
addFormats(ajv);

for (const { filePath, schema } of schemas) {
  try {
    ajv.addSchema(schema, schema.$id);
  } catch (error) {
    throw new Error(`${filePath}: failed to register schema: ${error.message}`);
  }
}

for (const { filePath, schema } of schemas) {
  try {
    ajv.compile(schema);
  } catch (error) {
    throw new Error(`${filePath}: invalid schema: ${error.message}`);
  }
}

function fixtureSchemaPath(fixturePath) {
  if (fixturePath.endsWith('device_capabilities.json')) {
    return fixturePath.replace(/device_capabilities\.json$/, 'device_capabilities.schema.json');
  }
  return fixturePath.replace(/\.example\.json$/, '.schema.json');
}

for (const fixturePath of fixtureFiles.sort()) {
  const schemaPath = fixtureSchemaPath(fixturePath);
  const registeredSchema = schemas.find(({ filePath }) => filePath === schemaPath);
  if (!registeredSchema) {
    throw new Error(`${fixturePath}: matching schema not found at ${schemaPath}`);
  }

  const validate = ajv.getSchema(registeredSchema.schema.$id);
  if (!validate) {
    throw new Error(`${fixturePath}: schema was not compiled`);
  }

  const document = JSON.parse(await readFile(fixturePath, 'utf8'));
  if (!validate(document)) {
    throw new Error(`${fixturePath}: ${ajv.errorsText(validate.errors)}`);
  }
}

await validateResponseContractVersions();
await validateMQTTTransportContract();

async function validateResponseContractVersions() {
  for (const filePath of responseSchemaPaths) {
    const schema = JSON.parse(await readFile(filePath, 'utf8'));
    const version = schema.properties?.schema_version?.const;
    if (version !== expectedSchemaVersion) {
      throw new Error(
        `${filePath}: expected response schema version ${expectedSchemaVersion}`,
      );
    }
  }

  for (const filePath of responseFixturePaths) {
    const fixture = JSON.parse(await readFile(filePath, 'utf8'));
    if (fixture.schema_version !== expectedSchemaVersion) {
      throw new Error(
        `${filePath}: expected response fixture version ${expectedSchemaVersion}`,
      );
    }
  }

  for (const filePath of openAPIpaths) {
    const document = parseYaml(await readFile(filePath, 'utf8'));
    const apiVersion = document.info?.version;
    const envelopeVersion =
      document.components?.schemas?.ResponseEnvelope?.properties?.schema_version
        ?.const;
    if (apiVersion !== expectedSchemaVersion || envelopeVersion !== expectedSchemaVersion) {
      throw new Error(
        `${filePath}: expected API and response envelope version ${expectedSchemaVersion}`,
      );
    }
  }
}

async function validateMQTTTransportContract() {
  const topicDocument = JSON.parse(await readFile(mqttTopicsPath, 'utf8'));
  const topics = topicDocument.topics;
  if (!Array.isArray(topics)) {
    throw new Error(`${mqttTopicsPath}: topics must be an array`);
  }

  const topicNames = topics.map(({ topic }) => topic);
  const requiredTopics = [
    'sprout/v1/devices/{device_id}/runtime',
    'sprout/v1/devices/{device_id}/commands',
    'sprout/v1/devices/{device_id}/commands/ack',
  ];
  for (const requiredTopic of requiredTopics) {
    if (!topicNames.includes(requiredTopic)) {
      throw new Error(`${mqttTopicsPath}: missing implemented topic ${requiredTopic}`);
    }
  }
  if (topicNames.includes('sprout/v1/devices/{device_id}/interaction')) {
    throw new Error(
      `${mqttTopicsPath}: interaction events travel in the runtime heartbeat, `
        + 'not on a separate MQTT topic',
    );
  }

  const readme = await readFile(mqttReadmePath, 'utf8');
  for (const topicName of requiredTopics) {
    if (!readme.includes(`\`${topicName}\``)) {
      throw new Error(`${mqttReadmePath}: missing documented topic ${topicName}`);
    }
  }
}

console.log(
  `Validated ${schemas.length} schemas, ${fixtureFiles.length} contract examples, `
    + `and response versions in ${openAPIpaths.length} services.`,
);
