import { test } from 'node:test';
import assert from 'node:assert/strict';
import { existsSync, readFileSync } from 'node:fs';
import { reusePRVerification, reuseOutputs } from './reuse-pr-verification.mjs';
import { verifyRequiredChecks } from './required-checks.mjs';

const workflow = readFileSync(new URL('../../.github/workflows/ci.yml', import.meta.url), 'utf8');
// Read only the stable job/step boundaries needed for this wiring contract; no YAML dependency is installed in Detect Changed Paths.
function job(name) {
  const block = workflow.match(new RegExp(`^  ${name}:\\n([\\s\\S]*?)(?=^  [a-z][a-z_-]*:|$(?![\\s\\S]))`, 'm'))?.[1];
  assert.ok(block, `Missing ${name} job`);
  return block;
}
function step(block, name) {
  const found = block.match(new RegExp(`^      - name: ${name}\\n([\\s\\S]*?)(?=^      - |$(?![\\s\\S]))`, 'm'))?.[1];
  assert.ok(found, `Missing ${name} step`);
  return found;
}

test('changes exports path flags and only the proof runner can authorize target reuse', () => {
  const changes = job('changes');
  for (const target of ['web', 'appview']) {
    assert.ok(changes.includes(`${target}: \${{ steps.filter.outputs.${target} }}`));
    assert.ok(changes.includes(`reuse_${target}: \${{ steps.reuse.outputs.reuse_${target} }}`));
    const runtime = job(target);
    assert.match(runtime, /^    needs: changes$/m);
    const expression = runtime.match(/^    if: (.+)$/m)?.[1];
    assert.equal(expression, `needs.changes.result == 'success' && needs.changes.outputs.${target} == 'true' && needs.changes.outputs.reuse_${target} != 'true'`);
  }
  const runner = step(changes, 'Reuse exact successful PR verification');
  assert.match(runner, /^        id: reuse$/m);
  assert.match(runner, /^        run: node scripts\/ci\/reuse-pr-verification.mjs$/m);
  assert.doesNotMatch(runner, /^        (?:if|continue-on-error):/m);
  assert.ok(runner.includes('GITHUB_TOKEN: ${{ github.token }}'));
  assert.ok(runner.includes('INPUT_WEB: ${{ steps.filter.outputs.web }}'));
  assert.ok(runner.includes('INPUT_APPVIEW: ${{ steps.filter.outputs.appview }}'));
  assert.ok(changes.indexOf('id: filter') < changes.indexOf('id: reuse'));
});

test('Required always runs and checks every actual target, path and proof result', () => {
  const required = job('required');
  assert.match(required, /^    name: CI — Required$/m);
  assert.match(required, /^    needs: \[changes, web, appview\]$/m);
  assert.match(required, /^    if: always\(\)$/m);
  const gate = step(required, 'Verify path-filtered checks');
  for (const [name, expression] of Object.entries({
    CHANGES_RESULT: 'needs.changes.result', WEB_FLAG: 'needs.changes.outputs.web', APPVIEW_FLAG: 'needs.changes.outputs.appview',
    WEB_RESULT: 'needs.web.result', APPVIEW_RESULT: 'needs.appview.result',
    REUSE_WEB: 'needs.changes.outputs.reuse_web', REUSE_APPVIEW: 'needs.changes.outputs.reuse_appview',
  })) assert.ok(gate.includes(`${name}: \${{ ${expression} }}`), `${name} must use its real job output`);
  assert.match(gate, /^        run: node scripts\/ci\/required-checks.mjs$/m);
  assert.doesNotMatch(gate, /^        (?:if|continue-on-error):/m);
});

test('current Production parity guard executes before reuse and uses trusted Development source', () => {
  const changes = job('changes');
  const parity = step(changes, 'Require Production changes in Development first');
  assert.ok(parity.includes("github.event_name == 'pull_request' && github.event.pull_request.base.ref == 'main'"));
  assert.ok(parity.includes("github.event_name == 'merge_group' && github.event.merge_group.base_ref == 'refs/heads/main'"));
  assert.ok(parity.includes("github.event_name == 'push' && github.ref == 'refs/heads/main'"));
  assert.doesNotMatch(parity, /reuse_|continue-on-error/);
  assert.ok(parity.includes('git fetch --no-tags origin dev'));
  assert.ok(parity.includes('git show refs/remotes/origin/dev:scripts/ci/development-parity.mjs'));
  assert.ok(parity.includes('--base "$PRODUCTION_BASE_SHA" --head "$PRODUCTION_HEAD_SHA" --development refs/remotes/origin/dev'));
  assert.ok(changes.indexOf('Require Production changes in Development first') < changes.indexOf('id: reuse'));
  assert.ok(changes.indexOf('Record immutable PR verification context') < changes.indexOf('id: reuse'));
});

