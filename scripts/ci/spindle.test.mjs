import assert from 'node:assert/strict';
import test from 'node:test';
import { cleanEnvironment, releaseContext, webBuildArguments, imageReference, sourceRevision, railway, waitForDeployment, runtimeSmoke } from './spindle.mjs';

const sha = 'a'.repeat(40);
const digest = 'b'.repeat(64);
const projectId = '11111111-1111-4111-8111-111111111111';
const environmentId = '22222222-2222-4222-8222-222222222222';
const web = '33333333-3333-4333-8333-333333333333';
const appview = '44444444-4444-4444-8444-444444444444';
const deploymentId = '55555555-5555-4555-8555-555555555555';
const base = { TANGLED_PIPELINE_KIND: 'push', TANGLED_REF_NAME: 'dev', TANGLED_REF: 'refs/heads/dev', TANGLED_SHA: sha, ATCR_NAMESPACE: 'samclemente.me', RAILWAY_PROJECT_ID: projectId,
  RAILWAY_DEVELOPMENT_ENVIRONMENT_ID: environmentId, RAILWAY_DEVELOPMENT_WEB_SERVICE_ID: web, RAILWAY_DEVELOPMENT_APPVIEW_SERVICE_ID: appview };
const context = releaseContext(base, sha);
const response = data => ({ ok: true, json: async () => ({ data }) });

test('release context rejects PRs, tags, feature refs and mismatched checkout', () => {
  for (const values of [
    { TANGLED_PIPELINE_KIND: 'manual' }, { TANGLED_PIPELINE_KIND: 'schedule' }, { TANGLED_PIPELINE_KIND: 'pull_request' },
    { TANGLED_PR_TARGET_BRANCH: 'main' }, { TANGLED_PR_SOURCE_SHA: sha }, { TANGLED_REF: 'refs/tags/dev' },
    { TANGLED_REF_NAME: 'feature/test', TANGLED_REF: 'refs/heads/feature/test' }, { TANGLED_SHA: 'a'.repeat(7) },
    { RAILWAY_DEVELOPMENT_WEB_SERVICE_ID: 'Web' }, { RAILWAY_DEVELOPMENT_APPVIEW_SERVICE_ID: web },
  ]) assert.throws(() => releaseContext({ ...base, ...values }, sha));
  assert.throws(() => releaseContext(base, 'b'.repeat(40)), /checkout/);
  assert.equal(context.appEnvironment, 'dev');
});

test('main uses explicit production selectors and prod image names', () => {
  const production = releaseContext({ ...base, TANGLED_REF_NAME: 'main', TANGLED_REF: 'refs/heads/main',
    RAILWAY_PRODUCTION_ENVIRONMENT_ID: environmentId, RAILWAY_PRODUCTION_WEB_SERVICE_ID: web, RAILWAY_PRODUCTION_APPVIEW_SERVICE_ID: appview }, sha);
  assert.equal(imageReference(production, 'web'), `atcr.io/samclemente.me/the-social-wire-web:prod-${sha}`);
  assert.throws(() => imageReference(production, 'other'));
});

test('source proof requires expected namespace, environment, full SHA and digest', () => {
  const image = `${imageReference(context, 'appview')}@sha256:${digest}`;
  assert.equal(sourceRevision(image, 'samclemente.me', 'appview', 'dev'), sha);
  for (const candidate of [imageReference(context, 'appview'), image.replace('dev-', 'prod-'), image.replace('samclemente.me', 'other.example'), image.replace(sha, 'short')]) {
    assert.throws(() => sourceRevision(candidate, 'samclemente.me', 'appview', 'dev'));
  }
});

test('container subprocess environments omit deployment credentials', () => {
  assert.deepEqual(cleanEnvironment({ PATH: '/bin', ATCR_DEVICE_SECRET: 'device-value', RAILWAY_TOKEN: 'project-token', RAILWAY_API_TOKEN: 'account-token', DOCKER_CONFIG: '/tmp/login' }), { PATH: '/bin', DOCKER_CONFIG: '/tmp/login' });
});

