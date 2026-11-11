<p align="center">
  <img src="build/appicon.png" width="108" alt="EagleProxy">
</p>

<h1 align="center">EagleProxy</h1>

<p align="center">Клиент прокси для Windows · подписки · таблица узлов · TUN / системный прокси</p>

<p align="center">
  <a href="https://github.com/sala-coding/EagleProxy/releases/latest"><img src="https://img.shields.io/github/v/release/sala-coding/EagleProxy?style=flat-square" alt="release"></a>
  <img src="https://img.shields.io/badge/platform-Windows%2010%20%7C%2011-0b6cbd?style=flat-square" alt="platform">
  <img src="https://img.shields.io/badge/core-sing--box-2f7d5a?style=flat-square" alt="core">
  <a href="LICENSE"><img src="https://img.shields.io/github/license/sala-coding/EagleProxy?style=flat-square" alt="license"></a>
</p>

<p align="center"><a href="README.md">简体中文</a> · <a href="README_EN.md">English</a> · <b>Русский</b> · <a href="README_FA.md">فارسی</a></p>

Написано на [Wails](https://wails.io) (Go + Vue 3), ядро — [sing-box](https://github.com/SagerNet/sing-box). Один исполняемый файл, портативная сборка: данные лежат в `config\` рядом с программой, без записей в реестре и без службы.

## Загрузка

| Файл | Содержимое | Назначение |
| --- | --- | --- |
| `Eagle-vX.Y.Z-windows-full.zip` | `Eagle.exe` + `sing-box\` | Первая установка: распакуйте и запускайте |
| `Eagle-vX.Y.Z-windows.exe` | только сама программа | Обновление поверх; его же скачивает встроенный автообновлятор |

Ядро не встроено: программа ищет `sing-box.exe` рядом с собой, в папке `sing-box\` и рядом с каталогом данных. Настройки → путь к ядру показывают найденное — сборку можно выбрать, а не вписывать вручную.

## Возможности

| Что | Подробнее |
| --- | --- |
| Подписки | Ссылки подписок и отдельные ссылки узлов, лимит трафика и срок действия разбираются автоматически, обновление по расписанию |
| Узлы | Задержка и скорость измеряются на месте, таблица сортируется, группы различаются цветом; переключение внутри группы идёт без перезапуска ядра |
| Режимы | Системный прокси / TUN переключаются на боковой панели, последний узел и режим запоминаются; стек TUN подстраивается под версию ядра |
| Сеть | Общий доступ в локальной сети, петлевое соединение UWP, два списка DNS-резолверов с выбором быстрейшего, наборы правил `.srs` по странам |
| Наблюдение | Список соединений в реальном времени (одно можно разорвать), журнал с раскраской по категориям, паузой, фильтром и экспортом |
| Рабочий стол | Глобальные горячие клавиши и меню в трее, четыре языка интерфейса, обновления с GitHub |

## Быстрый старт

1. Скачайте `Eagle-v*-windows-full.zip` со страницы [Releases](https://github.com/sala-coding/EagleProxy/releases/latest)
2. Распакуйте в любую папку, сохранив `Eagle.exe` и `sing-box\` рядом
3. Запустите `Eagle.exe`, добавьте подписку или вставьте ссылки узлов и запустите узел из таблицы

<details>
<summary><b>Сборка из исходников</b></summary>

Нужны Windows 10 / 11 (при отсутствии WebView2 программа предложит его установить), [Go](https://go.dev) 1.25+, [Node.js](https://nodejs.org) 18+ и [Wails CLI](https://wails.io/docs/gettingstarted/installation) v2. Ядро нужно своё: положите `sing-box.exe` и `wintun.dll` в `sing-box/` или укажите путь в настройках.

```cmd
wails dev
wails build                               :: сборка без номера версии
.\build\release.ps1 -Version v1.0.17      :: официальный выпуск с номером версии
```

Результат: `build/bin/Eagle.exe`.

</details>

## Благодарности и лицензия

Ядро [sing-box](https://github.com/SagerNet/sing-box) · каркас [Wails](https://wails.io) · структуры данных и словарь интерфейса — по образцу [nekoray](https://github.com/MatsuriDayo/nekoray). Распространяется по лицензии [GPL-3.0](LICENSE).
