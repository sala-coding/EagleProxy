<p align="center">
  <img src="build/banner.svg" width="100%" alt="EagleProxy">
</p>

<p align="center">
  <a href="https://github.com/sala-coding/EagleProxy/releases/latest"><img src="https://img.shields.io/github/v/release/sala-coding/EagleProxy?style=flat-square" alt="release"></a>
  <img src="https://img.shields.io/badge/platform-Windows%2010%20%7C%2011-0b6cbd?style=flat-square" alt="platform">
  <img src="https://img.shields.io/badge/core-sing--box-2f7d5a?style=flat-square" alt="core">
  <img src="https://img.shields.io/badge/ui-4%20languages-6e6a86?style=flat-square" alt="languages">
  <a href="LICENSE"><img src="https://img.shields.io/github/license/sala-coding/EagleProxy?style=flat-square" alt="license"></a>
</p>

<p align="center">
  <a href="README.md">简体中文</a> · <a href="README_EN.md">English</a> · <b>Русский</b> · <a href="README_FA.md">فارسی</a>
</p>

<p align="center">
  <img src=".github/assets/hero.png" width="100%" alt="Главное окно EagleProxy">
</p>
<p align="center">
  <sub>Настольный клиент sing-box для Windows<br>Подписки, узлы, два режима, правила и DNS — на одном экране, всё предсказуемо</sub>
</p>

<p align="center">
  <a href="https://github.com/sala-coding/EagleProxy/releases/latest"><img src="https://img.shields.io/badge/СКАЧАТЬ-FULL%20ZIP-2f7d5a?style=for-the-badge" alt="download full"></a>
  <a href="https://github.com/sala-coding/EagleProxy/releases/latest"><img src="https://img.shields.io/badge/СКАЧАТЬ-EXE%20ONLY-0b6cbd?style=for-the-badge" alt="download exe"></a>
</p>
<p align="center">
  <sub>Распакуйте и запускайте · без реестра · без служб · данные остаются в своей папке</sub>
</p>

---

## ✨ Возможности

| Возможность | Описание |
|---|---|
| **📥 Подписки** | Импорт по ссылке или чтение локальных конфигов sing-box / Clash; лимит и срок разбираются автоматически, с экспортом обратно в sing-box / Clash / ссылки |
| **⚡ Горячее переключение** | Смена узла в группе без перезапуска ядра; TUN прогрет заранее — первый переход без ожидания |
| **🧭 Два режима** | Системный прокси / TUN, одним щелчком; список исключений системного прокси редактируется |
| **📡 Локальная сеть** | Один адрес для всех устройств, счётчик на панели статуса |
| **🧱 Правила и DNS** | Наборы правил `.srs` по странам, два списка, быстрейший выбирается автоматически |
| **🔎 Наблюдение** | Соединения в реальном времени с разрывом в один клик, счётчики трафика, журнал по категориям |

<p align="center">
  <sub>Ещё: горячие клавиши и трей · 简体中文 / English / Русский / فارси · проверка обновлений на карточке «О программе»</sub>
</p>

## 📸 Скриншоты

<p align="center">
  <img src=".github/assets/gallery.png" width="100%" alt="подписки, соединения и настройки">
</p>
<p align="center">
  <sub>Панель подписок (полосы лимита) · соединения в реальном времени · панель настроек</sub>
</p>

## 🚀 Быстрый старт

| Файл | Для кого | Внутри |
|---|---|---|
| `Eagle-v*-windows-full.zip` | первая установка | `Eagle.exe` + папка ядра `sing-box\`, распаковать и запускать |
| `Eagle-v*-windows.exe` | обновление поверх | только программа, положить поверх старой |

1. Скачайте **full zip** со страницы [Releases](https://github.com/sala-coding/EagleProxy/releases/latest) и распакуйте в любую папку (сохранив `Eagle.exe` и `sing-box\` рядом)
2. Запустите `Eagle.exe`, добавьте подписку или вставьте ссылки узлов
3. Выберите узел в таблице и нажмите запуск

> **Требования**: Windows 10 / 11 x64. При отсутствии WebView2 программа предложит его установить; режим TUN при первом включении просит права администратора.

## 🧩 Ядро

Ядро не встроено: программа ищет `sing-box.exe` рядом с собой, в папке `sing-box\` и рядом с каталогом данных. Настройки → путь к ядру показывают всё найденное — сборку можно выбрать, так что несколько версий живут рядом.

## ⚖️ Дисклеймер

Это проект только клиентской части: никаких узлов или подписочных услуг он не предоставляет. Пользуйтесь в согласии с законами вашего региона.

---

<p align="center">
  <sub>Ядро <a href="https://github.com/SagerNet/sing-box">sing-box</a> · каркас <a href="https://wails.io">Wails</a> · структуры данных по образцу <a href="https://github.com/MatsuriDayo/nekoray">nekoray</a> · <a href="LICENSE">GPL-3.0</a></sub>
</p>