test('build arguments are public and cannot cross environment boundaries', () => {
  assert.ok(webBuildArguments({}, 'dev').includes('NEXT_PUBLIC_SITE_URL=https://testing.thesocialwire.app'));
  assert.ok(webBuildArguments({}, 'prod').includes('NEXT_PUBLIC_SOCIALWIRE_API_URL=https://api.thesocialwire.app'));
  for (const extras of [{ SECRET: 'value' }, { NEXT_PUBLIC_SITE_URL: 'https://attacker.invalid' }, { NEXT_PUBLIC_FEATURE: true }, { NEXT_PUBLIC_FEATURE: 'value\nother' }]) {
    assert.throws(() => webBuildArguments({ SPINDLE_WEB_BUILD_ARGS: JSON.stringify(extras) }, 'prod'));
  }
  assert.ok(webBuildArguments({ SPINDLE_WEB_BUILD_ARGS: '{"NEXT_PUBLIC_FEATURE":"true"}' }, 'prod').includes('NEXT_PUBLIC_FEATURE=true'));
});

test('Railway API uses one token type and does not expose error response bodies', async () => {
  let observed;
  const request = async (url, options) => { observed = { url, options }; return response({ accepted: true }); };
  await railway('query Test { test }', {}, { RAILWAY_TOKEN: 'project-token' }, request);
  assert.equal(observed.options.headers['Project-Access-Token'], 'project-token');
  assert.equal(observed.options.headers.Authorization, undefined);
  await railway('query Test { test }', {}, { RAILWAY_API_TOKEN: 'account-token' }, request);
  assert.equal(observed.options.headers.Authorization, 'Bearer account-token');
  await assert.rejects(() => railway('', {}, {}, request), /exactly one/);
  await assert.rejects(() => railway('', {}, { RAILWAY_API_TOKEN: 'one', RAILWAY_TOKEN: 'two' }, request), /exactly one/);
  await assert.rejects(() => railway('', {}, { RAILWAY_TOKEN: 'project-token' }, async () => ({ ok: true, json: async () => ({ errors: [{ message: 'sensitive response' }] }) })), error => !error.message.includes('sensitive'));
});

test('deployment polling follows the exact deployment through SUCCESS', async () => {
  const statuses = ['QUEUED', 'DEPLOYING', 'SUCCESS'];
  let pauses = 0;
  const result = await waitForDeployment(deploymentId, context, appview, { environment: { RAILWAY_TOKEN: 'project-token' },
    request: async (_url, options) => {
      assert.equal(JSON.parse(options.body).variables.id, deploymentId);
      return response({ deployment: { id: deploymentId, projectId, environmentId, serviceId: appview, status: statuses.shift() } });
    }, pause: async () => { pauses++; } });
  assert.equal(result.status, 'SUCCESS');
  assert.equal(pauses, 2);
});

test('failed, sleeping, pending approval and mismatched deployments never count as success', async () => {
  for (const status of ['FAILED', 'CRASHED', 'SLEEPING', 'NEEDS_APPROVAL', 'SKIPPED']) {
    await assert.rejects(() => waitForDeployment(deploymentId, context, appview, { environment: { RAILWAY_TOKEN: 'project-token' }, request: async () => response({ deployment: { id: deploymentId, projectId, environmentId, serviceId: appview, status } }) }), new RegExp(status));
  }
  await assert.rejects(() => waitForDeployment(deploymentId, context, appview, { environment: { RAILWAY_TOKEN: 'project-token' }, request: async () => response({ deployment: { id: deploymentId, projectId, environmentId, serviceId: web, status: 'SUCCESS' } }) }), /identity mismatch/);
});

test('polling timeout preserves the submitted deployment identity without retrying a mutation', async () => {
  let calls = 0;
  await assert.rejects(() => waitForDeployment(deploymentId, context, appview, { environment: { RAILWAY_TOKEN: 'project-token' }, attempts: 2, pause: async () => {},
    request: async (_url, options) => {
      assert.ok(JSON.parse(options.body).query.startsWith('query '));
      calls++;
      return response({ deployment: { id: deploymentId, projectId, environmentId, serviceId: appview, status: 'DEPLOYING' } });
    } }), /do not submit a duplicate/);
  assert.equal(calls, 2);
});

