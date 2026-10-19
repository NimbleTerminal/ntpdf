import { BoobaTerminal, installActionBar, installRendererHud, parseRendererFromURL } from './booba/booba.js';
import './ntshim/pdfium/pdfium-shim.js';

const demos = {
  gallery: 'Document gallery',
  report: 'Live report builder',
  inspector: 'Side-by-side inspector',
  pagedims: 'Aspect-matched viewport',
  pdfview: 'PDF viewer',
};
const help = {
  gallery: 'd: next doc · n/p: next/prev page · m: image/text · f: fit mode · +/-: zoom · arrows: pan · g: glyph/kitty · r: reset · q: quit.',
  report: 't: bar/line · d: dataset · c: theme · v: vary values · n/p: page · m: image/text · f: fit mode · g: glyph/kitty · r: reset · q: quit.',
  inspector: 'd: next doc · n/p: next/prev page · f: fit mode · +/-: zoom · arrows: pan · g: glyph/kitty · r: reset · q: quit.',
  pagedims: 'n/p: next/prev page (16:9 / Letter / 1:1) · a: aspect lock · m: image/text · g: glyph/kitty · r: reset · q: quit.',
  pdfview: 'n/p: next/prev page · m: image/text · f: fit mode · +/-: zoom · arrows: pan · g: glyph/kitty · r: reset · q: quit.',
};
const requested = new URLSearchParams(location.search).get('demo');
const demo = Object.hasOwn(demos, requested) ? requested : 'gallery';
const status = document.querySelector('#status');
document.querySelector('#title').textContent = demos[demo];
document.title = `${demos[demo]} · ntpdf`;
const restart = new URL(location.href);
restart.searchParams.set('demo', demo);
document.querySelector('#restart').href = restart.href;
for (const link of document.querySelectorAll('[data-demo]')) {
  if (link.dataset.demo === demo) link.setAttribute('aria-current', 'page');
}
document.querySelector('#help').textContent = help[demo];
function failed(error) {
  status.dataset.error = '';
  status.textContent = `Could not run the demo: ${error.message}. Try Restart.`;
  console.error(error);
}
async function main() {
  status.textContent = 'Initializing PDFium renderer…';
  try {
    await window.ntshim?.pdfium?.ready;
  } catch (err) {
    console.warn('PDFium init failed (ImageMode will fall back to TextMode):', err);
  }
  status.textContent = 'Starting terminal…';
  const go = new Go();
  const response = await fetch(`demos/${demo}.wasm`);
  if (!response.ok) throw new Error(`Download failed (${response.status})`);
  const { instance } = await WebAssembly.instantiateStreaming(response, go.importObject);
  const running = go.run(instance);
  running.then(() => { status.textContent = 'Session ended. Choose Restart to run again.'; }, failed);
  const terminal = new BoobaTerminal('terminal', { renderer: parseRendererFromURL(), allowOSC7501Actions: true });
  await terminal.init();
  terminal.setActionKeyEncoder({
    canEncode: gesture => terminal.encodeKey(gesture) !== null,
    encode: gesture => terminal.encodeKey(gesture),
  });
  installActionBar(terminal.actions, {
    parent: document.querySelector('#controls'),
    openKeyboard: () => terminal.focus(),
    onLayout: () => terminal.fitAddon?.fit(),
  });
  terminal.onActionError = failed;
  window.addEventListener('pagehide', () => terminal.dispose(), { once: true });
  terminal.connectWasm();
  terminal.focus();
  installRendererHud(terminal.term, {
    parent: document.querySelector('.terminal-frame'),
    position: 'absolute',
    bindToggleHotkey: false,
  });
  status.textContent = 'Running locally in your browser. No terminal server required.';
}
main().catch(failed);
