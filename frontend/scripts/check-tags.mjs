// Finds unclosed/mismatched tags in an SFC template by walking the tag stack.
// Attribute values and interpolations are blanked (newlines preserved, so the
// reported line numbers stay true), so `=>` or `<` inside expressions cannot
// confuse the tag tokenizer. On a closing tag, elements sitting above the
// matching opener are reported as unclosed.
// Usage: node scripts/check-tags.mjs <file>
import {readFileSync} from 'node:fs';

const file = process.argv[2];
let s = readFileSync(file, 'utf8');
const tplStart = s.indexOf('<template>');
const tplEnd = s.lastIndexOf('</template>');
const baseLine = s.slice(0, tplStart).split('\n').length;
let tpl = s.slice(tplStart + 10, tplEnd);

// blank comments, quoted attribute values and interpolations, keeping newlines
const blank = (src) => src.replace(/[^\n]/g, ' ');
tpl = tpl.replace(/<!--[\s\S]*?-->/g, blank)
         .replace(/"([^"\\]|\\.)*"/g, (m) => `"${m.slice(1, -1).replace(/[^\n]/g, ' ')}"`)
         .replace(/'([^'\\]|\\.)*'/g, (m) => `'${m.slice(1, -1).replace(/[^\n]/g, ' ')}'`)
         .replace(/\{\{[\s\S]*?\}\}/g, blank);

const VOID = new Set(['br', 'img', 'hr', 'input', 'meta', 'link', 'source', 'wbr']);
const re = /<\/?[A-Za-z][^>]*?>/g;
const stack = [];
let m;
while ((m = re.exec(tpl))) {
    const tag = m[0];
    const closing = tag.startsWith('</');
    const self = /\/>$/.test(tag);
    const name = tag.replace(/^<\/?/, '').match(/^[A-Za-z0-9-]+/)[0];
    const line = baseLine + tpl.slice(0, m.index).split('\n').length;
    if (closing) {
        const i = stack.findLastIndex((t) => t.name === name);
        if (i < 0) {
            console.log(`line ${line}: </${name}> has no matching opener`);
        } else {
            for (const t of stack.slice(i + 1))
                console.log(`line ${line}: </${name}> arrives while <${t.name}> (line ${t.line}) is still open`);
            stack.length = i;
        }
    } else if (!self && !VOID.has(name.toLowerCase())) {
        stack.push({name, line});
    }
}
console.log('EOF; still open:');
for (const t of stack) console.log(`  <${t.name}> line ${t.line}`);
