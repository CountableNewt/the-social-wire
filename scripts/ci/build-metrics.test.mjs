import assert from 'node:assert/strict';
import test from 'node:test';
import { mkdtempSync, mkdirSync, readFileSync, rmSync, symlinkSync, writeFileSync } from 'node:fs';
import { tmpdir } from 'node:os';
import { join } from 'node:path';
import { cacheBytes, recordBuildMetrics } from './build-metrics.mjs';
import { build } from './verify-runtime-images.mjs';

test('cache measurement counts nested files and ignores links and missing caches', () => {
  const root = mkdtempSync(join(tmpdir(), 'tsw-cache-metrics-'));
  try {
    mkdirSync(join(root, 'blobs'));
    writeFileSync(join(root, 'index.json'), '123');
    writeFileSync(join(root, 'blobs', 'blob'), '12345');
    symlinkSync(root, join(root, 'cycle'));
    assert.equal(cacheBytes(root), 8);
    assert.equal(cacheBytes(join(root, 'missing')), 0);
    assert.equal(cacheBytes(undefined), 0);
  } finally { rmSync(root, { recursive: true, force: true }); }
});

test('build metrics include exact commit, elapsed time and failed builds', () => {
  const sha = 'a'.repeat(40);
  for (const fail of [false, true]) {
    let clock = 1000, measured;
    const invoke = () => build({ sha, appEnvironment: 'dev' }, 'appview', 'backend-tests', {
      environment: {}, now: () => { clock += 250; return clock; },
      execute: () => { if (fail) throw new Error('compiler failure'); },
      record: value => { measured = value; },
    });
    if (fail) assert.throws(invoke, /compiler failure/); else invoke();
    assert.deepEqual(measured, { sha, kind: 'appview', target: 'backend-tests', seconds: 0.25,
      importedBytes: 0, exportedBytes: 0, result: fail ? 'failed' : 'passed' });
  }
});

test('build summary records transferable cache sizes and timing without environment values', () => {
  const root = mkdtempSync(join(tmpdir(), 'tsw-cache-summary-'));
  try {
    const path = join(root, 'summary.md');
    const metrics = { sha: 'a'.repeat(40), kind: 'appview', target: 'backend-tests', seconds: 12.5, importedBytes: 8, exportedBytes: 9, result: 'passed' };
    const logs = [];
    recordBuildMetrics(metrics, { GITHUB_STEP_SUMMARY: path, SECRET: 'do-not-record' }, value => logs.push(value));
    assert.deepEqual(JSON.parse(logs[0].slice('Build metrics: '.length)), metrics);
    assert.match(readFileSync(path, 'utf8'), /12\.5 \| 8 \| 9 \| passed/);
    assert.doesNotMatch(readFileSync(path, 'utf8'), /do-not-record/);
  } finally { rmSync(root, { recursive: true, force: true }); }
});

test('incomplete cache directories do not count as imported bytes', () => {
  const root = mkdtempSync(join(tmpdir(), 'tsw-incomplete-cache-'));
  try {
    mkdirSync(join(root, 'appview'));
    writeFileSync(join(root, 'appview', 'partial-blob'), 'not-an-export');
    let metrics;
    build({ sha: 'a'.repeat(40), appEnvironment: 'dev' }, 'appview', 'media-tests', {
      environment: { CI_DOCKER_CACHE_DIR: root }, execute: (_command, args) => {
        assert.ok(!args.includes('--cache-from'));
      }, record: value => { metrics = value; },
    });
    assert.equal(metrics.importedBytes, 0);
  } finally { rmSync(root, { recursive: true, force: true }); }
});
