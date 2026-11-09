import assert from 'node:assert/strict';
import test from 'node:test';
import { mkdtempSync, mkdirSync, writeFileSync, rmSync, copyFileSync } from 'node:fs';
import { tmpdir } from 'node:os';
import { join } from 'node:path';
import { spawnSync } from 'node:child_process';
import { cleanEnvironment, verificationContext, webBuildArguments, imageReference, runtimeSmoke, runtimeVolumeGuardSmoke, webVerificationCommand, webRuntimeSmoke, run, main } from './verify-runtime-images.mjs';

const sha = 'a'.repeat(40);

test('verification requires the exact triggering GitHub checkout and accepts local HEAD', () => {
  assert.deepEqual(verificationContext({}, sha), { sha, appEnvironment: 'dev' });
  assert.deepEqual(verificationContext({ GITHUB_SHA: sha }, sha), { sha, appEnvironment: 'dev' });
  for (const candidate of ['a'.repeat(7), 'b'.repeat(40), 'not-a-sha']) {
    assert.throws(() => verificationContext({ GITHUB_SHA: candidate }, sha), /checkout/);
  }
});

test('verification rejects unsupported commands before invoking external tools', async () => {
  for (const args of [['release'], ['plan'], ['web', 'appview']]) {
    await assert.rejects(() => main(args, {}), /Usage: verify-runtime-images/);
  }
});

test('verification uses only local commit-tagged image names', () => {
  assert.equal(imageReference(sha, 'web'), `social-wire-ci-web:${sha}`);
  assert.equal(imageReference(sha, 'appview'), `social-wire-ci-appview:${sha}`);
  assert.throws(() => imageReference(sha, 'other'));
  assert.throws(() => imageReference('short', 'web'));
});

test('container subprocess environments omit deployment and GitHub credentials', () => {
  assert.deepEqual(cleanEnvironment({ PATH: '/bin', GITHUB_TOKEN: 'github-value', GH_TOKEN: 'gh-value', RAILWAY_TOKEN: 'project-token', RAILWAY_API_TOKEN: 'account-token', DOCKER_CONFIG: '/tmp/login' }), { PATH: '/bin', DOCKER_CONFIG: '/tmp/login' });
});

