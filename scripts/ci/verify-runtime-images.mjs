import { execFileSync, spawnSync } from 'node:child_process';
import { randomUUID } from 'node:crypto';
import { existsSync, mkdirSync, renameSync, rmSync } from 'node:fs';
import { resolve } from 'node:path';
import { fileURLToPath } from 'node:url';

const canonical = 'https://github.com/Stygian-Tech/the-social-wire';
const shaPattern = /^[a-f0-9]{40}$/;
const credentials = ['RAILWAY_TOKEN', 'RAILWAY_API_TOKEN', 'GITHUB_TOKEN', 'GH_TOKEN'];

export function cleanEnvironment(environment) {
  return Object.fromEntries(Object.entries(environment).filter(([key]) => !credentials.includes(key)));
}

export function verificationContext(environment, head) {
  const sha = environment.GITHUB_SHA ?? head;
  if (!shaPattern.test(sha) || sha !== head) throw new Error('Verification checkout does not match its triggering commit.');
  return { sha, appEnvironment: 'dev' };
}

export function webBuildArguments(appEnvironment) {
  const development = appEnvironment !== 'prod';
  const values = {
    NEXT_PUBLIC_APP_ENV: development ? 'dev' : 'prod',
    NEXT_PUBLIC_SITE_URL: development ? 'https://testing.thesocialwire.app' : 'https://thesocialwire.app',
    NEXT_PUBLIC_SOCIALWIRE_API_URL: development ? 'https://api.testing.thesocialwire.app' : 'https://api.thesocialwire.app',
    NEXT_PUBLIC_USE_THIN_APPVIEW: 'true',
  };
  return Object.entries(values).flatMap(([key, value]) => ['--build-arg', `${key}=${value}`]);
}

export function imageReference(sha, kind) {
  if (!shaPattern.test(sha)) throw new Error('Local image tags require a full commit SHA.');
  if (!['web', 'appview'].includes(kind)) throw new Error('Unknown image kind.');
  return `social-wire-ci-${kind}:${sha}`;
}

export function run(command, args, { environment = process.env, capture = false, combineOutput = false, input, timeout = 60 * 60 * 1000 } = {}) {
  const result = spawnSync(command, args, { env: cleanEnvironment(environment), encoding: 'utf8', input,
    stdio: capture || input !== undefined ? ['pipe', 'pipe', 'pipe'] : 'inherit', timeout, maxBuffer: 8 * 1024 * 1024 });
  if (result.error || result.status !== 0) throw new Error(`${command} failed${result.status === null ? '' : ` (exit ${result.status})`}.`);
  return capture ? (combineOutput ? `${result.stdout}${result.stderr}` : result.stdout).trim() : '';
}

function git(args) { return execFileSync('git', args, { encoding: 'utf8', timeout: 60000, maxBuffer: 8 * 1024 * 1024 }).trim(); }

export function build(context, kind, target, { execute = run, environment = process.env } = {}) {
  const reference = imageReference(context.sha, kind);
  const cacheRoot = environment.CI_DOCKER_CACHE_DIR ? resolve(environment.CI_DOCKER_CACHE_DIR) : undefined;
  if (cacheRoot && /[,\r\n]/.test(cacheRoot)) throw new Error('Docker cache directory cannot contain commas or newlines.');
  const cache = cacheRoot && resolve(cacheRoot, kind);
  const nextCache = cacheRoot && resolve(cacheRoot, `${kind}-next`);
  // Export once per image kind, retaining intermediate compilation layers without
  // accumulating the unreferenced blobs in BuildKit's previous local exports.
  const exportCache = cacheRoot && ((kind === 'appview' && target === 'backend-tests') || (kind === 'web' && !target));
  const args = [...(cacheRoot ? ['buildx', 'build', '--load', '--progress', 'plain'] : ['build']), '--platform', 'linux/amd64', '--file', kind === 'appview' ? 'services/appview/Dockerfile.standalone' : 'apps/web/Dockerfile', '--tag', target ? `${reference}-verify` : reference,
    '--label', `org.opencontainers.image.revision=${context.sha}`, '--label', `org.opencontainers.image.source=${canonical}`];
  if (kind === 'web') args.push(...webBuildArguments(context.appEnvironment));
  if (target) args.push('--target', target);
  if (cacheRoot) {
    mkdirSync(cacheRoot, { recursive: true });
    if (existsSync(resolve(cache, 'index.json'))) args.push('--cache-from', `type=local,src=${cache}`);
    if (exportCache) {
      rmSync(nextCache, { recursive: true, force: true });
      args.push('--cache-to', `type=local,dest=${nextCache},mode=max`);
    }
  }
  args.push('.');
  try {
    execute('docker', args, { environment });
    if (exportCache) {
      if (!existsSync(resolve(nextCache, 'index.json'))) throw new Error('Docker build did not export its requested cache.');
      rmSync(cache, { recursive: true, force: true });
      renameSync(nextCache, cache);
    }
  } finally {
    if (exportCache) rmSync(nextCache, { recursive: true, force: true });
  }
  return target ? `${reference}-verify` : reference;
}

/** Reject an ephemeral root-filesystem database before starting any service. */
export async function runtimeVolumeGuardSmoke(image, { execute = run } = {}) {
  if (typeof image !== 'string' || !/^[a-zA-Z0-9][a-zA-Z0-9._/:@-]*$/.test(image)) throw new Error('Invalid runtime smoke image.');
  const name = `tsw-ci-volume-guard-${randomUUID()}`;
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
  const name = `tsw-ci-smoke-${randomUUID()}`;
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
  const name = `tsw-ci-web-smoke-${randomUUID()}`;
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

async function verify(context, selection = 'all', environment = process.env) {
  const buildImage = (kind, target) => build(context, kind, target, { environment });
  if (!['all', 'web', 'appview'].includes(selection)) throw new Error('Verification target must be all, web or appview.');
  run('node', ['--test', 'scripts/ci/verify-runtime-images.test.mjs', 'scripts/ci/development-parity.test.mjs']);
  for (const kind of ['appview', 'web']) {
    if (selection !== 'all' && selection !== kind) continue;
    if (kind === 'appview') {
      buildImage(kind, 'backend-tests');
      buildImage(kind, 'media-tests');
      const image = buildImage(kind);
      await runtimeVolumeGuardSmoke(image);
      await runtimeSmoke(image);
      continue;
    }
    // Contract tests read pinned upstream source fixtures in clean checkouts.
    run('python3', ['scripts/ci/check-out-swift-contracts.py']);
    const image = buildImage(kind, 'builder');
    run('docker', ['run', '--rm', '--init', '--platform', 'linux/amd64', '--entrypoint', 'bash', '--env', 'NEXT_PUBLIC_APP_ENV=test', image, '-euc', webVerificationCommand()]);
    await webRuntimeSmoke(buildImage(kind));
  }
}

export async function main(args, environment = process.env) {
  if (args.length > 1 || (args[0] && !['all', 'web', 'appview'].includes(args[0]))) throw new Error('Usage: verify-runtime-images.mjs [all|web|appview]');
  const context = verificationContext(environment, git(['rev-parse', 'HEAD']));
  await verify(context, args[0] ?? 'all', environment);
}

if (process.argv[1] && resolve(process.argv[1]) === fileURLToPath(import.meta.url)) {
  main(process.argv.slice(2)).catch(error => { console.error(error.message); process.exitCode = 1; });
}
