// Prints every Vue template parse error with source position.
// Usage: node scripts/parse-check.mjs <file>
import {readFileSync} from 'node:fs';
import {parse} from '@vue/compiler-dom';

const file = process.argv[2];
const s = readFileSync(file, 'utf8');
const st = s.indexOf('<template>');
const tpl = s.slice(st, s.lastIndexOf('</template>') + '</template>'.length);
const baseLine = s.slice(0, st).split('\n').length;

const errs = [];
parse(tpl, {
    onError: (e) => errs.push(e),
    loc: {start: {line: baseLine, column: 1, offset: st}, end: null, source: ''},
});
for (const e of errs) console.log(`${file}(${e.loc.start.line},${e.loc.start.column}): ${e.message} [${e.code}]`);
if (!errs.length) console.log('template parses clean');