test('proof access remains read-only and a second post-merge workflow cannot duplicate work', () => {
  assert.match(workflow, /^permissions:\n  contents: read$/m);
  assert.match(job('changes'), /^    permissions:\n      contents: read\n      pull-requests: read\n      checks: read\n      actions: read$/m);
  assert.doesNotMatch(workflow, /:\s*write\s*$/m);
  assert.equal(existsSync(new URL('../../.github/workflows/validated-merge.yml', import.meta.url)), false);
});

function gateEnvironment(result, context) {
  const outputs = Object.fromEntries(reuseOutputs(result).trim().split('\n').map(line => line.split('=')));
  return { ...context, CHANGES_RESULT: 'success', WEB_FLAG: 'true', APPVIEW_FLAG: 'true', WEB_RESULT: 'success', APPVIEW_RESULT: 'success', REUSE_WEB: outputs.reuse_web, REUSE_APPVIEW: outputs.reuse_appview };
}

for (const eventName of ['pull_request', 'merge_group', 'workflow_dispatch']) test(`${eventName} never inherits a skip even with claimed successful evidence`, async () => {
  const context = { GITHUB_EVENT_NAME: eventName, GITHUB_REF: 'refs/heads/dev' };
  const result = await reusePRVerification({}, context, { verify: () => assert.fail('PR, merge-group, and manual events must not consult reuse proof') });
  assert.equal(result.reuseWeb, false); assert.equal(result.reuseAppview, false);
  const environment = gateEnvironment(result, context);
  assert.deepEqual(verifyRequiredChecks(environment), { web: 'passed', appview: 'passed' });
  assert.throws(() => verifyRequiredChecks({ ...environment, WEB_RESULT: 'skipped' }));
  assert.throws(() => verifyRequiredChecks({ ...environment, APPVIEW_RESULT: 'skipped' }));
});

test('failed push proof emits false outputs so normal builds must run', async () => {
  const sha = 'a'.repeat(40);
  const context = { GITHUB_EVENT_NAME: 'push', GITHUB_REF: 'refs/heads/dev', GITHUB_SHA: sha, GITHUB_REPOSITORY: 'example/repository', GITHUB_TOKEN: 'test-token', INPUT_WEB: 'true', INPUT_APPVIEW: 'true' };
  const result = await reusePRVerification({ ref: context.GITHUB_REF, after: sha, repository: { full_name: context.GITHUB_REPOSITORY } }, context, { createAPI: () => ({}), verify: async () => { throw new Error('API evidence unavailable'); } });
  const environment = gateEnvironment(result, context);
  assert.equal(environment.REUSE_WEB, 'false'); assert.equal(environment.REUSE_APPVIEW, 'false');
  assert.deepEqual(verifyRequiredChecks(environment), { web: 'passed', appview: 'passed' });
  for (const service of ['WEB', 'APPVIEW']) assert.throws(() => verifyRequiredChecks({ ...environment, [`${service}_RESULT`]: 'skipped' }));
});

for (const proven of ['web', 'appview']) test(`partial ${proven} proof can skip only its matching target`, async () => {
  const sha = 'a'.repeat(40), head = 'b'.repeat(40), tree = 'c'.repeat(40);
  const context = { GITHUB_EVENT_NAME: 'push', GITHUB_REF: 'refs/heads/main', GITHUB_SHA: sha, GITHUB_REPOSITORY: 'example/repository', GITHUB_TOKEN: 'test-token', INPUT_WEB: 'true', INPUT_APPVIEW: 'true' };
  const result = await reusePRVerification({ ref: context.GITHUB_REF, after: sha, repository: { full_name: context.GITHUB_REPOSITORY } }, context, { createAPI: () => ({}), verify: async () => ({ baseRef: 'main', mergeSHA: sha, headSHA: head, treeSHA: tree, prNumber: 5, runID: 123, targets: { web: proven === 'web', appview: proven === 'appview' } }) });
  const environment = { ...gateEnvironment(result, context), [`${proven.toUpperCase()}_RESULT`]: 'skipped' };
  assert.deepEqual(verifyRequiredChecks(environment), proven === 'web' ? { web: 'reused', appview: 'passed' } : { web: 'passed', appview: 'reused' });
  const other = proven === 'web' ? 'APPVIEW' : 'WEB';
  assert.throws(() => verifyRequiredChecks({ ...environment, [`${other}_RESULT`]: 'skipped' }));
});
