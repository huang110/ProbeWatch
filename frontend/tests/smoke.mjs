import { existsSync, readdirSync, readFileSync } from "node:fs"
import { resolve } from "node:path"
import "./carrier_matching.test.mjs"
import "./ip_quality_ui.test.mjs"

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
const largestAsset = assetFiles.reduce((largest, file) => {
  const size = readFileSync(file).byteLength
  return size > largest.size ? { file, size } : largest
}, { file: "", size: 0 })
const maxAssetBytes = Number(process.env.PROBEWATCH_MAX_ASSET_BYTES || 450 * 1024)
if (!Number.isFinite(maxAssetBytes) || maxAssetBytes <= 0) throw new Error("invalid PROBEWATCH_MAX_ASSET_BYTES")
if (largestAsset.size > maxAssetBytes) {
  throw new Error("largest frontend asset exceeds budget: " + largestAsset.file + " (" + largestAsset.size + " bytes > " + maxAssetBytes + ")")
}
console.log("largest asset: " + largestAsset.file + " (" + largestAsset.size + " bytes)")
console.log("frontend smoke check passed: v" + expectedVersion + ", " + assetRefs.length + " entry assets")
