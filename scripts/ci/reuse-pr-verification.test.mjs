import test from 'node:test';
import assert from 'node:assert/strict';
import { main, reuseOutputs, reusePRVerification } from './reuse-pr-verification.mjs';

const merge = 'a'.repeat(40), head = 'b'.repeat(40), tree = 'c'.repeat(40);
function fixture(branch = 'dev') {
  const environment = { GITHUB_EVENT_NAME: 'push', GITHUB_REF: `refs/heads/${branch}`, GITHUB_SHA: merge, GITHUB_REPOSITORY: 'owner/repo', GITHUB_TOKEN: 'private-test-token', INPUT_WEB: 'true', INPUT_APPVIEW: 'true', GITHUB_OUTPUT: '/output', GITHUB_EVENT_PATH: '/event' };
  const event = { ref: environment.GITHUB_REF, after: merge, deleted: false, repository: { full_name: 'owner/repo' } };
  const evidence = { baseRef: branch, mergeSHA: merge, headSHA: head, treeSHA: tree, prNumber: 7, runID: 31, targets: { web: true, appview: true } };
  const calls = [];
  const dependencies = { createAPI: token => { calls.push({ token }); return 'api'; }, verify: async (...args) => { calls.push({ args }); return evidence; } };
  return { environment, event, evidence, calls, dependencies };
}

for (const branch of ['dev', 'main']) test(`reuses exact tested-tree PR target proof on a ${branch} push`, async () => {
  const f = fixture(branch);
  const result = await reusePRVerification(f.event, f.environment, f.dependencies);
  assert.deepEqual(result, { reuseWeb: true, reuseAppview: true, cacheRunID: 31, reason: `Verified PR #7, CI run 31, source tree ${tree}.` });
  assert.deepEqual(f.calls, [{ token: 'private-test-token' }, { args: ['api', 'owner/repo', merge, { baseRef: branch, requireTestedTree: true }] }]);
});

for (const [name, change] of [
  ['pull request', f => f.environment.GITHUB_EVENT_NAME = 'pull_request'],
  ['merge queue', f => f.environment.GITHUB_EVENT_NAME = 'merge_group'],
  ['manual dispatch', f => f.environment.GITHUB_EVENT_NAME = 'workflow_dispatch'],
  ['other branch', f => f.environment.GITHUB_REF = f.event.ref = 'refs/heads/feature/example'],
  ['tag', f => f.environment.GITHUB_REF = f.event.ref = 'refs/tags/dev'],
  ['mismatched ref', f => f.event.ref = 'refs/heads/main'],
  ['missing after', f => delete f.event.after],
  ['different pushed commit', f => f.event.after = head],
  ['invalid SHA', f => f.environment.GITHUB_SHA = f.event.after = 'bad'],
  ['deleted ref', f => f.event.deleted = true],
  ['empty SHA', f => f.environment.GITHUB_SHA = f.event.after = '0'.repeat(40)],
  ['other repository', f => f.event.repository.full_name = 'other/repo'],
  ['malformed repository', f => f.event.repository.full_name = 7],
  ['invalid repository', f => f.environment.GITHUB_REPOSITORY = f.event.repository.full_name = 'owner/repo/extra'],
  ['invalid web flag', f => f.environment.INPUT_WEB = 'TRUE'],
  ['missing backend flag', f => delete f.environment.INPUT_APPVIEW],
  ['missing token', f => delete f.environment.GITHUB_TOKEN],
]) test(`${name} falls back before querying PR evidence`, async () => {
  const f = fixture(); change(f);
  const result = await reusePRVerification(f.event, f.environment, f.dependencies);
  assert.equal(result.reuseWeb, false); assert.equal(result.reuseAppview, false);
  assert.deepEqual(f.calls, []);
});

test('unchanged targets never query evidence or request skips', async () => {
  const f = fixture(); f.environment.INPUT_WEB = f.environment.INPUT_APPVIEW = 'false';
  const result = await reusePRVerification(f.event, f.environment, f.dependencies);
  assert.equal(result.reuseWeb, false); assert.equal(result.reuseAppview, false); assert.deepEqual(f.calls, []);
});

for (const [web, appview] of [[true, true], [true, false], [false, true], [false, false]]) {
  test(`reuses only proven target jobs web=${web} appview=${appview}`, async () => {
    const f = fixture(); f.evidence.targets = { web, appview };
    const result = await reusePRVerification(f.event, f.environment, f.dependencies);
    assert.equal(result.reuseWeb, web); assert.equal(result.reuseAppview, appview);
  });
}
test('a proven target is not marked reusable when the current push does not need it', async () => {
  const f = fixture(); f.environment.INPUT_WEB = 'false';
  const result = await reusePRVerification(f.event, f.environment, f.dependencies);
  assert.equal(result.reuseWeb, false); assert.equal(result.reuseAppview, true);
});

