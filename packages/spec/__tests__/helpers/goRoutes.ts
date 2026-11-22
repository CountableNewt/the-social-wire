import { readdirSync, readFileSync } from "node:fs";
import { join } from "node:path";

const root = join(import.meta.dir, "../../../..");
function files(directory: string): string[] {
  return readdirSync(directory, { withFileTypes: true }).flatMap(entry => {
    const path = join(directory, entry.name);
    return entry.isDirectory() ? files(path) : entry.name.endsWith(".go") && !entry.name.endsWith("_test.go") ? [path] : [];
  });
}

/** Expand the literal routes, constant prefixes, string loops and reader route
 * table used by the Go router. Unknown registration expressions fail closed so
 * changing the registration style cannot silently weaken drift coverage. */
export function registeredGoRoutes(): Set<string> {
  const routes = new Set<string>();
  const sources = [...files(join(root, "services/appview/internal")), join(root, "services/appview/cmd/appview/router.go")];
  for (const file of sources.filter(path => !path.includes("/internal/httpx/"))) {
    const source = readFileSync(file, "utf8").replace(/^\s*\/\/.*$/gm, "");
    const constants = new Map([...source.matchAll(/const\s+(\w+)\s*=\s*"([^"]+)"/g)].map(match => [match[1]!, match[2]!]));
    const evaluate = (expression: string, bindings = new Map<string, string>()): string => expression.split("+").map(part => {
      const value = part.trim();
      if (/^"[^"]*"$/.test(value)) return JSON.parse(value) as string;
      const resolved = bindings.get(value) ?? constants.get(value);
      if (resolved === undefined) throw new Error(`Unknown Go route expression ${expression} in ${file}`);
      return resolved;
    }).join("");
    const handled = new Set<number>();
    const add = (method: string, path: string) => routes.add(`${method.toUpperCase()} ${path}`);
    // Loop variables may be a literal route, metadata constant, or suffix.
    for (const loop of source.matchAll(/for _, (\w+) := range \[\]string\{([^}]+)\}\s*\{\s*(?:\w+\s*:=\s*[^\n]+\n\s*)?(?:group|router|protected|g)\.(Get|Post|Put|Patch|Delete)\(([^,\n]+),/g)) {
      const [whole, variable, values, method, expression] = loop;
      const registration = loop.index! + whole!.search(/(?:group|router|protected|g)\.(?:Get|Post|Put|Patch|Delete)\(/);
      handled.add(registration);
      for (const item of values!.split(",").map(value => value.trim()).filter(Boolean)) {
        add(method!, evaluate(expression!, new Map([[variable!, evaluate(item)]])));
      }
    }
    for (const match of source.matchAll(/(?:group|router|protected|g)\.(Get|Post|Put|Patch|Delete)\(([^,\n]+),/g)) {
      if (handled.has(match.index!)) continue;
      add(match[1]!, evaluate(match[2]!));
    }
    // The reader table serves REST compatibility routes and their canonical
    // XRPC equivalents. DELETE compatibility routes become POST procedures.
    if (file.endsWith("/reader/routes.go")) {
      for (const match of source.matchAll(/\{"(GET|POST|DELETE)", "([^\"]+)", "(\w+)", s\.\w+\}/g)) {
        add(match[1]!, match[2]!);
        add(match[1] === "DELETE" ? "POST" : match[1]!, `/xrpc/app.thesocialwire.appview.${match[3]}`);
      }
    }
    for (const match of source.matchAll(/(?:group|router|protected|g)\.Handle\(([^,]+),\s*([^,]+),/g)) {
      if (file.endsWith("/reader/routes.go") && ["route.method", "method"].includes(match[1]!.trim())) continue;
      add(evaluate(match[1]!), evaluate(match[2]!));
    }
  }
  return routes;
}
