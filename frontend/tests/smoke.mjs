import { existsSync, readdirSync, readFileSync } from "node:fs"
import { resolve } from "node:path"

const root = resolve(process.cwd())
const packageJson = JSON.parse(readFileSync(resolve(root, "package.json"), "utf8"))
const dist = resolve(root, "dist")
const indexPath = resolve(dist, "index.html")
if (!existsSync(indexPath)) throw new Error("frontend dist/index.html is missing")
const html = readFileSync(indexPath, "utf8")
if (!html.includes("<div id=\"root\"></div>")) throw new Error("frontend root mount is missing")
if (!html.includes("<script type=\"module\"")) throw new Error("frontend module entry is missing")
const assetRefs = [...html.matchAll(/(?:src|href)=\"([^\"?#]+)\"/g)].map((match) => match[1]).filter((ref) => ref.startsWith("/assets/"))
for (const ref of assetRefs) { if (!existsSync(resolve(dist, ref.slice(1)))) throw new Error("missing referenced asset: " + ref) }
const expectedVersion = packageJson.version
const assetDir = resolve(dist, "assets")
const assetFiles = readdirSync(assetDir).map((file) => resolve(assetDir, file))
const bundleText = assetFiles.map((file) => readFileSync(file, "utf8")).join("\n")
if (!bundleText.includes(expectedVersion)) throw new Error("built assets do not contain frontend version " + expectedVersion)
console.log("frontend smoke check passed: v" + expectedVersion + ", " + assetRefs.length + " entry assets")