function fakeRuntime({ startup = ['200'], processExit = false, failedMetadata = false } = {}) {
  const calls = [];
  let paused = false;
  let resumed = false;
  const execute = (command, args) => {
    assert.equal(command, 'docker');
    calls.push(args);
    if (args[0] === 'inspect') return processExit ? 'false' : 'true';
    if (args[0] === 'exec' && args[2] === 'curl') {
      const url = args.at(-1);
      if (url.endsWith('/readyz')) {
        if (paused) return '503';
        if (resumed) return '200';
        return startup.length > 1 ? startup.shift() : startup[0];
      }
      if (url.endsWith('/ios-client-metadata.json') && failedMetadata) return '500';
      return url.endsWith('getSidebar') ? '401' : '200';
    }
    if (args[0] === 'exec' && args[2] === 'sh') return '42';
    if (args[0] === 'exec' && args[2] === 'kill') {
      if (args[3] === '-STOP') paused = true;
      if (args[3] === '-CONT') { paused = false; resumed = true; }
    }
    return '';
  };
  return { calls, execute };
}

test('runtime smoke isolates fixture resources, probes packaged services and tests media readiness loss', async () => {
  const fake = fakeRuntime({ startup: ['503', '200'] });
  let pauses = 0;
  await runtimeSmoke('fixture-runtime:tag', { execute: fake.execute, pause: async () => { pauses++; } });
  const launch = fake.calls.find(args => args[0] === 'run');
  assert.equal(launch[launch.indexOf('--network') + 1], 'none');
  assert.ok(!launch.includes('--publish') && !launch.includes('-p'));
  for (const fixture of ['PODCASTS_ENABLED=true', 'SOCIAL_DRAFTS_ENABLED=true', 'TOOLS_ENABLED=true']) assert.ok(launch.includes(fixture));
  assert.ok(!launch.some(value => value.startsWith('ENABLE_THIN_APPVIEW=') || value.startsWith('BIND_HOST=')), 'Smoke must exercise image defaults.');
  assert.ok(!launch.some(value => /^(ATCR_DEVICE_SECRET|RAILWAY_TOKEN|RAILWAY_API_TOKEN)=/.test(value)));
  assert.ok(fake.calls.some(args => args[0] === 'exec' && args[3] === '-STOP'));
  assert.ok(fake.calls.some(args => args[0] === 'exec' && args[3] === '-CONT'));
  assert.ok(fake.calls.some(args => args.at(-1) === 'http://127.0.0.1:9080/startupz'));
  assert.equal(pauses, 1);
  assert.ok(fake.calls.some(args => args[0] === 'rm' && args[1] === '--force'));
  assert.ok(fake.calls.some(args => args[0] === 'volume' && args[1] === 'rm'));
});

test('runtime smoke bounds failed startup and cleans container and volume after probe errors', async () => {
  const fake = fakeRuntime({ startup: ['503'] });
  await assert.rejects(() => runtimeSmoke('fixture-runtime:tag', { execute: fake.execute, pause: async () => {}, attempts: 2 }), /bounded deadline/);
  assert.equal(fake.calls.filter(args => args[0] === 'inspect').length, 2);
  assert.ok(fake.calls.some(args => args[0] === 'rm'));
  assert.ok(fake.calls.some(args => args[0] === 'volume' && args[1] === 'rm'));
  const metadata = fakeRuntime({ failedMetadata: true });
  await assert.rejects(() => runtimeSmoke('fixture-runtime:tag', { execute: metadata.execute }), /received 500/);
  assert.ok(metadata.calls.some(args => args[0] === 'rm'));
});

test('runtime smoke rejects an exited container and always resumes a stopped processor during failure cleanup', async () => {
  const exited = fakeRuntime({ startup: ['503'], processExit: true });
  await assert.rejects(() => runtimeSmoke('fixture-runtime:tag', { execute: exited.execute }), /exited/);
  const fake = fakeRuntime();
  let stopped = false;
  const execute = (command, args, options) => {
    if (args[0] === 'exec' && args[3] === '-STOP') stopped = true;
    if (stopped && args.at(-1) === 'http://127.0.0.1:8081/readyz') return '200';
    return fake.execute(command, args, options);
  };
  await assert.rejects(() => runtimeSmoke('fixture-runtime:tag', { execute }), /expected HTTP 503/);
  assert.ok(fake.calls.some(args => args[0] === 'exec' && args[3] === '-CONT'));
  assert.ok(fake.calls.some(args => args[0] === 'volume' && args[1] === 'rm'));
});
