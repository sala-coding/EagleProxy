// Runs the template parse check over every .vue file under src.
// Prints only files that have errors; a quiet run means all clean.
import {readdirSync, readFileSync} from 'node:fs';
import {join} from 'node:path';
import {parse} from '@vue/compiler-dom';

let bad = 0;
const walk = (d) => {
    for (const e of readdirSync(d, {withFileTypes: true})) {
        const fp = join(d, e.name);
        if (e.isDirectory()) walk(fp);
        else if (e.name.endsWith('.vue')) {
            const s = readFileSync(fp, 'utf8');
            const st = s.indexOf('<template>');
            if (st < 0) continue;
            const tpl = s.slice(st, s.lastIndexOf('</template>') + '</template>'.length);
            const errs = [];
            parse(tpl, {onError: (e) => errs.push(e)});
            if (errs.length) {
                bad++;
                console.log(`${fp}`);
                for (const e of errs) console.log(`  (line ${e.loc.start.line}) ${e.message}`);
            }
        }
    }
};
walk('src');
console.log(bad === 0 ? 'all .vue templates parse clean' : `${bad} file(s) with errors`);
