import { createRouter, createWebHistory } from 'vue-router'
import Home from '../views/Home.vue'
import HowItWorks from '../views/HowItWorks.vue'
import Features from '../views/Features.vue'
import UseCases from '../views/UseCases.vue'
import Docs from '../views/Docs.vue'
import ClawHub from '../views/ClawHub.vue'
import Telemetry from '../views/Telemetry.vue'

const routes = [
  { path: '/', name: 'Home', component: Home },
  { path: '/how-it-works', name: 'HowItWorks', component: HowItWorks },
  { path: '/features', name: 'Features', component: Features },
  { path: '/use-cases', name: 'UseCases', component: UseCases },
  { path: '/docs', name: 'Docs', component: Docs },
  { path: '/telemetry', name: 'Telemetry', component: Telemetry },
  { path: '/clawhub', name: 'ClawHub', component: ClawHub },
]

const router = createRouter({
  history: createWebHistory(),
  routes,
  scrollBehavior(to) {
    if (to.hash) {
      return {
        el: to.hash,
        top: 88,
        behavior: 'smooth',
      }
    }
    return { top: 0 }
  },
})

export default router
