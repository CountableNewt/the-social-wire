import { execFileSync, spawnSync } from 'node:child_process';
import { randomUUID } from 'node:crypto';
import { mkdtempSync, rmSync } from 'node:fs';
import { tmpdir } from 'node:os';
import { join, resolve } from 'node:path';
import { fileURLToPath } from 'node:url';
import { verifyDevelopmentParity } from './development-parity.mjs';

const canonical = 'https://tangled.org/samclemente.me/the-social-wire';
const railwayEndpoint = 'https://backboard.railway.com/graphql/v2';
const shaPattern = /^[a-f0-9]{40}$/;
const uuidPattern = /^[a-f0-9]{8}-[a-f0-9]{4}-[a-f0-9]{4}-[a-f0-9]{4}-[a-f0-9]{12}$/i;
const credentials = ['ATCR_DEVICE_SECRET', 'RAILWAY_TOKEN', 'RAILWAY_API_TOKEN'];

export function cleanEnvironment(environment) {
  return Object.fromEntries(Object.entries(environment).filter(([key]) => !credentials.includes(key)));
}

function required(environment, key) {
  const value = environment[key];
  if (typeof value !== 'string' || !value.trim()) throw new Error(`Missing ${key}.`);
  return value;
}

export function releaseContext(environment, head) {
  if (environment.TANGLED_PIPELINE_KIND !== 'push') throw new Error('Only push pipelines can publish or deploy.');
  if (environment.TANGLED_PR_TARGET_BRANCH || environment.TANGLED_PR_SOURCE_SHA) throw new Error('Pull request runs cannot publish or deploy.');
  const branch = required(environment, 'TANGLED_REF_NAME');
  if (!['dev', 'main'].includes(branch) || environment.TANGLED_REF !== `refs/heads/${branch}`) throw new Error('Releases require the dev or main branch reference.');
  const sha = required(environment, 'TANGLED_SHA');
  if (!shaPattern.test(sha) || sha !== head) throw new Error('The checkout must equal the triggering full commit SHA.');
  const namespace = required(environment, 'ATCR_NAMESPACE');
  if (!/^[a-z0-9](?:[a-z0-9.-]*[a-z0-9])?$/.test(namespace) || !namespace.includes('.')) throw new Error('ATCR_NAMESPACE must be an ATProto handle.');
  const prefix = branch === 'dev' ? 'DEVELOPMENT' : 'PRODUCTION';
  const target = { environmentId: required(environment, `RAILWAY_${prefix}_ENVIRONMENT_ID`) };
  for (const kind of ['web', 'appview']) target[kind] = required(environment, `RAILWAY_${prefix}_${kind.toUpperCase()}_SERVICE_ID`);
  const projectId = required(environment, 'RAILWAY_PROJECT_ID');
  if (![projectId, ...Object.values(target)].every(value => uuidPattern.test(value))) throw new Error('Railway targets must be explicit UUIDs.');
  if (target.web === target.appview) throw new Error('Web and AppView must be different Railway services.');
  return { branch, sha, namespace, projectId, target, appEnvironment: branch === 'dev' ? 'dev' : 'prod' };
}

export function webBuildArguments(environment, appEnvironment) {
  const development = appEnvironment !== 'prod';
  const values = {
    NEXT_PUBLIC_APP_ENV: development ? 'dev' : 'prod',
    NEXT_PUBLIC_SITE_URL: development ? 'https://testing.thesocialwire.app' : 'https://thesocialwire.app',
    NEXT_PUBLIC_SOCIALWIRE_API_URL: development ? 'https://api.testing.thesocialwire.app' : 'https://api.thesocialwire.app',
    NEXT_PUBLIC_USE_THIN_APPVIEW: 'true',
  };
  if (environment.SPINDLE_WEB_BUILD_ARGS) {
    const extras = JSON.parse(environment.SPINDLE_WEB_BUILD_ARGS);
    if (!extras || Array.isArray(extras) || typeof extras !== 'object') throw new Error('SPINDLE_WEB_BUILD_ARGS must be a JSON object.');
    for (const [key, value] of Object.entries(extras)) {
      if (!/^NEXT_PUBLIC_[A-Z0-9_]+$/.test(key) || typeof value !== 'string' || /[\0\r\n]/.test(value)) throw new Error('Only public string build arguments are accepted.');
      if (key in values && values[key] !== value) throw new Error(`The release branch determines ${key}.`);
      values[key] = value;
    }
  }
  return Object.entries(values).flatMap(([key, value]) => ['--build-arg', `${key}=${value}`]);
}