for (const [name, change] of [
  ['wrong merge commit', f => f.evidence.mergeSHA = head],
  ['wrong target branch', f => f.evidence.baseRef = 'main'],
  ['absent head', f => delete f.evidence.headSHA],
  ['invalid tested tree', f => f.evidence.treeSHA = 'invalid'],
  ['invalid PR number', f => f.evidence.prNumber = -1],
  ['invalid run', f => f.evidence.runID = '31\nreuse_web=true'],
  ['old receipt without target proof', f => delete f.evidence.targets],
  ['incomplete target proof', f => delete f.evidence.targets.appview],
  ['string target proof', f => f.evidence.targets.web = 'true'],
]) test(`rejects ${name} and runs ordinary verification`, async () => {
  const f = fixture(); change(f);
  const result = await reusePRVerification(f.event, f.environment, f.dependencies);
  assert.equal(result.reuseWeb, false); assert.equal(result.reuseAppview, false);
});

test('unavailable, failed or stale PR evidence cannot skip a build or disclose error contents', async () => {
  const f = fixture();
  for (const error of [new Error('private-test-token signedURL=secret'), undefined, 'private-test-token']) {
    const result = await reusePRVerification(f.event, f.environment, { ...f.dependencies, verify: async () => { throw error; } });
    assert.deepEqual(result, { reuseWeb: false, reuseAppview: false, reason: 'PR evidence could not be reused; ordinary verification will run.' });
    assert.doesNotMatch(result.reason, /private-test-token|secret/);
  }
});

test('strict output serialization permits only exact boolean values', () => {
  assert.equal(reuseOutputs({ reuseWeb: true, reuseAppview: false }), 'reuse_web=true\nreuse_appview=false\ncache_run_id=\n');
  for (const result of [null, {}, { reuseWeb: 'true', reuseAppview: false }, { reuseWeb: true, reuseAppview: 'false\nreuse_web=true' }]) assert.throws(() => reuseOutputs(result), /Invalid verification reuse outputs/);
});

test('runner appends outputs and bounded safe provenance without logging credentials', async () => {
  const f = fixture(); const writes = [], logs = [];
  const result = await main(f.environment, { read: () => JSON.stringify(f.event), append: (...args) => writes.push(args), log: value => logs.push(value), decide: (event, environment) => reusePRVerification(event, environment, f.dependencies) });
  assert.equal(result.reuseWeb, true);
  assert.deepEqual(writes, [['/output', 'reuse_web=true\nreuse_appview=true\ncache_run_id=31\n']]);
  assert.deepEqual(logs, [`Verified PR #7, CI run 31, source tree ${tree}.`]);
  assert.doesNotMatch(logs.join('\n'), /private-test-token/);
});

test('unreadable or malformed event payloads append false flags for full build fallback', async () => {
  const f = fixture();
  for (const read of [() => '{bad JSON', () => { throw new Error('private-test-token'); }]) {
    const writes = [], logs = [];
    await main(f.environment, { read, append: (...args) => writes.push(args), log: value => logs.push(value) });
    assert.deepEqual(writes, [['/output', 'reuse_web=false\nreuse_appview=false\ncache_run_id=\n']]);
    assert.doesNotMatch(logs.join('\n'), /private-test-token/);
  }
});
test('decision exceptions fall back, but invalid or unwriteable outputs fail the step', async () => {
  const f = fixture(); const writes = [];
  await main(f.environment, { read: () => '{}', decide: async () => { throw new Error('private-test-token'); }, append: (...args) => writes.push(args), log: () => {} });
  assert.equal(writes[0][1], 'reuse_web=false\nreuse_appview=false\ncache_run_id=\n');
  await assert.rejects(main(f.environment, { read: () => '{}', decide: async () => ({ reuseWeb: 'true', reuseAppview: false }), append: () => { throw new Error('Should not append'); } }), /Invalid verification reuse outputs/);
  await assert.rejects(main({ ...f.environment, GITHUB_OUTPUT: '' }), /Missing GitHub output file/);
  await assert.rejects(main(f.environment, { read: () => '{}', decide: async () => ({ reuseWeb: false, reuseAppview: false }), append: () => { throw new Error('Write failed'); } }), /Write failed/);
});

test('cache provenance is emitted only for a proven reused target', async () => {
  for (const [web, appview] of [[true, true], [true, false], [false, true], [false, false]]) {
    const f = fixture(); f.evidence.targets = { web, appview };
    const outputs = reuseOutputs(await reusePRVerification(f.event, f.environment, f.dependencies));
    assert.match(outputs, new RegExp(`cache_run_id=${web || appview ? '31' : ''}\\n$`));
  }
  const f = fixture(); f.evidence.mergeSHA = head;
  assert.match(reuseOutputs(await reusePRVerification(f.event, f.environment, f.dependencies)), /cache_run_id=\n$/);
  for (const cacheRunID of ['31', '31\nreuse_web=true', 0, -1, NaN, Infinity, Number.MAX_SAFE_INTEGER + 1]) {
    assert.throws(() => reuseOutputs({ reuseWeb: true, reuseAppview: false, cacheRunID }), /Invalid cache provenance output/);
  }
});
