// Exercise one real WASM program through booba's JS bridge in a fresh process.
// This checks startup, OSC 7501 action publishing, input state transitions, resize, and clean exit.
import assert from 'node:assert/strict';
import { readFile } from 'node:fs/promises';
import vm from 'node:vm';
import { setTimeout as delay } from 'node:timers/promises';

const demo = process.argv[2];
assert.ok(['pdfview'].includes(demo));

vm.runInThisContext(await readFile('web/wasm_exec.js', 'utf8'));
const go = new Go();
let exitCode;
go.exit = code => { exitCode = code; };
const { instance } = await WebAssembly.instantiate(await readFile(`web/demos/${demo}.wasm`), go.importObject);
const timeout = setTimeout(() => { console.error(`${demo}: timed out`); process.exit(1); }, 15000);
const running = go.run(instance);

try {
  for (let n = 0; typeof globalThis.bubbletea_read !== 'function' && n < 100; n++) await delay(20);
  assert.equal(typeof globalThis.bubbletea_read, 'function', 'booba bridge installed');
  bubbletea_resize(80, 28);

  let output = '';
  for (let n = 0; n < 100 && !output.includes('\x1b]7501;?'); n++) {
    await delay(25);
    output += bubbletea_read();
  }
  assert.ok(output.includes('\x1b]7501;?'), 'application queries action support');
  bubbletea_write('\x1b]7501;?\x1b\\');

  let report;
  for (let n = 0; n < 100 && !report; n++) {
    await delay(25);
    output += bubbletea_read();
    report = output.match(/\x1b\]7501;state=idle:[^\x1b]*:msg=([A-Za-z0-9+/=]+)\x1b\\/);
  }
  assert.ok(report, 'application publishes OSC action snapshot');
  const payload = Buffer.from(report[1], 'base64').toString('utf8');
  const snapshot = JSON.parse(payload.split(' | booba-actions/v1 ')[1]);
  const quit = snapshot.actions.find(action => action.label === 'Quit');
  assert.ok(quit?.enabled, 'quit action enabled');

  async function expectSummary(expected) {
    let received = '';
    for (let n = 0; n < 100; n++) {
      await delay(20);
      received += bubbletea_read();
      for (const match of received.matchAll(/\x1b\]7501;state=idle:[^\x1b]*:msg=([A-Za-z0-9+/=]+)\x1b\\/g)) {
        const message = Buffer.from(match[1], 'base64').toString('utf8');
        if (message.split(' | booba-actions/v1 ')[0] === expected) return;
      }
    }
    assert.fail(`Missing summary state: ${expected}; received ${JSON.stringify(received)}`);
  }

  async function send(bytes, summary) {
    bubbletea_read();
    bubbletea_write(bytes);
    await delay(25);
    bubbletea_write('\x1b]7501;?\x1b\\');
    await expectSummary(summary);
  }

  bubbletea_resize(72, 24);
  if (demo === 'pdfview') {
    await send('n', 'Viewer: name=Example.pdf page=2/3 mode=text fit=contain zoom=1x');
    await send('f', 'Viewer: name=Example.pdf page=2/3 mode=text fit=fill zoom=1x');
    await send('p', 'Viewer: name=Example.pdf page=1/3 mode=text fit=fill zoom=1x');
  }

  bubbletea_write(quit.keys[quit.preferred].key);
  await running;
  assert.equal(exitCode, 0);
  assert.equal(globalThis.bubbletea_exited, true, 'exit published to adapter');
  console.log(`${demo}: startup, actions, resize, exact demo state transitions, and clean exit passed`);
} finally {
  clearTimeout(timeout);
}
process.exit(0);
