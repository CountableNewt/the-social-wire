import { appendFileSync, lstatSync, readdirSync } from 'node:fs';
import { join } from 'node:path';

// Do not follow links in restored caches.
export function cacheBytes(directory) {
  if (!directory) return 0;
  let entry;
  try { entry = lstatSync(directory); } catch (error) {
    if (error.code === 'ENOENT') return 0;
    throw error;
  }
  if (entry.isSymbolicLink()) return 0;
  if (entry.isFile()) return entry.size;
  if (!entry.isDirectory()) return 0;
  return readdirSync(directory).reduce((total, name) => total + cacheBytes(join(directory, name)), 0);
}

export function recordBuildMetrics(metrics, environment, log = console.log) {
  log(`Build metrics: ${JSON.stringify(metrics)}`);
  if (environment.GITHUB_STEP_SUMMARY) {
    appendFileSync(environment.GITHUB_STEP_SUMMARY,
      `\n| Runtime | Target | Commit | Seconds | Imported Cache Bytes | Exported Cache Bytes | Result |\n| --- | --- | --- | ---: | ---: | ---: | --- |\n| ${metrics.kind} | ${metrics.target} | ${metrics.sha} | ${metrics.seconds} | ${metrics.importedBytes} | ${metrics.exportedBytes} | ${metrics.result} |\n`);
  }
}