export function imageReference(context, kind) {
  if (!['web', 'appview'].includes(kind)) throw new Error('Unknown image kind.');
  return `atcr.io/${context.namespace}/the-social-wire-${kind}:${context.appEnvironment}-${context.sha}`;
}

export function sourceRevision(reference, namespace, kind, appEnvironment) {
  const prefix = `atcr.io/${namespace}/the-social-wire-${kind}:${appEnvironment}-`;
  if (typeof reference !== 'string' || !reference.startsWith(prefix)) throw new Error(`Expected a pinned ${kind} ${appEnvironment} ATCR source.`);
  const suffix = reference.slice(prefix.length);
  if (!/^[a-f0-9]{40}@sha256:[a-f0-9]{64}$/.test(suffix)) throw new Error('Railway image sources must include a full commit tag and immutable digest.');
  return suffix.slice(0, 40);
}

export function run(command, args, { environment = process.env, capture = false, combineOutput = false, input, timeout = 60 * 60 * 1000 } = {}) {
  const result = spawnSync(command, args, { env: cleanEnvironment(environment), encoding: 'utf8', input,
    stdio: capture || input !== undefined ? ['pipe', 'pipe', 'pipe'] : 'inherit', timeout, maxBuffer: 8 * 1024 * 1024 });
  if (result.error || result.status !== 0) throw new Error(`${command} failed${result.status === null ? '' : ` (exit ${result.status})`}.`);
  return capture ? (combineOutput ? `${result.stdout}${result.stderr}` : result.stdout).trim() : '';
}

function git(args) { return execFileSync('git', args, { encoding: 'utf8', timeout: 60000, maxBuffer: 8 * 1024 * 1024 }).trim(); }

function currentHead(branch) {
  const line = git(['ls-remote', '--exit-code', canonical, `refs/heads/${branch}`]);
  const sha = line.split(/\s+/)[0];
  if (!shaPattern.test(sha)) throw new Error('Cannot resolve canonical branch head.');
  return sha;
}

function assertCurrent(context) {
  if (currentHead(context.branch) !== context.sha) throw new Error('A newer push superseded this release.');
}

export async function railway(query, variables, environment = process.env, request = fetch) {
  const accountToken = environment.RAILWAY_API_TOKEN;
  const projectToken = environment.RAILWAY_TOKEN;
  if (Boolean(accountToken) === Boolean(projectToken)) throw new Error('Configure exactly one Railway account or project token.');
  const headers = { 'Content-Type': 'application/json', ...(accountToken ? { Authorization: `Bearer ${accountToken}` } : { 'Project-Access-Token': projectToken }) };
  const response = await request(railwayEndpoint, { method: 'POST', headers, body: JSON.stringify({ query, variables }), signal: AbortSignal.timeout(30000) });
  if (!response.ok) throw new Error(`Railway API returned HTTP ${response.status}.`);
  const payload = await response.json();
  if (payload.errors?.length || !payload.data) throw new Error('Railway API rejected the operation.');
  return payload.data;
}

async function instance(environmentId, serviceId, projectId) {
  const data = await railway(`query Instance($environmentId: String!, $serviceId: String!) {
    serviceInstance(environmentId: $environmentId, serviceId: $serviceId) {
      source { image } latestDeployment { id status projectId }
    }
  }`, { environmentId, serviceId });
  const value = data.serviceInstance;
  if (!value || (value.latestDeployment && value.latestDeployment.projectId !== projectId)) throw new Error('Railway target does not match the configured project.');
  if (value.latestDeployment && value.latestDeployment.status !== 'SUCCESS') throw new Error('The target has an unfinished or unsuccessful deployment; resolve it before releasing.');
  return value;
}

