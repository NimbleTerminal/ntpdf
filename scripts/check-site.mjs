// Validate the generated Pages artifact without requiring a local HTTP server.
import assert from 'node:assert/strict';
import { readFile, access } from 'node:fs/promises';

const demos = ['pdfview'];

for (const file of [
  'index.html', 'site.css', 'site.js', 'wasm_exec.js', 'booba/booba.js',
  'ghostty-web/ghostty-web.js', 'ghostty-web/ghostty-vt.wasm',
  'booba-shim/pdfium/pdfium-shim.js',
  'LICENSE',
]) await access(`web/${file}`);

for (const demo of demos) {
  const bytes = await readFile(`web/demos/${demo}.wasm`);
  assert.deepEqual([...bytes.subarray(0, 4)], [0, 97, 115, 109], `${demo}: WASM header`);
  assert.ok(WebAssembly.validate(bytes), `${demo}: valid WASM`);
}

const html = await readFile('web/index.html', 'utf8');
for (const [, path] of html.matchAll(/(?:src|href)="([^"?#]+)"/g)) {
  if (/^(https?:|\.\/)/.test(path)) continue;
  await access(`web/${path}`);
}
for (const name of demos) {
  assert.ok(html.includes(`data-demo="${name}"`), `${name}: navigation`);
}
console.log('Site links, runtime assets, and all WASM demo binaries checked.');
