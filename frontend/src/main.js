import { createApp } from 'vue'
import { createPinia } from 'pinia'
import { createRouter, createWebHistory } from 'vue-router'

import './style.css'
import '@fontsource-variable/inter'

import App from './App.vue'
import { fetchUser } from './api'

const routes = [
  { path: '/', component: () => import('./views/WorkspaceView.vue') },
  { 
    path: '/tasks/:filter', 
    component: () => import('./views/TaskListView.vue'),
    children: [
      { path: ':workspaceId/:taskId', component: () => import('./views/TaskDetailView.vue') },
      { path: ':workspaceId/:taskId/instances', component: () => import('./views/ScheduledTaskInstancesView.vue') }
    ]
  },
  { 
    path: '/workspaces/:id', 
    component: () => import('./views/WorkspaceDetailView.vue'),
    children: [
      { path: 'analytics', component: () => import('./views/WorkspaceAnalyticsView.vue') },
      { path: 'settings', component: () => import('./views/WorkspaceSettingsView.vue') },
      { path: 'tasks/:taskId', component: () => import('./views/TaskDetailView.vue') },
      { path: 'tasks/:taskId/instances', component: () => import('./views/ScheduledTaskInstancesView.vue') }
    ]
  },
  { path: '/workspaces/:id/tasks/new', component: () => import('./views/TaskFormView.vue') },
  { path: '/workspaces/:id/tasks/:taskId/edit', component: () => import('./views/TaskFormView.vue') },

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
