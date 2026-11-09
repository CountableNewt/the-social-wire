import { test } from 'node:test';
import assert from 'node:assert/strict';
import { spawnSync } from 'node:child_process';
import { fileURLToPath } from 'node:url';
import { verifyRequiredChecks } from './required-checks.mjs';

const normal = {
  CHANGES_RESULT: 'success', WEB_FLAG: 'true', APPVIEW_FLAG: 'true',
  WEB_RESULT: 'success', APPVIEW_RESULT: 'success', REUSE_WEB: 'false', REUSE_APPVIEW: 'false',
  GITHUB_EVENT_NAME: 'pull_request', GITHUB_REF: 'refs/pull/123/merge',
};

for (const [web, appview, expected] of [
  ['true', 'true', { web: 'passed', appview: 'passed' }],
  ['true', 'false', { web: 'passed', appview: 'not-required' }],
  ['false', 'true', { web: 'not-required', appview: 'passed' }],
  ['false', 'false', { web: 'not-required', appview: 'not-required' }],
]) test(`accepts successful checks with path selection web=${web}, appview=${appview}`, () => {
  const environment = { ...normal, WEB_FLAG: web, APPVIEW_FLAG: appview, WEB_RESULT: web === 'true' ? 'success' : 'skipped', APPVIEW_RESULT: appview === 'true' ? 'success' : 'skipped' };
  const before = { ...environment };
  assert.deepEqual(verifyRequiredChecks(environment), expected);
  assert.deepEqual(environment, before);
});

test('permits checks to run and pass even when paths are unchanged', () => {
  assert.deepEqual(verifyRequiredChecks({ ...normal, WEB_FLAG: 'false', APPVIEW_FLAG: 'false' }), { web: 'passed', appview: 'passed' });
});

for (const branch of ['dev', 'main']) {
  for (const [web, appview, expected] of [
    ['true', 'true', { web: 'reused', appview: 'reused' }],
    ['true', 'false', { web: 'reused', appview: 'passed' }],
    ['false', 'true', { web: 'passed', appview: 'reused' }],
    ['false', 'false', { web: 'passed', appview: 'passed' }],
  ]) test(`accepts independently proven reuse on ${branch}: web=${web}, appview=${appview}`, () => {
    assert.deepEqual(verifyRequiredChecks({ ...normal, GITHUB_EVENT_NAME: 'push', GITHUB_REF: `refs/heads/${branch}`, REUSE_WEB: web, REUSE_APPVIEW: appview, WEB_RESULT: web === 'true' ? 'skipped' : 'success', APPVIEW_RESULT: appview === 'true' ? 'skipped' : 'success' }), expected);
  });
}

for (const service of ['WEB', 'APPVIEW']) {
  const name = service === 'WEB' ? /Web/ : /SQLite AppView/;
  test(`${service} cannot skip changed paths without proof`, () => {
    assert.throws(() => verifyRequiredChecks({ ...normal, [`${service}_RESULT`]: 'skipped' }), name);
  });
  test(`${service} rejects unsuccessful or missing outcomes even when unchanged`, () => {
    for (const flag of ['true', 'false']) for (const result of ['failure', 'cancelled', 'timed_out', 'neutral', 'pending', '', undefined]) {
      assert.throws(() => verifyRequiredChecks({ ...normal, [`${service}_FLAG`]: flag, [`${service}_RESULT`]: result }), name);
    }
  });
  test(`${service} cannot reuse checks on other events or refs`, () => {
    for (const event of ['pull_request', 'pull_request_target', 'merge_group', 'workflow_dispatch', 'workflow_run', '', undefined]) {
      assert.throws(() => verifyRequiredChecks({ ...normal, [`REUSE_${service}`]: 'true', [`${service}_RESULT`]: 'skipped', GITHUB_EVENT_NAME: event, GITHUB_REF: 'refs/heads/dev' }), name);
    }
    for (const ref of ['refs/heads/feature/dev', 'refs/heads/develop', 'refs/tags/dev', 'refs/pull/123/merge', 'dev', '', undefined]) {
      assert.throws(() => verifyRequiredChecks({ ...normal, [`REUSE_${service}`]: 'true', [`${service}_RESULT`]: 'skipped', GITHUB_EVENT_NAME: 'push', GITHUB_REF: ref }), name);
    }
  });
  test(`${service} reuse cannot hide a failed, canceled, missing, or unexpectedly run job`, () => {
    for (const result of ['failure', 'cancelled', 'success', '', undefined]) {
      assert.throws(() => verifyRequiredChecks({ ...normal, [`REUSE_${service}`]: 'true', [`${service}_RESULT`]: result, GITHUB_EVENT_NAME: 'push', GITHUB_REF: 'refs/heads/dev' }), name);
    }
    assert.throws(() => verifyRequiredChecks({ ...normal, [`${service}_FLAG`]: 'false', [`REUSE_${service}`]: 'true', [`${service}_RESULT`]: 'skipped', GITHUB_EVENT_NAME: 'push', GITHUB_REF: 'refs/heads/main' }), name);
  });
  test(`${service} requires explicit path and proof outputs, including for successful jobs`, () => {
    for (const key of [`${service}_FLAG`, `REUSE_${service}`]) for (const invalid of [undefined, '', 'TRUE', 'False', '1', true, false, null, 'true\n', ' true']) {
      assert.throws(() => verifyRequiredChecks({ ...normal, [key]: invalid }), new RegExp(key));
    }
  });
}

test('successful service results and claimed reuse cannot override failed path detection', () => {
  for (const result of ['failure', 'cancelled', 'skipped', '', undefined]) {
    assert.throws(() => verifyRequiredChecks({ ...normal, CHANGES_RESULT: result }), /Path detection failed/);
    assert.throws(() => verifyRequiredChecks({ ...normal, CHANGES_RESULT: result, REUSE_WEB: 'true', REUSE_APPVIEW: 'true', WEB_RESULT: 'skipped', APPVIEW_RESULT: 'skipped', GITHUB_EVENT_NAME: 'push', GITHUB_REF: 'refs/heads/dev' }), /Path detection failed/);
  }
});

test('CLI reports success or exits nonzero when proof outputs are missing', () => {
  const script = fileURLToPath(new URL('./required-checks.mjs', import.meta.url));
  const passed = spawnSync(process.execPath, [script], { env: normal, encoding: 'utf8' });
  assert.equal(passed.status, 0); assert.match(passed.stdout, /Required checks verified/);
  const rejected = spawnSync(process.execPath, [script], { env: { ...normal, REUSE_WEB: '' }, encoding: 'utf8' });
  assert.equal(rejected.status, 1); assert.match(rejected.stderr, /REUSE_WEB/);
});
