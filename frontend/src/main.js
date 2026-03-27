import { createApp } from 'vue'
import { createPinia } from 'pinia'
import { createRouter, createWebHistory } from 'vue-router'

import './style.css'
import '@fontsource-variable/inter'

import App from './App.vue'
import { fetchUser } from './api'

const routes = [
  { path: '/', component: () => import('./views/HomeView.vue') },
  { path: '/tasks/:filter', component: () => import('./views/TaskListView.vue') },
  { path: '/workspaces/:id', component: () => import('./views/WorkspaceDetailView.vue') },
  { path: '/workspaces/:workspaceId/tasks/:taskId', component: () => import('./views/TaskDetailView.vue'), meta: { fullPage: true } },
  { path: '/login', component: () => import('./views/LoginView.vue'), meta: { public: true } }
]

const router = createRouter({
  history: createWebHistory(),
  routes,
})

router.beforeEach(async (to, from, next) => {
  if (to.meta.public) return next()
  
  try {
    const user = await fetchUser()
    if (!user) return next('/login')
    next()
  } catch (err) {
    next('/login')
  }
})

const pinia = createPinia()
const app = createApp(App)

// Global Click Outside Directive
app.directive('click-outside', {
  mounted(el, binding) {
    el._clickOutside = (event) => {
      if (!(el === event.target || el.contains(event.target))) {
        binding.value(event);
      }
    };
    document.body.addEventListener('click', el._clickOutside);
  },
  unmounted(el) {
    document.body.removeEventListener('click', el._clickOutside);
  },
});

app.use(pinia)
app.use(router)
app.mount('#app')
