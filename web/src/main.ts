import { VueQueryPlugin } from '@tanstack/vue-query'
import ElementPlus from 'element-plus'
import 'element-plus/dist/index.css'
import { createApp } from 'vue'
import App from '@/app/App.vue'
import { icons } from '@/app/icons'
import { queryClient } from '@/app/queryClient'
import { router } from '@/app/router'
import '@/styles/theme.css'

const app = createApp(App)
app.use(ElementPlus)
app.use(VueQueryPlugin, { queryClient })
app.use(router)
for (const [name, comp] of Object.entries(icons)) app.component(name, comp)
app.mount('#app')