async function productionParity(context, environment) {
  if (context.branch !== 'main') return;
  const developmentId = required(environment, 'RAILWAY_DEVELOPMENT_ENVIRONMENT_ID');
  if (!uuidPattern.test(developmentId)) throw new Error('Invalid Development environment UUID.');
  const developmentSHA = currentHead('dev');
  git(['fetch', '--no-tags', canonical, `refs/heads/dev:refs/remotes/tangled-ci/dev`]);
  for (const kind of ['web', 'appview']) {
    const serviceId = required(environment, `RAILWAY_DEVELOPMENT_${kind.toUpperCase()}_SERVICE_ID`);
    if (!uuidPattern.test(serviceId)) throw new Error('Invalid Development service UUID.');
    const development = await instance(developmentId, serviceId, context.projectId);
    if (!development.latestDeployment || sourceRevision(development.source?.image, context.namespace, kind, 'dev') !== developmentSHA) throw new Error('Both Development services must successfully deploy current dev before Production.');
    const production = await instance(context.target.environmentId, context.target[kind], context.projectId);
    const baseline = sourceRevision(production.source?.image, context.namespace, kind, 'prod');
    verifyDevelopmentParity({ base: baseline, head: context.sha, development: developmentSHA });
  }
  console.log(`Development parity verified against deployed ${developmentSHA}.`);
}

function build(context, kind, target) {
  const reference = imageReference(context, kind);
  const args = ['build', '--platform', 'linux/amd64', '--file', kind === 'appview' ? 'services/appview/Dockerfile.standalone' : 'apps/web/Dockerfile', '--tag', target ? `${reference}-verify` : reference,
    '--label', `org.opencontainers.image.revision=${context.sha}`, '--label', `org.opencontainers.image.source=${canonical}`];
  if (kind === 'web') args.push(...webBuildArguments(process.env, context.appEnvironment));
  if (target) args.push('--target', target);
  args.push('.');
  run('docker', args);
  return target ? `${reference}-verify` : reference;
}

/** Reject an ephemeral root-filesystem database before starting any service. */
export async function runtimeVolumeGuardSmoke(image, { execute = run } = {}) {
  if (typeof image !== 'string' || !/^[a-zA-Z0-9][a-zA-Z0-9._/:@-]*$/.test(image)) throw new Error('Invalid runtime smoke image.');
  const name = `tsw-spindle-volume-guard-${randomUUID()}`;
  const docker = (args, options = {}) => execute('docker', args, { capture: true, timeout: 30000, ...options });
  try {
    docker(['run', '--detach', '--name', name, '--network', 'none', image]);
    const status = docker(['wait', name]);
    if (!/^[1-9][0-9]*$/.test(status)) throw new Error('Standalone image must reject startup without a persistent /data mount.');
    const logs = docker(['logs', name], { combineOutput: true });
    if (!logs.includes('Standalone backend requires a persistent volume mounted at /data.')) throw new Error('Standalone startup did not report its required volume.');
    console.log('Packaged runtime rejected startup without a persistent /data mount.');
  } finally {
    try { docker(['rm', '--force', name]); } catch {}
  }
}

/** Exercise the final packaged processes, not just Swift/Bun build targets.
 * No host ports, network access, production credentials or existing volumes. */
