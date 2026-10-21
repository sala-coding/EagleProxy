// probe-17.mjs — 崩溃后的窗口状态：CrashBoundary 是否接管。
return JSON.stringify({
    crashScreen: !!$('.crash'),
    crashText: T($('.crash')).slice(0, 200),
    appStillThere: !!$('.app'),
    settingsDockStillThere: !!$('.settings-dock'),
    errs: errs().map(e => e.text.split('\n')[0]),
}, null, 2);
