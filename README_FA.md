<p align="center">
  <img src="build/appicon.png" width="108" alt="EagleProxy">
</p>

<h1 align="center">EagleProxy</h1>

<p align="center">کلاینت پروکسی ویندوز · اشتراک‌ها · جدول نودها · TUN / پروکسی سیستم</p>

<p align="center">
  <a href="https://github.com/sala-coding/EagleProxy/releases/latest"><img src="https://img.shields.io/github/v/release/sala-coding/EagleProxy?style=flat-square" alt="release"></a>
  <img src="https://img.shields.io/badge/platform-Windows%2010%20%7C%2011-0b6cbd?style=flat-square" alt="platform">
  <img src="https://img.shields.io/badge/core-sing--box-2f7d5a?style=flat-square" alt="core">
  <a href="LICENSE"><img src="https://img.shields.io/github/license/sala-coding/EagleProxy?style=flat-square" alt="license"></a>
</p>

<p align="center"><a href="README.md">简体中文</a> · <a href="README_EN.md">English</a> · <a href="README_RU.md">Русский</a> · <b>فارسی</b></p>

ساخته‌شده با [Wails](https://wails.io) (Go + Vue 3)، با هستهٔ [sing-box](https://github.com/SagerNet/sing-box). یک فایل اجرایی، قابل حمل: داده‌ها در پوشهٔ `config\` کنار برنامه می‌مانند، بدون نوشتن در رجیستری و بدون نصب سرویس.

## دانلود

| فایل | محتوا | کاربرد |
| --- | --- | --- |
| `Eagle-vX.Y.Z-windows-full.zip` | `Eagle.exe` + `sing-box\` | نصب نخست: از حالت فشرده خارج و اجرا کنید |
| `Eagle-vX.Y.Z-windows.exe` | فقط خود برنامه | به‌روزرسانی روی نسخهٔ قبلی؛ به‌روزرسان درون‌برنامه‌ای همین را می‌گیرد |

هسته درون برنامه نیست: برنامه `sing-box.exe` را کنار خود، در پوشهٔ `sing-box\` و نزدیک پوشهٔ داده‌ها می‌جوید. در تنظیمات → مسیر هسته فهرست می‌شود و می‌توانید بی‌درنگ هستهٔ دیگری را انتخاب کنید.

## امکانات

| امکان | توضیح |
| --- | --- |
| اشتراک‌ها | لینک اشتراک و لینک نودها، حجم و تاریخ انقضا خودکار خوانده می‌شود و به‌موقع تازه می‌گردد |
| نودها | تأخیر و سرعت زنده اندازه‌گیری و مرتب می‌شود، گروه‌ها رنگ‌بندی دارند؛ جابه‌جایی درون گروه بدون راه‌اندازی دوبارهٔ هسته |
| حالت‌ها | پروکسی سیستم / TUN از نوار کنار، آخرین نود و حالت به یاد می‌ماند؛ پشتهٔ TUN با نسخهٔ هسته هم‌آهنگ می‌شود |
| شبکه | اشتراک در شبکهٔ محلی، لوپ‌بک UWP، دو فهرست DNS با سنجش تأخیر و انتخاب سریع‌ترین، مجموعه‌قواعد `.srs` بر پایهٔ کشور |
| پایش | فهرست زندهٔ اتصال‌ها (هر اتصال را می‌توان بست)، لاگ رنگ‌بندی‌شده با توقف، پالایش و برون‌بری |
| میزکار | کلیدهای میانبر عمومی و منوی سینی سیستم، چهار زبان رابط، به‌روزرسانی از GitHub |

## شروع سریع

1. `Eagle-v*-windows-full.zip` را از صفحهٔ [Releases](https://github.com/sala-coding/EagleProxy/releases/latest) دانلود کنید
2. در هر پوشه‌ای از حالت فشرده خارج کنید، با `Eagle.exe` و `sing-box\` کنار هم
3. `Eagle.exe` را اجرا کنید، اشتراک یا لینک نود را وارد کنید و از جدول یک نود را روشن کنید

<details>
<summary><b>ساخت از سورس</b></summary>

ویندوز ۱۰ / ۱۱ (اگر WebView2 نباشد برنامه پیشنهاد نصب می‌دهد)، [Go](https://go.dev) 1.25+، [Node.js](https://nodejs.org) 18+ و [Wails CLI](https://wails.io/docs/gettingstarted/installation) نسخهٔ ۲ لازم است. هسته را خودتان بگذارید: `sing-box.exe` و `wintun.dll` را در `sing-box/` بریزید یا مسیرشان را در تنظیمات بدهید.

```cmd
wails dev
wails build                               :: بسته‌بندی بدون شمارهٔ نسخه
.\build\release.ps1 -Version v1.0.17      :: انتشار رسمی با شمارهٔ نسخه
```

خروجی: `build/bin/Eagle.exe`.

</details>

## قدردانی و مجوز

هسته [sing-box](https://github.com/SagerNet/sing-box) · چارچوب [Wails](https://wails.io) · ساختار داده و واژگان رابط با الگوگیری از [nekoray](https://github.com/MatsuriDayo/nekoray). تحت مجوز [GPL-3.0](LICENSE) منتشر می‌شود.
