import { test } from 'node:test';
import assert from 'node:assert/strict';
import { execFileSync } from 'node:child_process';
import { chmodSync, mkdirSync, mkdtempSync, readFileSync, symlinkSync, writeFileSync, unlinkSync, rmSync } from 'node:fs';
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

const bundlePath = 'packages/go/lexicons/contracts_generated.go';
const generatorPath = 'scripts/go/generate-contracts.py';
function lexiconRepository(t, corruptBase = false) {
  const r = repository(t);
  r.write(generatorPath, readFileSync(new URL('../go/generate-contracts.py', import.meta.url)));
  r.write('packages/spec/endpoint-manifest.json', '{"entries":[]}\n');
  r.write('packages/spec/openapi.yaml', 'openapi: 3.1.0\npaths: {}\n');
  const schema = id => r.write(`packages/lexicons/${id}.json`, JSON.stringify({ lexicon: 1, id, defs: { main: { type: 'string' } } }) + '\n');
  const generate = () => execFileSync('python3', ['-I', generatorPath], { cwd: r.cwd, stdio: ['ignore', 'pipe', 'pipe'] });
  schema('app.example.base'); generate();
  if (corruptBase) r.write(bundlePath, readFileSync(join(r.cwd, bundlePath), 'utf8') + '// stale baseline\n');
  const base = r.commit(); r.git('branch', '-f', 'development', base);
  return { ...r, base, schema, generate,
    verify: (head, development = 'development') => verifyDevelopmentParity({ cwd: r.cwd, base, head, development }),
  };
}
function sharedLexiconFeature(r, { headDrift = false, developmentDrift = false } = {}) {
  r.schema('app.example.article'); r.generate();
  if (headDrift) r.write(bundlePath, readFileSync(join(r.cwd, bundlePath), 'utf8') + '// stale production\n');
  const head = r.commit();
  r.git('checkout', '-q', 'development'); r.git('merge', '--ff-only', head);
  r.schema('app.example.liveEvent'); r.generate();
  if (developmentDrift) r.write(bundlePath, readFileSync(join(r.cwd, bundlePath), 'utf8') + '// stale development\n');
  r.commit();
  return head;
}

test('verifies an idempotent Lexicon patch despite unrelated Development schemas in the generated line', t => {
  const r = lexiconRepository(t), head = sharedLexiconFeature(r);
  assert.throws(() => r.git('merge-tree', '--write-tree', '--no-messages', `--merge-base=${r.base}`, 'development', head));
  const result = r.verify(head);
  assert.deepEqual(result.verifiedDerivedOutputs, [bundlePath]);
  assert.equal(result.method, 'equivalent-patch');
});
test('rejects a Production-only canonical Lexicon even when its generated bundle is correct', t => {
  const r = lexiconRepository(t);
  r.schema('app.example.article'); r.generate(); const head = r.commit();
  r.git('checkout', '-q', 'development'); r.schema('app.example.liveEvent'); r.generate(); r.commit();
  assert.throws(() => r.verify(head), /missing from Development|conflict.*Development/);
});
for (const where of ['base', 'head', 'development']) test(`rejects generated output drift in ${where}`, t => {
  const r = lexiconRepository(t, where === 'base');
  const head = sharedLexiconFeature(r, { headDrift: where === 'head', developmentDrift: where === 'development' });
  assert.throws(() => r.verify(head), /Generated output.*drift/s);
});
test('rejects a stale unchanged bundle when canonical Production schemas changed', t => {
  const r = lexiconRepository(t);
  r.schema('app.example.article'); const head = r.commit();
  r.git('checkout', '-q', 'development'); r.git('merge', '--ff-only', head);
  r.schema('app.example.liveEvent'); r.generate(); r.commit();
  assert.throws(() => r.verify(head), /Generated output.*drift/s);
});
test('rejects a generator change even when backported identically to Development', t => {
  const r = lexiconRepository(t);
  r.schema('app.example.article'); r.generate();
  r.write(generatorPath, readFileSync(join(r.cwd, generatorPath), 'utf8') + '\nraise SystemExit("must never execute untrusted generator")\n');
  const head = r.commit(); r.git('checkout', '-q', 'development'); r.git('merge', '--ff-only', head);
  assert.throws(() => r.verify(head), /Untrusted generator change/);
});
for (const kind of ['missing', 'executable', 'symlink']) test(`rejects ${kind} generated output`, t => {
  const r = lexiconRepository(t);
  r.schema('app.example.article'); r.generate();
  if (kind === 'missing') unlinkSync(join(r.cwd, bundlePath));
  if (kind === 'executable') chmodSync(join(r.cwd, bundlePath), 0o755);
  if (kind === 'symlink') { unlinkSync(join(r.cwd, bundlePath)); symlinkSync('../../spec/openapi.yaml', join(r.cwd, bundlePath)); }
  const head = r.commit(); r.git('checkout', '-q', 'development'); r.git('merge', '--ff-only', head);
  assert.throws(() => r.verify(head), /Missing generated output|non-executable mode|regular committed file/);
});
for (const path of ['packages/go/spec/contracts_generated.go', 'packages/go/migration/source-inventory.json']) {
  test(`keeps ${path} subject to the exact source patch proof`, t => {
    const r = lexiconRepository(t); r.write(path, 'Production-only generated change\n'); const head = r.commit();
    assert.throws(() => r.verify(head), /missing from Development/);
  });
}
test('verified derivation preserves dirty files, the original index and refs', t => {
  const r = lexiconRepository(t), head = sharedLexiconFeature(r);
  r.write(bundlePath, 'uncommitted generated edit'); r.git('add', bundlePath); r.write('untracked.txt', 'keep');
  const status = r.git('status', '--porcelain'), refs = r.git('show-ref'), index = r.git('write-tree');
  assert.deepEqual(r.verify(head).verifiedDerivedOutputs, [bundlePath]);
  assert.equal(r.git('status', '--porcelain'), status); assert.equal(r.git('show-ref'), refs); assert.equal(r.git('write-tree'), index);
});
