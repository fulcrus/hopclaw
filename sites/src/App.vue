<script setup>
import { watch } from 'vue'
import { useRoute } from 'vue-router'
import Footer from './components/Footer.vue'
import NavBar from './components/NavBar.vue'
import { useI18n } from './i18n'

const route = useRoute()
const { t, locale } = useI18n()

const SITE_URL = 'https://hopclaw.com'
const THEME_COLOR = '#f5ede2'

function normalizePath(path) {
  if (!path || path === '/') return '/'
  return path.replace(/\/+$/, '')
}

function ogImageUrl(code) {
  const image = code === 'zh' ? 'og-card-zh.svg' : 'og-card.svg'
  return `${SITE_URL}/${image}`
}

function ogImageAlt(code) {
  if (code === 'zh') {
    return 'HopClaw：给个人和团队的 Go Agent Runtime，可执行、可治理、可接入'
  }
  return 'HopClaw: Go agent runtime for personal users and teams with real execution and governed rollout'
}

function ogLocale(code) {
  if (code === 'zh') return 'zh_CN'
  if (code === 'ja') return 'ja_JP'
  if (code === 'es') return 'es_ES'
  return 'en_US'
}

function pageMeta(path) {
  const key = path === '/' ? 'home' : path.slice(1)

  switch (key) {
    case 'how-it-works':
      return {
        title: `${t('nav.runtime')} | HopClaw`,
        description: t('runtime.desc'),
      }
    case 'features':
      return {
        title: `${t('nav.features')} | HopClaw`,
        description: t('features.desc'),
      }
    case 'use-cases':
      return {
        title: `${t('nav.useCases')} | HopClaw`,
        description: t('useCases.desc'),
      }
    case 'docs':
      return {
        title: `${t('nav.docs')} | HopClaw`,
        description: t('docs.desc'),
      }
    case 'telemetry':
      return {
        title: `${t('nav.telemetry')} | HopClaw`,
        description: t('telemetry.desc'),
      }
    case 'clawhub':
      return {
        title: `${t('nav.skills')} | HopClaw`,
        description: t('clawHub.desc'),
      }
    default:
      return {
        title: `HopClaw | ${t('nav.tagline')}`,
        description: t('home.desc'),
      }
  }
}

function upsertMeta(selector, attrs) {
  let node = document.head.querySelector(selector)
  if (!node) {
    node = document.createElement('meta')
    document.head.appendChild(node)
  }
  Object.entries(attrs).forEach(([name, value]) => {
    node.setAttribute(name, value)
  })
}

function upsertLink(selector, attrs) {
  let node = document.head.querySelector(selector)
  if (!node) {
    node = document.createElement('link')
    document.head.appendChild(node)
  }
  Object.entries(attrs).forEach(([name, value]) => {
    node.setAttribute(name, value)
  })
}

watch(
  () => [route.path, locale.value],
  () => {
    const normalizedPath = normalizePath(route.path)
    const meta = pageMeta(normalizedPath)
    const url = new URL(normalizedPath === '/' ? '/' : normalizedPath, SITE_URL).toString()
    const imageUrl = ogImageUrl(locale.value)
    const imageAlt = ogImageAlt(locale.value)

    document.title = meta.title

    upsertMeta('meta[name="description"]', {
      name: 'description',
      content: meta.description,
    })
    upsertMeta('meta[name="theme-color"]', {
      name: 'theme-color',
      content: THEME_COLOR,
    })
    upsertMeta('meta[property="og:type"]', {
      property: 'og:type',
      content: 'website',
    })
    upsertMeta('meta[property="og:site_name"]', {
      property: 'og:site_name',
      content: 'HopClaw',
    })
    upsertMeta('meta[property="og:locale"]', {
      property: 'og:locale',
      content: ogLocale(locale.value),
    })
    upsertMeta('meta[property="og:title"]', {
      property: 'og:title',
      content: meta.title,
    })
    upsertMeta('meta[property="og:description"]', {
      property: 'og:description',
      content: meta.description,
    })
    upsertMeta('meta[property="og:url"]', {
      property: 'og:url',
      content: url,
    })
    upsertMeta('meta[property="og:image"]', {
      property: 'og:image',
      content: imageUrl,
    })
    upsertMeta('meta[property="og:image:alt"]', {
      property: 'og:image:alt',
      content: imageAlt,
    })
    upsertMeta('meta[name="twitter:card"]', {
      name: 'twitter:card',
      content: 'summary_large_image',
    })
    upsertMeta('meta[name="twitter:title"]', {
      name: 'twitter:title',
      content: meta.title,
    })
    upsertMeta('meta[name="twitter:description"]', {
      name: 'twitter:description',
      content: meta.description,
    })
    upsertMeta('meta[name="twitter:image"]', {
      name: 'twitter:image',
      content: imageUrl,
    })
    upsertMeta('meta[name="twitter:image:alt"]', {
      name: 'twitter:image:alt',
      content: imageAlt,
    })
    upsertLink('link[rel="canonical"]', {
      rel: 'canonical',
      href: url,
    })
  },
  { immediate: true },
)
</script>

<template>
  <div class="site-shell">
    <NavBar />
    <main class="app-main">
      <router-view />
    </main>
    <Footer />
  </div>
</template>
