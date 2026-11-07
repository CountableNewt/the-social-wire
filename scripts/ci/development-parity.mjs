import { execFileSync } from 'node:child_process';
import { mkdirSync, mkdtempSync, rmSync } from 'node:fs';
import { fileURLToPath } from 'node:url';
import { tmpdir } from 'node:os';
import { join, resolve } from 'node:path';

function git(cwd, args, options = {}) {
  return execFileSync('git', args, { cwd, encoding: 'utf8', stdio: ['ignore', 'pipe', 'pipe'], maxBuffer: 8 * 1024 * 1024, timeout: 60_000, ...options }).trim();
}
function commit(cwd, ref) {
  if (typeof ref !== 'string' || !ref || ref.startsWith('-') || /[\s\x00-\x1f\x7f]/.test(ref)) throw new Error('Invalid commit reference.');
  try {
    const sha = git(cwd, ['rev-parse', '--verify', '--end-of-options', `${ref}^{commit}`]);
    if (!/^(?:[a-f0-9]{40}|[a-f0-9]{64})$/.test(sha)) throw new Error();
    return sha;
  } catch { throw new Error(`Commit reference is missing or invalid: ${ref}`); }
}
function ancestor(cwd, base, head) {
  try { git(cwd, ['merge-base', '--is-ancestor', base, head]); return true; }
  catch (error) { if (error.status === 1) return false; throw error; }
}

// This is an output, not a source exemption. Every excluded byte must be
// reproduced from each committed tree by the same trusted Development generator.
const derivedOutputs = [
  { path: 'packages/go/lexicons/contracts_generated.go', generator: 'scripts/go/generate-contracts.py',
    input: path => path.startsWith('packages/lexicons/') && path.endsWith('.json') },
  { path: 'packages/go/spec/contracts_generated.go', generator: 'scripts/go/generate-contracts.py',
    input: path => ['packages/spec/openapi.yaml', 'packages/spec/endpoint-manifest.json'].includes(path) },
  { path: 'packages/go/migration/source-inventory.json', generator: 'scripts/go/package-inventory.py',
    trusted: ['scripts/go/check-out-swift-contracts.py', 'packages/go/migration/extracted-swift-packages.json'],
    input: path => path.startsWith('packages/') || path.startsWith('services/') },
];

