import { appendFileSync, readFileSync } from 'node:fs';
import { pathToFileURL } from 'node:url';
import { githubReadAPI, verifyMergedCommit } from './validated-merge.mjs';

const sha = value => typeof value === 'string' && /^[a-f0-9]{40}$/.test(value) && !/^0+$/.test(value);
const repositoryName = value => typeof value === 'string' && /^[A-Za-z0-9_.-]+\/[A-Za-z0-9_.-]+$/.test(value);
const flags = ['true', 'false'];
const noReuse = reason => ({ reuseWeb: false, reuseAppview: false, reason });

/** All unsupported or incomplete evidence falls back to ordinary target builds. */
export async function reusePRVerification(event, environment, {
  verify = verifyMergedCommit,
  createAPI = githubReadAPI,
} = {}) {
  if (environment.GITHUB_EVENT_NAME !== 'push' || !['refs/heads/dev', 'refs/heads/main'].includes(environment.GITHUB_REF)) return noReuse('This event requires ordinary verification.');
  if (!event || typeof event !== 'object' || Array.isArray(event)
    || event.ref !== environment.GITHUB_REF || event.deleted === true
    || !sha(environment.GITHUB_SHA) || event.after !== environment.GITHUB_SHA
    || !repositoryName(environment.GITHUB_REPOSITORY)
    || typeof event.repository?.full_name !== 'string' || event.repository.full_name.toLowerCase() !== environment.GITHUB_REPOSITORY.toLowerCase()
    || !flags.includes(environment.INPUT_WEB) || !flags.includes(environment.INPUT_APPVIEW)) return noReuse('Push context or target flags could not be verified.');
  const web = environment.INPUT_WEB === 'true', appview = environment.INPUT_APPVIEW === 'true';
  if (!web && !appview) return noReuse('No runtime target changed.');
  if (typeof environment.GITHUB_TOKEN !== 'string' || !environment.GITHUB_TOKEN) return noReuse('PR evidence is unavailable; ordinary verification will run.');
  try {
    const baseRef = environment.GITHUB_REF.slice('refs/heads/'.length);
    const evidence = await verify(createAPI(environment.GITHUB_TOKEN), environment.GITHUB_REPOSITORY, environment.GITHUB_SHA, { baseRef, requireTestedTree: true });
    // Do not interpret missing target proof as success, or trust metadata outside
    // the verifier's exact merged commit and immutable CI run evidence.
    if (!evidence || evidence.baseRef !== baseRef || evidence.mergeSHA !== environment.GITHUB_SHA || !sha(evidence.headSHA) || !sha(evidence.treeSHA)
      || !Number.isSafeInteger(evidence.prNumber) || evidence.prNumber < 1
      || !Number.isSafeInteger(evidence.runID) || evidence.runID < 1
      || !evidence.targets || typeof evidence.targets.web !== 'boolean' || typeof evidence.targets.appview !== 'boolean') return noReuse('Complete PR target evidence is unavailable; ordinary verification will run.');
    return {
      reuseWeb: web && evidence.targets.web,
      reuseAppview: appview && evidence.targets.appview,
      reason: `Verified PR #${evidence.prNumber}, CI run ${evidence.runID}, source tree ${evidence.treeSHA}.`,
    };
  } catch {
    // API exceptions may contain credentials or signed URLs. Keep logs fixed,
    // and never turn failed/missing/old evidence into a successful skip.
    return noReuse('PR evidence could not be reused; ordinary verification will run.');
  }
}

export function reuseOutputs(result) {
  if (!result || typeof result.reuseWeb !== 'boolean' || typeof result.reuseAppview !== 'boolean') throw new Error('Invalid verification reuse outputs.');
  return `reuse_web=${result.reuseWeb}\nreuse_appview=${result.reuseAppview}\n`;
}

export async function main(environment = process.env, {
  read = readFileSync,
  append = appendFileSync,
  log = console.log,
  decide = reusePRVerification,
} = {}) {
  if (typeof environment.GITHUB_OUTPUT !== 'string' || !environment.GITHUB_OUTPUT) throw new Error('Missing GitHub output file.');
  let result;
  try {
    const event = JSON.parse(read(environment.GITHUB_EVENT_PATH, 'utf8'));
    result = await decide(event, environment);
  } catch { result = noReuse('Push context or PR evidence is unavailable; ordinary verification will run.'); }
  append(environment.GITHUB_OUTPUT, reuseOutputs(result));
  // Only the fixed reasons or strictly validated numeric/hash metadata above
  // reach workflow logs; raw GitHub API responses and tokens never do.
  log(result.reason);
  return result;
}

if (process.argv[1] && import.meta.url === pathToFileURL(process.argv[1]).href) {
  main().catch(() => { console.error('Could not record verification reuse outputs.'); process.exitCode = 1; });
}
