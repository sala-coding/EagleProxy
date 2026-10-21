// probe-jsoncards.mjs — 由 JSON 表单驱动的设置卡片（入站/订阅/核心/测速/快捷键/其它）：
// 每一张都要能打开、看得见字段、改一个值、保存，并确认后端收到了。
const R = {};
const nav = n => $$('.set-nav-item').find(b => T(b).includes(n));
const slideFor = (name) => {
    click(nav(name));
    return null;
};

const CARDS = ['入站监听', '订阅管理', '核心与日志', '测速与统计', '全局快捷键', '其它'];

// 打开设置面板
click($('.status-logo'));
await sleep(1500);
R.dockOpen = !!$('.settings-dock');

for (const card of CARDS) {
    await step(card, async () => {
        const btn = nav(card);
        if (!btn) return 'no nav item';
        click(btn);
        await sleep(900);
        const slide = $$('.set-group.set-slide').find(s => s.classList.contains('on'));
        const info = {
            title: slide ? T(slide.querySelector('.set-group-title')) : null,
            inputs: slide ? $$('input, select, textarea', slide).map(i => ({
                type: i.type, label: i.getAttribute('aria-label') || i.placeholder || '',
                val: i.value, cls: i.className,
            })) : [],
            text: slide ? T(slide).slice(0, 220) : null,
        };
        // 改一个文本字段，确认保存被触发
        const text = slide ? $$('input[type=text], input[type=number], textarea', slide).find(i => i.value !== '') : null;
        if (text) {
            const b0 = calls().length;
            const proto = text instanceof HTMLTextAreaElement ? HTMLTextAreaElement.prototype : HTMLInputElement.prototype;
            Object.getOwnPropertyDescriptor(proto, 'value').set.call(text, text.value + 'x');
            text.dispatchEvent(new Event('input', {bubbles: true}));
            await sleep(1500);   // 等过 650ms 的防抖
            info.saveCalls = calls().slice(b0).map(c => c.name);
            info.updateSettingsArg = calls('UpdateSettings').slice(-1).map(c => JSON.stringify(c.args[0] || '').slice(0, 160));
        }
        R[card] = info;
    });
}

R.errs = errs().map(e => e.text.split('\n')[0]);
R.crash = !!$('.crash');
return JSON.stringify(R, null, 2);