export async function runtimeSmoke(image, {
  execute = run,
  pause = ms => new Promise(done => setTimeout(done, ms)),
  attempts = 90,
} = {}) {
  if (typeof image !== 'string' || !/^[a-zA-Z0-9][a-zA-Z0-9._/:@-]*$/.test(image)) throw new Error('Invalid runtime smoke image.');
  const name = `tsw-spindle-smoke-${randomUUID()}`;
  const volume = `${name}-data`;
  const fixtureKey = Buffer.alloc(32, 7).toString('base64');
  const environment = {
    APP_ENV: 'dev', PORT: '8081',
    PODCASTS_ENABLED: 'true', SOCIAL_DRAFTS_ENABLED: 'true', TOOLS_ENABLED: 'true',
    PODCAST_BRIDGE_ENABLED: 'false',
    PDS_ATTESTATION_RECEIPT_SECRET: 'smoke-fixture-attestation-secret-at-least-32-bytes',
    GATEWAY_APPVIEW_INTERNAL_SECRET: 'smoke-fixture-analytics-secret-at-least-32-bytes',
    SOCIAL_DRAFT_STORAGE_KEY: fixtureKey, PODCAST_PRIVATE_STORAGE_KEY: fixtureKey,
    PODCAST_MEDIA_INTERNAL_SECRET: 'smoke-fixture-media-secret-at-least-32-bytes',
    SOCIAL_DRAFT_S3_ENDPOINT: 'https://smoke.invalid', SOCIAL_DRAFT_S3_BUCKET: 'fixture',
    SOCIAL_DRAFT_S3_ACCESS_KEY_ID: 'fixture', SOCIAL_DRAFT_S3_SECRET_ACCESS_KEY: 'fixture', SOCIAL_DRAFT_S3_REGION: 'auto',
    PODCAST_S3_ENDPOINT: 'https://smoke.invalid', PODCAST_S3_BUCKET: 'fixture',
    PODCAST_S3_ACCESS_KEY_ID: 'fixture', PODCAST_S3_SECRET_ACCESS_KEY: 'fixture',
  };
  const docker = (args, options = {}) => execute('docker', args, { capture: true, ...options });
  const status = url => docker(['exec', name, 'curl', '--silent', '--show-error', '--connect-timeout', '1', '--max-time', '3', '--output', '/dev/null', '--write-out', '%{http_code}', url]);
  const expectStatus = (url, expected) => {
    const actual = status(url);
    if (actual !== expected) throw new Error(`Runtime smoke expected HTTP ${expected} at ${url}, received ${actual}.`);
  };
  const ready = async () => {
    for (let attempt = 0; attempt < attempts; attempt++) {
      try { if (status('http://127.0.0.1:8081/readyz') === '200') return; } catch {}
      if (docker(['inspect', '--format', '{{.State.Running}}', name]) !== 'true') throw new Error('Runtime smoke container exited during startup.');
      await pause(1000);
    }
    throw new Error('Runtime smoke startup did not become ready within the bounded deadline.');
  };
  let bunPID;
  try {
    docker(['volume', 'create', volume]);
    docker(['run', '--detach', '--name', name, '--network', 'none', '--volume', `${volume}:/data`,
      ...Object.entries(environment).flatMap(([key, value]) => ['--env', `${key}=${value}`]), image]);
    await ready();
    expectStatus('http://127.0.0.1:8081/livez', '200');
    expectStatus('http://127.0.0.1:8081/ios-client-metadata.json', '200');
    expectStatus('http://127.0.0.1:8081/xrpc/app.thesocialwire.publication.getSidebar', '401');
    expectStatus('http://127.0.0.1:9080/startupz', '200');
    expectStatus('http://127.0.0.1:9080/healthz', '200');
    bunPID = docker(['exec', name, 'sh', '-euc', 'for file in /proc/[0-9]*/comm; do if [ "$(cat "$file" 2>/dev/null)" = bun ]; then pid=${file%/comm}; printf "%s\\n" "${pid#/proc/}"; exit 0; fi; done; exit 1']);
    if (!/^[1-9][0-9]*$/.test(bunPID)) throw new Error('Runtime smoke could not identify the Bun processor.');
    docker(['exec', name, 'kill', '-STOP', bunPID]);
    expectStatus('http://127.0.0.1:8081/readyz', '503');
    expectStatus('http://127.0.0.1:8081/livez', '200');
    docker(['exec', name, 'kill', '-CONT', bunPID]);
    bunPID = undefined;
    await ready();
    console.log('Packaged runtime smoke passed: API, private media processor, and dependency-aware readiness.');
  } catch (error) {
    try { console.error(docker(['logs', '--tail', '80', name])); } catch {}
    throw error;
  } finally {
    if (bunPID) { try { docker(['exec', name, 'kill', '-CONT', bunPID]); } catch {} }
    try { docker(['rm', '--force', name]); } catch {}
    try { docker(['volume', 'rm', '--force', volume]); } catch {}
  }
}

/** Check the final standalone Next.js image, including copied runtime assets. */
export async function webRuntimeSmoke(image, {
  execute = run,
  pause = ms => new Promise(done => setTimeout(done, ms)),
  attempts = 60,
} = {}) {
  if (typeof image !== 'string' || !/^[a-zA-Z0-9][a-zA-Z0-9._/:@-]*$/.test(image)) throw new Error('Invalid runtime smoke image.');
  const name = `tsw-spindle-web-smoke-${randomUUID()}`;
  const docker = (args, options = {}) => execute('docker', args, { capture: true, timeout: 10000, ...options });
  try {
    docker(['run', '--detach', '--name', name, '--network', 'none', image]);
    for (let attempt = 0; attempt < attempts; attempt++) {
      let response;
      try {
        response = docker(['exec', name, 'node', '-e', "fetch('http://127.0.0.1:3000/', { signal: AbortSignal.timeout(3000) }).then(response => console.log(response.status)).catch(() => process.exit(1))"]);
      } catch {}
      if (response === '200') {
        console.log('Packaged Web runtime served the homepage successfully.');
        return;
      }
      if (docker(['inspect', '--format', '{{.State.Running}}', name]) !== 'true') throw new Error('Packaged Web runtime exited during startup.');
      await pause(1000);
    }
    throw new Error('Packaged Web runtime did not serve the homepage within the bounded deadline.');
  } catch (error) {
    try { console.error(docker(['logs', '--tail', '80', name])); } catch {}
    throw error;
  } finally {
    try { docker(['rm', '--force', name]); } catch {}
  }
}