function exists(cwd, ref, path) {
  try { git(cwd, ['cat-file', '-e', `${ref}:${path}`]); return true; }
  catch (error) { if ([1, 128].includes(error.status)) return false; throw error; }
}
function unchanged(cwd, refs, path) {
  const blobs = refs.map(ref => {
    if (!exists(cwd, ref, path)) throw new Error(`Missing trusted generation input: ${path}`);
    return git(cwd, ['ls-tree', ref, '--', path]);
  });
  if (new Set(blobs).size !== 1) throw new Error(`Untrusted generator change: ${path}`);
}
function indexEnvironment(index) {
  return { ...process.env, GIT_INDEX_FILE: index,
    GIT_AUTHOR_NAME: 'Parity verification', GIT_COMMITTER_NAME: 'Parity verification',
    GIT_AUTHOR_EMAIL: 'parity@example.invalid', GIT_COMMITTER_EMAIL: 'parity@example.invalid',
    GIT_AUTHOR_DATE: '2000-01-01T00:00:00Z', GIT_COMMITTER_DATE: '2000-01-01T00:00:00Z' };
}
function withoutDerived(cwd, ref, outputs, index) {
  const env = indexEnvironment(index);
  git(cwd, ['read-tree', ref], { env });
  git(cwd, ['update-index', '--force-remove', '--', ...outputs.map(output => output.path)], { env });
  const tree = git(cwd, ['write-tree'], { env });
  return git(cwd, ['-c', 'commit.gpgsign=false', 'commit-tree', tree, '-m', 'Temporary parity proof'], { env });
}
function generationSnapshot(cwd, ref, directory, index, outputs) {
  mkdirSync(directory, { recursive: true });
  const generators = new Set(outputs.map(output => output.generator));
  const trusted = new Set(outputs.flatMap(output => output.trusted ?? []));
  const records = git(cwd, ['ls-tree', '-rz', ref]).split('\0').filter(Boolean);
  const paths = records.flatMap(record => {
    const separator = record.indexOf('\t');
    const metadata = record.slice(0, separator), path = record.slice(separator + 1);
    // Every derived output must regenerate from its committed canonical inputs.
    // Those source inputs remain subject to the exact patch proof below.
    const needed = generators.has(path) || trusted.has(path) || outputs.some(output => output.path === path) ||
      ['packages/spec/endpoint-manifest.json', 'packages/spec/openapi.yaml'].includes(path) ||
      derivedOutputs.filter(output => generators.has(output.generator)).some(output => output.input(path));
    if (!needed || path.split('/').includes('node_modules')) return [];
    if (!/^100(?:644|755) blob /.test(metadata)) throw new Error(`Generation input must be a regular committed file: ${path}`);
    if (outputs.some(output => output.path === path) && !metadata.startsWith('100644 ')) {
      throw new Error(`Derived output must retain its regular non-executable mode: ${path}`);
    }
    return [path];
  });
  const env = indexEnvironment(index);
  git(cwd, ['read-tree', ref], { env });
  git(cwd, ['checkout-index', '--force', `--prefix=${directory}/`, '-z', '--stdin'], {
    env, input: paths.join('\0') + '\0', stdio: ['pipe', 'pipe', 'pipe'],
  });
}
function verifyDerived(refs, directories, outputs) {
  // Run Python in isolated mode: only the already-verified generator can execute.
  // Each generator's exact serialization is retained, including hashes/order.
  const check = `import json,runpy,subprocess,sys\nfrom pathlib import Path\nroot=Path(sys.argv[1])\noutputs=json.loads(sys.argv[2])\ncontracts=[item for item in outputs if item['generator']=='scripts/go/generate-contracts.py']\nif contracts:\n rendered=runpy.run_path(str(root/'scripts/go/generate-contracts.py'))['render']()\n for item in contracts:\n  expected=rendered[item['path'].removeprefix('packages/go/')]\n  if (root/item['path']).read_bytes()!=expected.encode('utf-8'): raise SystemExit('Generated output drift: '+item['path'])\nif any(item['generator']=='scripts/go/package-inventory.py' for item in outputs):\n subprocess.run([sys.executable,'-I',str(root/'scripts/go/check-out-swift-contracts.py')],cwd=root,check=True)\n subprocess.run([sys.executable,'-I',str(root/'scripts/go/package-inventory.py'),'--check'],cwd=root,check=True)\n`;
  for (let i = 0; i < refs.length; i++) {
    try { execFileSync('python3', ['-I', '-c', check, directories[i], JSON.stringify(outputs)], {
      encoding: 'utf8', stdio: ['ignore', 'pipe', 'pipe'], timeout: outputs.some(output => output.generator === 'scripts/go/package-inventory.py') ? 180_000 : 60_000, maxBuffer: 1024 * 1024,
    }); } catch (error) {
      throw new Error(`Generated output verification failed at ${refs[i]}: ${String(error.stderr ?? error.message).trim().slice(0, 1200)}`);
    }
  }
}
/** Prove the Production PR patch is present in current Development, without changing refs/files. */
export function verifyDevelopmentParity({ base, head, development, cwd = process.cwd() }) {
  const baseSHA = commit(cwd, base), headSHA = commit(cwd, head), developmentSHA = commit(cwd, development);
  if (!ancestor(cwd, baseSHA, headSHA)) throw new Error('Production PR base must be an ancestor of its head.');
  const refs = [baseSHA, headSHA, developmentSHA];
  const changed = git(cwd, ['diff', '--name-only', '-z', baseSHA, headSHA]).split('\0').filter(Boolean);
  const outputs = derivedOutputs.filter(output => changed.some(path => path === output.path || path === output.generator || (output.trusted ?? []).includes(path) || output.input(path)) &&
    refs.some(ref => exists(cwd, ref, output.path)));
  for (const output of outputs) for (const ref of refs) {
    if (!exists(cwd, ref, output.path)) throw new Error(`Missing generated output at ${ref}: ${output.path}`);
  }
  for (const generator of new Set(outputs.map(output => output.generator))) unchanged(cwd, refs, generator);
  for (const path of new Set(outputs.flatMap(output => output.trusted ?? []))) unchanged(cwd, refs, path);
  const scratch = outputs.length ? mkdtempSync(join(tmpdir(), 'development-parity-proof-')) : undefined;
  try {
    const proofRefs = outputs.length ? refs.map((ref, i) => withoutDerived(cwd, ref, outputs, join(scratch, `index-${i}`))) : refs;
    let mergedTree;
    try {
      // The explicit PR base limits the proof to this PR's patch, even when main
      // and dev have unrelated changes since their historical common ancestor.
      mergedTree = git(cwd, ['merge-tree', '--write-tree', '--no-messages', `--merge-base=${proofRefs[0]}`, proofRefs[2], proofRefs[1]]);
    } catch {
      throw new Error('Production changes conflict with Development. Backport this PR to dev and validate it before promoting to main.');
    }
    const developmentTree = git(cwd, ['rev-parse', `${proofRefs[2]}^{tree}`]);
    if (mergedTree !== developmentTree) throw new Error('Production changes are missing from Development. Backport this PR to dev and validate it before promoting to main.');
    if (outputs.length) {
      const directories = refs.map((ref, i) => {
        const directory = join(scratch, `tree-${i}`);
        generationSnapshot(cwd, ref, directory, join(scratch, `snapshot-index-${i}`), outputs);
        return directory;
      });
      verifyDerived(refs, directories, outputs);
    }
    return { method: 'equivalent-patch', headSHA, developmentSHA, verifiedDerivedOutputs: outputs.map(output => output.path) };
  } finally { if (scratch) rmSync(scratch, { recursive: true, force: true }); }
}

export function parseArguments(args) {
  const options = {};
  const flags = { '--base': 'base', '--head': 'head', '--development': 'development' };
  for (let i = 0; i < args.length; i += 2) {
    const key = flags[args[i]], value = args[i + 1];
    if (!key || !value || options[key] !== undefined) throw new Error('Usage: development-parity.mjs --base <PR base> --head <PR head> --development <dev ref>');
    options[key] = value;
  }
  if (Object.keys(options).length !== 3) throw new Error('Production base, head and Development references are required.');
  return options;
}

if (process.argv[1] && resolve(process.argv[1]) === fileURLToPath(import.meta.url)) {
  try {
    const result = verifyDevelopmentParity(parseArguments(process.argv.slice(2)));
    const derived = result.verifiedDerivedOutputs.length ? `; exact regeneration: ${result.verifiedDerivedOutputs.join(', ')}` : '';
    console.log(`Development parity verified (${result.method}): ${result.headSHA} -> ${result.developmentSHA}${derived}`);
  } catch (error) {
    console.error(`Development parity failed: ${error.message}`);
    process.exitCode = 1;
  }
}
