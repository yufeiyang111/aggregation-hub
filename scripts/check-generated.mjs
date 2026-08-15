import { spawnSync } from 'node:child_process';
import { existsSync, mkdtempSync, readFileSync, rmSync } from 'node:fs';
import { tmpdir } from 'node:os';
import { fileURLToPath } from 'node:url';
import path from 'node:path';

const ROOT = path.resolve(path.dirname(fileURLToPath(import.meta.url)), '..');
const OPENAPI_PATH = path.join(ROOT, 'contracts', 'control-plane.openapi.yaml');
const FIXTURE_PATH = path.join(ROOT, 'contracts', 'fixtures', 'runtime.json');
const REDOCLY_CLI_PATH = path.join(ROOT, 'apps', 'desktop', 'node_modules', '@redocly', 'cli', 'bin', 'cli.js');
const RUNTIME_KEYS = ['data_plane_url', 'last_error', 'started_at', 'state', 'version'];
const RUNTIME_STATES = ['starting', 'running', 'degraded', 'stopped', 'failed'];
const DATA_PLANE_URL_PATTERN = String.raw`^http://127[.]0[.]0[.]1(?::(?:[1-9][0-9]{0,3}|[1-5][0-9]{4}|6[0-4][0-9]{3}|65[0-4][0-9]{2}|655[0-2][0-9]|6553[0-5]))?$`;
const STARTED_AT_PATTERN = String.raw`^\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}Z$`;
const LAST_ERROR_PATTERN = String.raw`^[^\r\n]*$`;
const LAST_ERROR_MAX_LENGTH = 2000;
const MANAGEMENT_TOKEN_HEADER = 'X-Aggregation-Hub-Management-Token';
const SENSITIVE_PROPERTY_NAME = /^(?:api[_-]?key|access[_-]?token|bearer[_-]?token|client[_-]?secret|credential|password|secret)$/iu;

function assertCondition(condition, message) {
  if (!condition) {
    throw new Error(`[contract] ${message}`);
  }
}

function readJson(filePath) {
  try {
    return JSON.parse(readFileSync(filePath, 'utf8'));
  } catch (error) {
    throw new Error(`[contract] 无法解析 JSON ${path.relative(ROOT, filePath)}: ${error.message}`);
  }
}

function assertExactKeys(value, expectedKeys, source) {
  const actualKeys = Object.keys(value).sort();
  const normalizedExpectedKeys = [...expectedKeys].sort();
  assertCondition(
    JSON.stringify(actualKeys) === JSON.stringify(normalizedExpectedKeys),
    `${source} 的字段必须严格为：${normalizedExpectedKeys.join(', ')}`,
  );
}

function assertStringUnionType(schema, source) {
  assertCondition(Array.isArray(schema?.type), `${source}.type 必须声明 string/null 联合类型`);
  assertCondition(
    JSON.stringify([...schema.type].sort()) === JSON.stringify(['null', 'string']),
    `${source}.type 必须仅为 string/null 联合类型`,
  );
}

function isSafeCredentialSchema(key, child) {
  if (key !== 'credential' || child === null || typeof child !== 'object' || Array.isArray(child)) {
    return false;
  }
  if (child.writeOnly === true) {
    return child.type === 'string'
      && child.minLength === 1
      && Number.isInteger(child.maxLength)
      && child.maxLength > 0
      && child.maxLength <= 5120
      && !Object.hasOwn(child, 'default')
      && !Object.hasOwn(child, 'example')
      && !Object.hasOwn(child, 'examples');
  }
  if (child.readOnly === true) {
    return child.type === 'object'
      && child.additionalProperties === false
      && Array.isArray(child.required)
      && child.required.length === 1
      && child.required[0] === 'configured'
      && child.properties?.configured?.type === 'boolean'
      && child.properties?.masked_hint?.type === 'string';
  }
  return false;
}

function assertNoSensitivePropertyNames(value, source = 'OpenAPI') {
  if (Array.isArray(value)) {
    value.forEach((item, index) => assertNoSensitivePropertyNames(item, `${source}[${index}]`));
    return;
  }
  if (value === null || typeof value !== 'object') {
    return;
  }

  for (const [key, child] of Object.entries(value)) {
    assertCondition(!SENSITIVE_PROPERTY_NAME.test(key) || isSafeCredentialSchema(key, child), `${source} 含有不应出现在控制面契约中的敏感字段：${key}`);
    assertNoSensitivePropertyNames(child, `${source}.${key}`);
  }
}

