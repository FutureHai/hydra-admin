import fs from 'node:fs'
import path from 'node:path'
import { fileURLToPath } from 'node:url'

// @rc-component/util 1.13.0 injects a temporary scrollbar <style> without
// passing its existing dynamicCSS CSP option. Fail on version drift so CI
// rechecks the upstream fix rather than silently dropping nonce protection.
const webRoot = path.resolve(path.dirname(fileURLToPath(import.meta.url)), '..')
const packageRoot = path.join(webRoot, 'node_modules/@rc-component/util')
const pkg = JSON.parse(fs.readFileSync(path.join(packageRoot, 'package.json'), 'utf8'))
if (pkg.version !== '1.13.0') throw new Error(`Review scrollbar CSP patch for @rc-component/util ${pkg.version}`)

const replacement = "}`, randomId, { csp: { nonce: document.querySelector('meta[name=\"csp-nonce\"]')?.getAttribute('content') } });"
for (const build of ['es', 'lib']) {
  const file = path.join(packageRoot, build, 'getScrollBarSize.js')
  const source = fs.readFileSync(file, 'utf8')
  if (source.includes(replacement)) continue
  const needle = '}`, randomId);'
  if (source.split(needle).length !== 2) throw new Error(`Review scrollbar CSP patch context in ${file}`)
  fs.writeFileSync(file, source.replace(needle, replacement))
}