export function webVerificationCommand() {
  // Bun's text coverage reporter cannot reliably write directly to Docker's log pipe.
  const coverage = '(cd apps/web; coverage_status=0; bun run test:coverage > /tmp/web-coverage.log 2>&1 || coverage_status=$?; report_status=0; python3 ../../scripts/ci/emit-log.py /tmp/web-coverage.log || report_status=$?; if [ "$coverage_status" -ne 0 ]; then exit "$coverage_status"; fi; exit "$report_status")';
  return `bunx turbo typecheck --filter=web && bunx turbo lint --filter=web && bun --cwd packages/read-state typecheck && bun --cwd packages/read-state test && ${coverage} && bun --cwd packages/lexicons test && (cd packages/spec && bun test --timeout 30000)`;
}

async function verify(context, selection = 'all') {
  if (!['all', 'web', 'appview'].includes(selection)) throw new Error('Verification target must be all, web or appview.');
  run('node', ['--test', 'scripts/ci/spindle.test.mjs', 'scripts/ci/development-parity.test.mjs']);
  for (const kind of ['appview', 'web']) {
    if (selection !== 'all' && selection !== kind) continue;
    if (kind === 'appview') {
      build(context, kind, 'backend-tests');
      build(context, kind, 'media-tests');
      const image = build(context, kind);
      await runtimeVolumeGuardSmoke(image);
      await runtimeSmoke(image);
      continue;
    }
    // Contract tests read pinned upstream source fixtures in clean checkouts.
    run('python3', ['scripts/go/check-out-swift-contracts.py']);
    const image = build(context, kind, 'builder');
    run('docker', ['run', '--rm', '--init', '--platform', 'linux/amd64', '--entrypoint', 'bash', '--env', 'NEXT_PUBLIC_APP_ENV=test', image, '-euc', webVerificationCommand()]);
    await webRuntimeSmoke(build(context, kind));
  }
}

function push(context, kind, environment) {
  const reference = build(context, kind);
  run('docker', ['push', reference], { environment });
  const digestReference = run('docker', ['image', 'inspect', reference, '--format', '{{index .RepoDigests 0}}'], { environment, capture: true });
  const repository = reference.slice(0, reference.lastIndexOf(':'));
  if (!digestReference.startsWith(`${repository}@sha256:`) || !/sha256:[a-f0-9]{64}$/.test(digestReference)) throw new Error('The registry did not return the pushed image digest.');
  return `${reference}@${digestReference.split('@')[1]}`;
}

export async function waitForDeployment(deploymentId, context, serviceId, { environment = process.env, request = fetch, pause = ms => new Promise(done => setTimeout(done, ms)), attempts = 120 } = {}) {
  for (let attempt = 0; attempt < attempts; attempt++) {
    const data = await railway('query Deployment($id: String!) { deployment(id: $id) { id status projectId environmentId serviceId } }', { id: deploymentId }, environment, request);
    const deployment = data.deployment;
    if (!deployment || deployment.id !== deploymentId || deployment.projectId !== context.projectId || deployment.environmentId !== context.target.environmentId || deployment.serviceId !== serviceId) throw new Error('Deployment identity mismatch.');
    if (deployment.status === 'SUCCESS') return deployment;
    if (!['QUEUED', 'INITIALIZING', 'WAITING', 'BUILDING', 'DEPLOYING'].includes(deployment.status)) throw new Error(`Deployment ${deploymentId} ended in ${deployment.status}.`);
    await pause(10000);
  }
  throw new Error(`Deployment ${deploymentId} is still pending; do not submit a duplicate deployment.`);
}

