import { resolve } from 'node:path';
import { fileURLToPath } from 'node:url';

function booleanOutput(environment, name) {
  const value = environment[name];
  if (value !== 'true' && value !== 'false') throw new Error(`${name} must be an explicit true or false output.`);
  return value === 'true';
}

/** Validate current job outcomes; reuse outputs must come from the workflow's proof runner. */
export function verifyRequiredChecks(environment) {
  if (environment.CHANGES_RESULT !== 'success') throw new Error(`Path detection failed (${environment.CHANGES_RESULT ?? 'missing'}).`);
  const outcomes = {};
  for (const [key, name] of [['WEB', 'Web'], ['APPVIEW', 'SQLite AppView']]) {
    const required = booleanOutput(environment, `${key}_FLAG`);
    const reused = booleanOutput(environment, `REUSE_${key}`);
    const result = environment[`${key}_RESULT`];
    if (reused) {
      if (environment.GITHUB_EVENT_NAME !== 'push' || !['refs/heads/dev', 'refs/heads/main'].includes(environment.GITHUB_REF)) {
        throw new Error(`${name} verification may only be reused on a push to dev or main.`);
      }
      if (!required || result !== 'skipped') throw new Error(`${name} reuse requires changed paths and a skipped job (got ${result ?? 'missing'}).`);
      outcomes[key.toLowerCase()] = 'reused';
    } else if (result === 'success') {
      outcomes[key.toLowerCase()] = 'passed';
    } else if (!required && result === 'skipped') {
      outcomes[key.toLowerCase()] = 'not-required';
    } else {
      throw new Error(`${name} ${required ? 'must pass when its paths change' : 'must pass or be skipped when unchanged'} (got ${result ?? 'missing'}).`);
    }
  }
  return outcomes;
}

if (process.argv[1] && resolve(process.argv[1]) === fileURLToPath(import.meta.url)) {
  try {
    const outcomes = verifyRequiredChecks(process.env);
    console.log(`Required checks verified: Web ${outcomes.web}; SQLite AppView ${outcomes.appview}.`);
  } catch (error) {
    console.error(`Required checks failed: ${error.message}`);
    process.exitCode = 1;
  }
}