function getRuntimeSchema(openapiDocument) {
  const schema = openapiDocument?.components?.schemas?.RuntimeStatus;
  assertCondition(schema !== null && typeof schema === 'object' && !Array.isArray(schema), '缺少 components.schemas.RuntimeStatus');
  return schema;
}

function assertOpenApiContract(openapiDocument) {
  assertCondition(openapiDocument !== null && typeof openapiDocument === 'object' && !Array.isArray(openapiDocument), 'OpenAPI 根节点必须是对象');
  assertCondition(openapiDocument.openapi === '3.1.0', 'OpenAPI 版本必须为 3.1.0');
  assertCondition(Array.isArray(openapiDocument.servers) && openapiDocument.servers.length > 0, 'OpenAPI 必须声明 server');

  for (const [index, server] of openapiDocument.servers.entries()) {
    assertCondition(server !== null && typeof server === 'object', `servers[${index}] 必须是对象`);
    let url;
    try {
      url = new URL(server.url);
    } catch (error) {
      throw new Error(`[contract] servers[${index}].url 不是有效 URL: ${error.message}`);
    }
    assertCondition(url.protocol === 'http:', `servers[${index}].url 必须使用 http`);
    assertCondition(url.hostname === '127.0.0.1', `servers[${index}].url 必须绑定 127.0.0.1`);
    assertCondition(url.username === '' && url.password === '', `servers[${index}].url 不得携带用户信息`);
    assertCondition(url.search === '' && url.hash === '', `servers[${index}].url 不得携带查询参数或片段`);
  }

  const runtimePath = openapiDocument.paths?.['/internal/v1/runtime'];
  const getRuntime = runtimePath?.get;
  assertCondition(getRuntime !== null && typeof getRuntime === 'object', '缺少 /internal/v1/runtime GET 操作');
  assertCondition(getRuntime.operationId === 'getRuntimeStatus', '必须使用 getRuntimeStatus operationId');
  assertCondition(
    getRuntime.responses?.['200']?.content?.['application/json']?.schema?.$ref === '#/components/schemas/RuntimeStatus',
    '运行时响应必须引用 RuntimeStatus',
  );
  assertCondition(getRuntime.responses?.['401'] !== undefined, '运行时接口必须声明 401 响应');
  assertCondition(
    Array.isArray(getRuntime.security)
      && getRuntime.security.some((requirement) => requirement?.ManagementToken !== undefined),
    '运行时接口必须要求 ManagementToken',
  );

  const managementTokenScheme = openapiDocument.components?.securitySchemes?.ManagementToken;
  assertCondition(managementTokenScheme?.type === 'apiKey', 'ManagementToken 必须是 apiKey 安全方案');
  assertCondition(managementTokenScheme.in === 'header', 'ManagementToken 必须通过请求头传递');
  assertCondition(managementTokenScheme.name === MANAGEMENT_TOKEN_HEADER, 'ManagementToken 请求头名称不正确');

  const schema = getRuntimeSchema(openapiDocument);
  assertCondition(schema.type === 'object', 'RuntimeStatus.type 必须为 object');
  assertCondition(schema.additionalProperties === false, 'RuntimeStatus 必须拒绝额外字段');
  assertCondition(JSON.stringify([...schema.required].sort()) === JSON.stringify([...RUNTIME_KEYS].sort()), 'RuntimeStatus.required 必须与 fixture 字段严格一致');
  assertExactKeys(schema.properties, RUNTIME_KEYS, 'RuntimeStatus.properties');

  const stateSchema = schema.properties.state;
  assertCondition(stateSchema?.type === 'string', 'RuntimeStatus.state.type 必须为 string');
  assertCondition(JSON.stringify(stateSchema.enum) === JSON.stringify(RUNTIME_STATES), 'RuntimeStatus.state.enum 必须与生命周期状态严格一致');

  const dataPlaneUrlSchema = schema.properties.data_plane_url;
  assertStringUnionType(dataPlaneUrlSchema, 'RuntimeStatus.data_plane_url');
  assertCondition(dataPlaneUrlSchema.pattern === DATA_PLANE_URL_PATTERN, 'RuntimeStatus.data_plane_url.pattern 必须约束回环 HTTP 地址及合法端口');

  const startedAtSchema = schema.properties.started_at;
  assertStringUnionType(startedAtSchema, 'RuntimeStatus.started_at');
  assertCondition(startedAtSchema.format === 'date-time', 'RuntimeStatus.started_at.format 必须为 date-time');
  assertCondition(startedAtSchema.pattern === STARTED_AT_PATTERN, 'RuntimeStatus.started_at.pattern 必须约束 UTC 的 ISO-8601 时间');

  const versionSchema = schema.properties.version;
  assertCondition(versionSchema?.type === 'string' && versionSchema.minLength === 1, 'RuntimeStatus.version 必须是非空字符串');

  const lastErrorSchema = schema.properties.last_error;
  assertStringUnionType(lastErrorSchema, 'RuntimeStatus.last_error');
  assertCondition(lastErrorSchema.minLength === 1, 'RuntimeStatus.last_error 最小长度必须为 1');
  assertCondition(lastErrorSchema.maxLength === LAST_ERROR_MAX_LENGTH, 'RuntimeStatus.last_error.maxLength 必须为 2000');
  assertCondition(lastErrorSchema.pattern === LAST_ERROR_PATTERN, 'RuntimeStatus.last_error.pattern 必须拒绝换行');

  assertNoSensitivePropertyNames(openapiDocument);
  return schema;
}

