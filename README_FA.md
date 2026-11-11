<p align="center">
  <img src="build/banner.svg" width="100%" alt="EagleProxy">
</p>

<p align="center">
  <a href="https://github.com/sala-coding/EagleProxy/releases/latest"><img src="https://img.shields.io/github/v/release/sala-coding/EagleProxy?style=flat-square" alt="release"></a>
  <img src="https://img.shields.io/badge/platform-Windows%2010%20%7C%2011-0b6cbd?style=flat-square" alt="platform">
  <img src="https://img.shields.io/badge/core-sing--box-2f7d5a?style=flat-square" alt="core">
  <a href="LICENSE"><img src="https://img.shields.io/github/license/sala-coding/EagleProxy?style=flat-square" alt="license"></a>
</p>

<p align="center">
  <a href="README.md">简体中文</a> · <a href="README_EN.md">English</a> · <a href="README_RU.md">Русский</a> · <b>فارسی</b>
</p>

<p align="center">
  <a href="https://github.com/sala-coding/EagleProxy/releases/latest"><img src="https://img.shields.io/badge/DOWNLOAD-FULL%20ZIP-2f7d5a?style=for-the-badge" alt="download full"></a>
  <a href="https://github.com/sala-coding/EagleProxy/releases/latest"><img src="https://img.shields.io/badge/DOWNLOAD-EXE%20ONLY-0b6cbd?style=for-the-badge" alt="download exe"></a>
</p>

<p align="center">
  <sub><b>full</b> برای نصب نخست، <b>exe</b> برای به‌روزرسانی روی نسخهٔ قبلی · بدون رجیستری و سرویس</sub>
</p>

---

### امکانات

<table>
<tr>
<td align="center" width="33%"><b>اشتراک‌ها</b><br><sub>لینک‌ها<br>حجم و انقضا خودکار</sub></td>
<td align="center" width="33%"><b>جابه‌جایی سریع</b><br><sub>تغییر نود در گروه<br>بدون راه‌اندازی دوبارهٔ هسته</sub></td>
<td align="center" width="33%"><b>دو حالت</b><br><sub>پروکسی سیستم / TUN<br>با یک کلیک</sub></td>
</tr>
<tr><td colspan="3"><br><br></td></tr>
<tr>
<td align="center"><b>شبکهٔ محلی</b><br><sub>یک نشانی<br>برای همهٔ دستگاه‌ها</sub></td>
<td align="center"><b>قواعد و DNS</b><br><sub><code>.srs</code> بر پایهٔ کشور<br>دو فهرست، سریع‌ترین انتخاب می‌شود</sub></td>
<td align="center"><b>پایش</b><br><sub>اتصال‌های زنده<br>لاگ دسته‌بندی‌شده</sub></td>
</tr>
</table>

<p align="center">
  <sub>همچنین: کلیدهای میانبر و سینی سیستم · چهار زبان · بررسی به‌روزرسانی در GitHub</sub>
</p>

### شروع سریع

1. `Eagle-v*-windows-full.zip` را از صفحهٔ [Releases](https://github.com/sala-coding/EagleProxy/releases/latest) دانلود کنید
2. در هر پوشه‌ای از حالت فشرده خارج کنید، با `Eagle.exe` و `sing-box\` کنار هم
3. `Eagle.exe` را اجرا کنید، اشتراک یا لینک نود را وارد کنید و از جدول یک نود را روشن کنید

### هسته

هسته درون برنامه نیست: برنامه `sing-box.exe` را کنار خود، در پوشهٔ `sing-box\` و نزدیک پوشهٔ داده‌ها می‌جوید. در تنظیمات → مسیر هسته همهٔ هسته‌های یافته‌شده فهرست می‌شوند و انتخابشان یک کلیک است. داده‌ها در `config\` کنار برنامه می‌مانند.

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

<p align="center">
  <sub>هسته <a href="https://github.com/SagerNet/sing-box">sing-box</a> · چارچوب <a href="https://wails.io">Wails</a> · ساختار داده با الگوی <a href="https://github.com/MatsuriDayo/nekoray">nekoray</a> · <a href="LICENSE">GPL-3.0</a></sub>
</p>
