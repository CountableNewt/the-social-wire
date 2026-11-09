import { test } from 'node:test';
import assert from 'node:assert/strict';
import { execFileSync } from 'node:child_process';
import { mkdirSync, mkdtempSync, writeFileSync, unlinkSync, rmSync } from 'node:fs';
import { dirname, join } from 'node:path';
import { tmpdir } from 'node:os';
import { parseArguments, verifyDevelopmentParity } from './development-parity.mjs';

function repository(t) {
  const cwd = mkdtempSync(join(tmpdir(), 'development-parity-'));
  t.after(() => rmSync(cwd, { recursive: true, force: true }));
  const git = (...args) => execFileSync('git', args, { cwd, encoding: 'utf8', stdio: ['ignore', 'pipe', 'pipe'] }).trim();
  git('init', '-q'); git('config', 'user.name', 'Parity Test'); git('config', 'user.email', 'test@example.invalid'); git('config', 'commit.gpgsign', 'false');
  const write = (name, contents) => { mkdirSync(dirname(join(cwd, name)), { recursive: true }); writeFileSync(join(cwd, name), contents); };
  const commit = () => { git('add', '-A'); git('commit', '-qm', 'Test change'); return git('rev-parse', 'HEAD'); };
  write('feed.txt', 'first\nsecond\nthird\n'); write('delete.txt', 'old'); write('binary.bin', Buffer.from([0, 1, 2]));
  const base = commit(); git('branch', 'development');
  return { cwd, git, write, commit, base, verify: (head, development = 'development') => verifyDevelopmentParity({ cwd, base, head, development }) };
}
test('accepts the exact feature commit already landed in dev, including later dev changes', t => {
  const r = repository(t); r.write('feed.txt', 'retired\nsecond\nthird\n'); const head = r.commit();
  r.git('checkout', '-q', 'development'); r.git('merge', '--ff-only', head); r.write('later.txt', 'dev advance'); r.commit();
  assert.equal(r.verify(head).method, 'equivalent-patch');
});
test('accepts an equivalent cherry-pick despite divergent main/dev history and later dev work', t => {
  const r = repository(t); r.write('main-only.txt', 'main history'); const base = r.commit();
  r.write('feed.txt', 'retired\nsecond\nthird\n'); const head = r.commit();
  r.git('checkout', '-q', 'development'); r.write('dev-only.txt', 'dev history'); r.commit(); r.git('cherry-pick', head);
  r.write('later.txt', 'dev advance'); r.commit();
  assert.equal(verifyDevelopmentParity({ cwd: r.cwd, base, head, development: 'development' }).method, 'equivalent-patch');
});
test('rejects a Production-only patch', t => {
  const r = repository(t); r.write('feed.txt', 'retired\nsecond\nthird\n'); const head = r.commit();
  assert.throws(() => r.verify(head), /missing from Development/);
});
test('rejects conflicting Development state', t => {
  const r = repository(t); r.write('feed.txt', 'retired\nsecond\nthird\n'); const head = r.commit();
  r.git('checkout', '-q', 'development'); r.write('feed.txt', 'active\nsecond\nthird\n'); r.commit();
  assert.throws(() => r.verify(head), /conflict.*Development/);
});
for (const kind of ['new', 'delete', 'binary']) test(`handles ${kind} files and verifies an exact cherry-pick`, t => {
  const r = repository(t);
  if (kind === 'new') r.write('new.txt', 'new file');
  if (kind === 'delete') unlinkSync(join(r.cwd, 'delete.txt'));
  if (kind === 'binary') r.write('binary.bin', Buffer.from([0, 7, 9, 0, 10]));
  const head = r.commit(); assert.throws(() => r.verify(head), /missing from Development/);
  r.git('checkout', '-q', 'development'); r.write('dev-only.txt', 'diverge'); r.commit(); r.git('cherry-pick', head);
  assert.equal(r.verify(head).method, 'equivalent-patch');
});
test('rejects missing, malformed and non-commit references', t => {
  const r = repository(t);
  for (const ref of ['missing', '--help', 'HEAD\n', 'HEAD^{tree}']) assert.throws(() => r.verify(ref), /reference/);
  assert.throws(() => r.verify(r.base, 'missing-dev'), /reference/);
});
test('rejects a base unrelated to the Production head', t => {
  const r = repository(t); r.write('new.txt', 'new'); const future = r.commit();
  assert.throws(() => verifyDevelopmentParity({ cwd: r.cwd, base: future, head: r.base, development: 'development' }), /base must be an ancestor/);
});
test('requires exactly the documented CLI inputs', () => {
  assert.deepEqual(parseArguments(['--base', 'a', '--head', 'b', '--development', 'dev']), { base: 'a', head: 'b', development: 'dev' });
  for (const args of [[], ['--head', 'a'], ['--base', 'a', '--base', 'b', '--head', 'c', '--development', 'dev'], ['--unknown', 'a', '--head', 'b', '--development', 'dev']]) assert.throws(() => parseArguments(args));
});

test('rejects a reverted feature even when its head is an ancestor of dev', t => {
  const r = repository(t); r.write('feed.txt', 'retired\nsecond\nthird\n'); const head = r.commit();
  r.git('checkout', '-q', 'development'); r.git('merge', '--ff-only', head); r.git('revert', '--no-edit', head);
  assert.throws(() => r.verify(head), /missing from Development/);
});

test('leaves refs, index and dirty working files untouched', t => {
  const r = repository(t); r.write('feed.txt', 'retired\nsecond\nthird\n'); const head = r.commit();
  r.git('checkout', '-q', 'development'); r.git('merge', '--ff-only', head);
  r.write('feed.txt', 'uncommitted edit'); r.git('add', 'feed.txt'); r.write('untracked.txt', 'keep');
  const status = r.git('status', '--porcelain'), refs = r.git('show-ref'), index = r.git('write-tree');
  assert.equal(r.verify(head).method, 'equivalent-patch');
  assert.equal(r.git('status', '--porcelain'), status); assert.equal(r.git('show-ref'), refs); assert.equal(r.git('write-tree'), index);
});

for (const path of [
  'packages/lexicons/lexicons/site.standard.document.json',
  'packages/spec/openapi.yaml',
  'packages/spec/endpoint-manifest.json',
  'packages/swift/thin-app-view-core/Sources/ThinAppViewCore/Store.swift',
]) test(`retains strict Development parity for canonical ${path}`, t => {
  const r = repository(t);
  r.write(path, 'canonical source change\n');
  const head = r.commit();
  assert.throws(() => r.verify(head), /missing from Development/);
  r.git('checkout', '-q', 'development');
  r.write('dev-only.txt', 'unrelated Development work');
  r.commit();
  r.git('cherry-pick', head);
  assert.equal(r.verify(head).method, 'equivalent-patch');
  r.git('revert', '--no-edit', head);
  assert.throws(() => r.verify(head), /missing from Development/);
});