function assertRuntimeStatus(runtime, source, schema) {
  assertCondition(runtime !== null && typeof runtime === 'object' && !Array.isArray(runtime), `${source} 必须是 JSON 对象`);
  assertExactKeys(runtime, schema.required, source);

  assertCondition(typeof runtime.state === 'string' && schema.properties.state.enum.includes(runtime.state), `${source}.state 不属于允许的状态：${String(runtime.state)}`);

  if (runtime.data_plane_url !== null) {
    assertCondition(typeof runtime.data_plane_url === 'string', `${source}.data_plane_url 必须是字符串或 null`);
    assertCondition(new RegExp(schema.properties.data_plane_url.pattern, 'u').test(runtime.data_plane_url), `${source}.data_plane_url 不符合回环地址和端口约束`);
    let url;
    try {
      url = new URL(runtime.data_plane_url);
    } catch (error) {
      throw new Error(`[contract] ${source}.data_plane_url 不是有效 URL: ${error.message}`);
    }
    assertCondition(url.protocol === 'http:', `${source}.data_plane_url 必须使用 http`);
    assertCondition(url.hostname === '127.0.0.1', `${source}.data_plane_url 必须绑定 127.0.0.1`);
    assertCondition(url.username === '' && url.password === '', `${source}.data_plane_url 不得携带用户信息`);
    assertCondition(url.search === '' && url.hash === '', `${source}.data_plane_url 不得携带查询参数或片段`);
  }

  if (runtime.started_at !== null) {
    assertCondition(typeof runtime.started_at === 'string', `${source}.started_at 必须是 ISO 时间字符串或 null`);
    assertCondition(new RegExp(schema.properties.started_at.pattern, 'u').test(runtime.started_at), `${source}.started_at 必须为 UTC 的 ISO-8601 时间`);
    assertCondition(!Number.isNaN(Date.parse(runtime.started_at)), `${source}.started_at 不是有效日期`);
  }

  assertCondition(typeof runtime.version === 'string' && runtime.version.length >= schema.properties.version.minLength, `${source}.version 必须是非空字符串`);

  if (runtime.last_error !== null) {
    assertCondition(typeof runtime.last_error === 'string', `${source}.last_error 必须是字符串或 null`);
    assertCondition(runtime.last_error.length >= schema.properties.last_error.minLength, `${source}.last_error 不得为空字符串`);
    assertCondition(runtime.last_error.length <= schema.properties.last_error.maxLength, `${source}.last_error 超过长度上限 ${schema.properties.last_error.maxLength}`);
    assertCondition(new RegExp(schema.properties.last_error.pattern, 'u').test(runtime.last_error), `${source}.last_error 不得包含换行`);
  }
}

function expectFailure(label, callback, expectedMessage) {
  let caughtError = null;
  try {
    callback();
  } catch (error) {
    caughtError = error;
  }

  assertCondition(caughtError instanceof Error, `[self-test] ${label} 必须抛出 Error`);
  assertCondition(caughtError.message.startsWith('[contract]'), `[self-test] ${label} 错误前缀不正确：${caughtError.message}`);
  assertCondition(caughtError.message.includes(expectedMessage), `[self-test] ${label} 未拒绝预期字段：${caughtError.message}`);
}