async function deploy(context, kind, image, expectedImage) {
  assertCurrent(context);
  const existing = await instance(context.target.environmentId, context.target[kind], context.projectId);
  if (existing.source?.image !== expectedImage) throw new Error('The service source changed during the release.');
  const variables = { environmentId: context.target.environmentId, serviceId: context.target[kind] };
  const update = await railway('mutation Source($environmentId: String!, $serviceId: String!, $input: ServiceInstanceUpdateInput!) { serviceInstanceUpdate(environmentId: $environmentId, serviceId: $serviceId, input: $input) }', { ...variables, input: { source: { image, repo: null } } });
  if (!update.serviceInstanceUpdate) throw new Error('Railway rejected the image source update.');
  const started = await railway('mutation Deploy($environmentId: String!, $serviceId: String!) { serviceInstanceDeployV2(environmentId: $environmentId, serviceId: $serviceId) }', variables);
  const id = started.serviceInstanceDeployV2;
  if (!uuidPattern.test(id ?? '')) throw new Error('Railway did not return a deployment ID.');
  console.log(`Waiting for ${kind} deployment ${id}.`);
  await waitForDeployment(id, context, context.target[kind]);
  const observed = await instance(context.target.environmentId, context.target[kind], context.projectId);
  if (observed.source?.image !== image || observed.latestDeployment?.id !== id) throw new Error('Deployment source changed before verification.');
  console.log(`${kind} deployed successfully: ${image}`);
}

export async function main(args, environment = process.env) {
  const action = args[0];
  const head = git(['rev-parse', 'HEAD']);
  if (action === 'verify') {
    const sha = environment.TANGLED_SHA ?? head;
    if (!shaPattern.test(sha) || sha !== head) throw new Error('Verification checkout does not match its triggering commit.');
    await verify({ sha, namespace: 'ci.invalid', appEnvironment: 'dev' }, args[1] ?? 'all');
    return;
  }
  if (action !== 'release' && action !== 'plan') throw new Error('Usage: spindle.mjs verify [all|web|appview] | plan | release');
  const context = releaseContext(environment, head);
  if (action === 'plan') {
    console.log(JSON.stringify({ ...context, images: ['web', 'appview'].map(kind => imageReference(context, kind)) }, null, 2));
    return;
  }
  if (context.branch === 'main' && !environment.RAILWAY_API_TOKEN) throw new Error('Production parity requires a Railway API token with access to both environments.');
  const secret = required(environment, 'ATCR_DEVICE_SECRET');
  if (!secret.startsWith('atcr_device_')) throw new Error('ATCR publishing requires a device secret from ATCR Settings → Devices.');
  const username = required(environment, 'ATCR_DEVICE_USERNAME');
  git(['fetch', '--no-tags', ...(git(['rev-parse', '--is-shallow-repository']) === 'true' ? ['--unshallow'] : []), canonical, 'refs/heads/main:refs/remotes/tangled-ci/main', 'refs/heads/dev:refs/remotes/tangled-ci/dev']);
  assertCurrent(context);
  await productionParity(context, environment);
  const previous = {};
  for (const kind of ['web', 'appview']) previous[kind] = (await instance(context.target.environmentId, context.target[kind], context.projectId)).source?.image;
  await verify(context);
  const directory = mkdtempSync(join(tmpdir(), 'social-wire-atcr-'));
  const dockerEnvironment = { ...environment, DOCKER_CONFIG: directory };
  const originalConfig = process.env.DOCKER_CONFIG;
  process.env.DOCKER_CONFIG = directory;
  try {
    run('docker', ['login', 'atcr.io', '--username', username, '--password-stdin'], { environment: dockerEnvironment, input: secret + '\n' });
    const images = {};
    for (const kind of ['appview', 'web']) images[kind] = push(context, kind, dockerEnvironment);
    assertCurrent(context);
    await productionParity(context, environment);
    for (const kind of ['appview', 'web']) await deploy(context, kind, images[kind], previous[kind]);
  } finally {
    if (originalConfig === undefined) delete process.env.DOCKER_CONFIG;
    else process.env.DOCKER_CONFIG = originalConfig;
    rmSync(directory, { recursive: true, force: true });
  }
}

if (process.argv[1] && resolve(process.argv[1]) === fileURLToPath(import.meta.url)) {
  main(process.argv.slice(2)).catch(error => { console.error(error.message); process.exitCode = 1; });
}