test('Web verification build arguments contain only the fixed public environment contract', () => {
  const args = webBuildArguments('dev');
  assert.ok(args.includes('NEXT_PUBLIC_SITE_URL=https://testing.thesocialwire.app'));
  assert.ok(args.includes('NEXT_PUBLIC_SOCIALWIRE_API_URL=https://api.testing.thesocialwire.app'));
  assert.ok(webBuildArguments('prod').includes('NEXT_PUBLIC_SITE_URL=https://thesocialwire.app'));
  assert.ok(webBuildArguments('prod').includes('NEXT_PUBLIC_SOCIALWIRE_API_URL=https://api.thesocialwire.app'));
  assert.equal(args.length, 8);
  for (let index = 0; index < args.length; index += 2) {
    assert.equal(args[index], '--build-arg');
    assert.match(args[index + 1], /^NEXT_PUBLIC_[A-Z0-9_]+=/);
  }
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
  assert.ok(!launch.some(value => /^(GITHUB_TOKEN|GH_TOKEN|RAILWAY_TOKEN|RAILWAY_API_TOKEN)=/.test(value)));
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

function fakeVolumeGuard(exit = '1', logs = 'Standalone backend requires a persistent volume mounted at /data.') {
  const calls = [];
  return { calls, execute(command, args, options) {
    assert.equal(command, 'docker');
    assert.equal(options.timeout, 30000);
    calls.push(args);
    if (args[0] === 'wait') return exit;
    if (args[0] === 'logs') {
      assert.equal(options.combineOutput, true);
      return logs;
    }
    assert.equal(options.combineOutput, undefined);
    return '';
  } };
}

test('packaged standalone image rejects missing persistent volume within a bounded deadline', async () => {
  const fake = fakeVolumeGuard();
  await runtimeVolumeGuardSmoke('fixture-runtime:tag', { execute: fake.execute });
  const start = fake.calls.find(args => args[0] === 'run');
  assert.ok(start.includes('none'));
  assert.ok(!start.includes('--volume'));
  assert.ok(fake.calls.some(args => args[0] === 'rm' && args[1] === '--force'));
});

test('volume guard smoke rejects successful startup or an unrelated failure and always cleans up', async () => {
  for (const fake of [fakeVolumeGuard('0'), fakeVolumeGuard('1', 'Unrelated startup error')]) {
    await assert.rejects(() => runtimeVolumeGuardSmoke('fixture-runtime:tag', { execute: fake.execute }));
    assert.ok(fake.calls.some(args => args[0] === 'rm'));
  }
});

test('volume guard smoke cleans up when the container fails to exit before the deadline', async () => {
  const fake = fakeVolumeGuard();
  await assert.rejects(() => runtimeVolumeGuardSmoke('fixture-runtime:tag', { execute(command, args, options) {
    fake.execute(command, args, options);
    if (args[0] === 'wait') throw new Error('deadline exceeded');
    return '';
  } }), /deadline exceeded/);
  assert.ok(fake.calls.some(args => args[0] === 'rm'));
});

test('the actual Web image command preserves failing coverage and report transport statuses', () => {
  const root = mkdtempSync(join(tmpdir(), 'tsw-web-coverage-command-'));
  const python = spawnSync('which', ['python3'], { encoding: 'utf8' }).stdout.trim();
  try {
    for (const path of ['bin', 'apps/web', 'packages/spec', 'scripts/ci']) mkdirSync(join(root, path), { recursive: true });
    copyFileSync(new URL('./emit-log.py', import.meta.url), join(root, 'scripts/ci/emit-log.py'));
    writeFileSync(join(root, 'bin/bunx'), '#!/bin/sh\nexit 0\n', { mode: 0o700 });
    writeFileSync(join(root, 'bin/bun'), '#!/bin/sh\nif [ "$*" = "run test:coverage" ]; then echo coverage-fixture-output; exit "$FIXTURE_COVERAGE_STATUS"; fi\nexit 0\n', { mode: 0o700 });
    writeFileSync(join(root, 'bin/python3'), '#!/bin/sh\nif [ "$FIXTURE_REPORT_STATUS" -ne 0 ]; then exit "$FIXTURE_REPORT_STATUS"; fi\nexec "$FIXTURE_PYTHON" "$@"\n', { mode: 0o700 });
    for (const [coverage, report, expected] of [[0, 0, 0], [23, 0, 23], [0, 47, 47], [23, 47, 23]]) {
      const result = spawnSync('bash', ['-euc', webVerificationCommand()], {
        cwd: root, encoding: 'utf8', env: { ...process.env, PATH: `${join(root, 'bin')}:${process.env.PATH}`, FIXTURE_PYTHON: python, FIXTURE_COVERAGE_STATUS: String(coverage), FIXTURE_REPORT_STATUS: String(report) },
      });
      assert.equal(result.status, expected, result.stderr);
      if (report === 0) assert.ok(result.stdout.includes('coverage-fixture-output'));
    }
  } finally { rmSync(root, { recursive: true, force: true }); }
});

test('report emitter transfers the complete report through a pressured nonblocking pipe', () => {
  const root = mkdtempSync(join(tmpdir(), 'tsw-report-backpressure-'));
  try {
    const report = join(root, 'report.log');
    writeFileSync(report, Buffer.alloc(2 * 1024 * 1024, 65));
    const check = `import os, subprocess, sys, time
from pathlib import Path
reader, writer = os.pipe()
os.set_blocking(writer, False)
child = subprocess.Popen([sys.executable, sys.argv[1], sys.argv[2]], stdout=writer, stderr=subprocess.PIPE)
os.close(writer)
time.sleep(0.05)
output = bytearray()
while chunk := os.read(reader, 65536):
    output.extend(chunk)
os.close(reader)
errors = child.communicate(timeout=10)[1]
assert child.returncode == 0, errors
assert bytes(output) == Path(sys.argv[2]).read_bytes(), 'report was truncated'
`;
    const result = spawnSync('python3', ['-c', check, new URL('./emit-log.py', import.meta.url).pathname, report], { encoding: 'utf8', timeout: 15000 });
    assert.equal(result.status, 0, result.stderr);
  } finally { rmSync(root, { recursive: true, force: true }); }
});

test('report emitter fails when its downstream consumer closes the pipe', () => {
  const root = mkdtempSync(join(tmpdir(), 'tsw-report-broken-pipe-'));
  try {
    const report = join(root, 'report.log');
    writeFileSync(report, Buffer.alloc(256 * 1024, 65));
    const check = `import os, subprocess, sys
reader, writer = os.pipe()
os.close(reader)
child = subprocess.run([sys.executable, sys.argv[1], sys.argv[2]], stdout=writer, stderr=subprocess.PIPE, timeout=10)
os.close(writer)
assert child.returncode != 0, 'broken report transport succeeded'
`;
    const result = spawnSync('python3', ['-c', check, new URL('./emit-log.py', import.meta.url).pathname, report], { encoding: 'utf8', timeout: 15000 });
    assert.equal(result.status, 0, result.stderr);
  } finally { rmSync(root, { recursive: true, force: true }); }
});

function fakeWebRuntime({ statuses = ['503', '200'], running = true } = {}) {
  const calls = [];
  return { calls, execute(command, args, options) {
    assert.equal(command, 'docker');
    assert.equal(options.timeout, 10000);
    calls.push(args);
    if (args[0] === 'exec') return statuses.shift() ?? '503';
    if (args[0] === 'inspect') return String(running);
    return '';
  } };
}

test('Web smoke exercises final image defaults in a network-isolated container', async () => {
  const fake = fakeWebRuntime();
  let pauses = 0;
  await webRuntimeSmoke('web-runtime:tag', { execute: fake.execute, pause: async () => { pauses++; } });
  const launch = fake.calls.find(args => args[0] === 'run');
  assert.ok(launch.includes('none'));
  assert.ok(!launch.includes('--env') && !launch.includes('--publish'));
  assert.ok(fake.calls.some(args => args[0] === 'exec' && args.includes('node') && args.at(-1).includes('http://127.0.0.1:3000/')));
  assert.equal(pauses, 1);
  assert.ok(fake.calls.some(args => args[0] === 'rm'));
});

test('Web smoke bounds unhealthy startup and rejects exited processes with cleanup', async () => {
  for (const fake of [fakeWebRuntime({ statuses: [] }), fakeWebRuntime({ statuses: ['503'], running: false })]) {
    await assert.rejects(() => webRuntimeSmoke('web-runtime:tag', { execute: fake.execute, pause: async () => {}, attempts: 2 }));
    assert.ok(fake.calls.some(args => args[0] === 'logs'));
    assert.ok(fake.calls.some(args => args[0] === 'rm'));
  }
});

test('process captures keep stdout unchanged unless combined stderr is explicitly requested', () => {
  const args = ['-e', "process.stdout.write('standard-output\\n'); process.stderr.write('diagnostic-output\\n');"];
  assert.equal(run(process.execPath, args, { capture: true }), 'standard-output');
  assert.equal(run(process.execPath, args, { capture: true, combineOutput: true }), 'standard-output\ndiagnostic-output');
});