function runRedocly(commandArguments, options = {}) {
  assertCondition(existsSync(REDOCLY_CLI_PATH), `缺少本地 Redocly CLI：${path.relative(ROOT, REDOCLY_CLI_PATH)}`);
  return spawnSync(process.execPath, [REDOCLY_CLI_PATH, ...commandArguments], {
    cwd: ROOT,
    encoding: 'utf8',
    windowsHide: true,
    ...options,
  });
}

function loadStructuredOpenApi() {
  const temporaryDirectory = mkdtempSync(path.join(tmpdir(), 'aggregation-hub-openapi-'));
  const bundlePath = path.join(temporaryDirectory, 'openapi.json');

  try {
    const result = runRedocly(['bundle', OPENAPI_PATH, '--output', bundlePath, '--ext', 'json'], {
      stdio: ['ignore', 'pipe', 'pipe'],
    });
    if (result.error) {
      throw new Error(`[contract] 无法执行 Redocly bundle: ${result.error.message}`);
    }
    assertCondition(result.status === 0, `Redocly bundle 失败，退出码：${String(result.status)}\n${result.stderr ?? ''}`);
    return readJson(bundlePath);
  } finally {
    rmSync(temporaryDirectory, { recursive: true, force: true });
  }
}

function runRedoclyLint() {
  const result = runRedocly(['lint', OPENAPI_PATH], { stdio: 'inherit' });
  if (result.error) {
    throw new Error(`[contract] 无法执行 Redocly lint: ${result.error.message}`);
  }
  assertCondition(result.status === 0, `Redocly lint 失败，退出码：${String(result.status)}`);
}

function main() {
  const args = new Set(process.argv.slice(2));
  const openapiDocument = loadStructuredOpenApi();
  const runtime = readJson(FIXTURE_PATH);
  const runtimeSchema = assertOpenApiContract(openapiDocument);

  if (args.has('--self-test')) {
    const invalidState = { ...runtime, state: 'booted' };
    expectFailure('非法 runtime state', () => assertRuntimeStatus(invalidState, 'invalid runtime fixture', runtimeSchema), 'invalid runtime fixture.state');

    const invalidPort = { ...runtime, data_plane_url: 'http://127.0.0.1:65536' };
    expectFailure('非法 Data Plane 端口', () => assertRuntimeStatus(invalidPort, 'invalid runtime fixture', runtimeSchema), 'invalid runtime fixture.data_plane_url');

    const invalidDate = { ...runtime, started_at: '2026-08-02T10:00:00+08:00' };
    expectFailure('非 UTC 格式的 started_at', () => assertRuntimeStatus(invalidDate, 'invalid runtime fixture', runtimeSchema), 'invalid runtime fixture.started_at');

    const invalidLastError = { ...runtime, last_error: `${'x'.repeat(LAST_ERROR_MAX_LENGTH)}\nsecret` };
    expectFailure('含换行的 last_error', () => assertRuntimeStatus(invalidLastError, 'invalid runtime fixture', runtimeSchema), 'invalid runtime fixture.last_error');

    const unexpectedSecret = { ...runtime, local_access_key: 'should-not-exist' };
    expectFailure('意外敏感字段', () => assertRuntimeStatus(unexpectedSecret, 'invalid runtime fixture', runtimeSchema), '字段必须严格为');

    const invalidOpenApi = JSON.parse(JSON.stringify(openapiDocument));
    invalidOpenApi.components.schemas.RuntimeStatus.properties.state.enum = ['booted'];
    expectFailure('非法 OpenAPI state enum', () => assertOpenApiContract(invalidOpenApi), 'RuntimeStatus.state.enum');

    const unsafeCredentialSchema = JSON.parse(JSON.stringify(openapiDocument));
    unsafeCredentialSchema.components.schemas.ProviderCreateInput.properties.credential = { type: 'string' };
    expectFailure('非受限凭据字段', () => assertOpenApiContract(unsafeCredentialSchema), '敏感字段：credential');

    console.log('[self-test] state、端口、时间、last_error、额外字段和 OpenAPI enum 均被拒绝。');
  }

  assertRuntimeStatus(runtime, 'runtime fixture', runtimeSchema);
  console.log(`[contract] runtime fixture 通过：${path.relative(ROOT, FIXTURE_PATH)}`);

  if (args.has('--redocly')) {
    runRedoclyLint();
    console.log('[contract] Redocly lint 通过。');
  }
}

try {
  main();
} catch (error) {
  console.error(error instanceof Error ? error.message : String(error));
  process.exitCode = 1;
}