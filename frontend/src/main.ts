import {createApp, h} from 'vue'
// Real country flags (ISO 3166-1, SVG) for node names — see NodeName in App.vue.
import 'flag-icons/css/flag-icons.min.css'
import './style.css'
import App from './App.vue'
import CrashBoundary from './CrashBoundary.vue'
import {getLang} from './i18n';
import {loadTips} from './tips';

// The transient words live in public/fa/tips and arrive over fetch. Chinese is
// awaited here — a few milliseconds of a blank window — so the very first word
// the app speaks is a word rather than a key. Any other language is loaded with
// the settings that name it, which is later and does not hold the paint.
await loadTips(getLang())

// CrashBoundary keeps a render error from blanking the whole window: the app
// reports what went wrong instead of silently disappearing.
createApp({
  render: () => h(CrashBoundary, null, {default: () => h(App)}),
}).mount(document.getElementById('app')!)
