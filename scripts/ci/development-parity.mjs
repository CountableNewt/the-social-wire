import { execFileSync } from 'node:child_process';
import { fileURLToPath } from 'node:url';
import { resolve } from 'node:path';

function git(cwd, args) {
  return execFileSync('git', args, { cwd, encoding: 'utf8', stdio: ['ignore', 'pipe', 'pipe'], maxBuffer: 4 * 1024 * 1024 }).trim();
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

/** Prove the Production PR patch is present in current Development, without changing refs/files. */
export function verifyDevelopmentParity({ base, head, development, cwd = process.cwd() }) {
  const baseSHA = commit(cwd, base), headSHA = commit(cwd, head), developmentSHA = commit(cwd, development);
  if (!ancestor(cwd, baseSHA, headSHA)) throw new Error('Production PR base must be an ancestor of its head.');
  let mergedTree;
  try {
    // The explicit PR base limits the proof to this PR's patch, even when main
    // and dev have unrelated changes since their historical common ancestor.
    mergedTree = git(cwd, ['merge-tree', '--write-tree', '--no-messages', `--merge-base=${baseSHA}`, developmentSHA, headSHA]);
  } catch {
    throw new Error('Production changes conflict with Development. Backport this PR to dev and validate it before promoting to main.');
  }
  const developmentTree = git(cwd, ['rev-parse', `${developmentSHA}^{tree}`]);
  if (mergedTree !== developmentTree) throw new Error('Production changes are missing from Development. Backport this PR to dev and validate it before promoting to main.');
  return { method: 'equivalent-patch', headSHA, developmentSHA };
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
    console.log(`Development parity verified (${result.method}): ${result.headSHA} -> ${result.developmentSHA}`);
  } catch (error) {
    console.error(`Development parity failed: ${error.message}`);
    process.exitCode = 1;
  }
}
