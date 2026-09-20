import { createApp } from 'vue'
import App from './App.vue'
import '@fontsource/barlow/400.css'
import '@fontsource/barlow/500.css'
import '@fontsource/barlow/600.css'
import '@fontsource/barlow/700.css'
import '@fontsource-variable/jetbrains-mono'
import './style.css'
import './composables/useTheme'

createApp(App).mount('#app')
