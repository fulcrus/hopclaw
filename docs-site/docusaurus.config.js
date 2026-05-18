/** @type {import('@docusaurus/types').Config} */
const config = {
  title: 'HopClaw',
  tagline: 'Governed agent runtime for real-world automation',
  favicon: 'img/logo.svg',
  url: 'https://docs.hopclaw.dev',
  baseUrl: '/',
  organizationName: 'fulcrus',
  projectName: 'hopclaw',
  onBrokenLinks: 'throw',
  onBrokenMarkdownLinks: 'warn',

  i18n: {
    defaultLocale: 'en',
    locales: ['en', 'zh-CN', 'ja-JP'],
  },

  presets: [
    [
      'classic',
      /** @type {import('@docusaurus/preset-classic').Options} */
      ({
        docs: {
          sidebarPath: './sidebars.js',
          routeBasePath: '/',
          editUrl: 'https://github.com/fulcrus/hopclaw/tree/main/docs-site/',
        },
        blog: false,
        theme: {
          customCss: './src/css/custom.css',
        },
      }),
    ],
  ],

  themeConfig:
    /** @type {import('@docusaurus/preset-classic').ThemeConfig} */
    ({
      image: 'img/logo.svg',
      navbar: {
        title: 'HopClaw',
        logo: {
          alt: 'HopClaw',
          src: 'img/logo.svg',
        },
        items: [
          { type: 'docSidebar', sidebarId: 'docs', position: 'left', label: 'Docs' },
          { type: 'localeDropdown', position: 'right' },
          { href: 'https://github.com/fulcrus/hopclaw', label: 'GitHub', position: 'right' },
        ],
      },
      footer: {
        style: 'dark',
        links: [
          {
            title: 'Start',
            items: [
              { label: 'Installation', to: '/getting-started/installation' },
              { label: 'Quick Start', to: '/getting-started/quick-start' },
              { label: 'Web Dashboard', to: '/guides/web-dashboard' },
            ],
          },
          {
            title: 'Build',
            items: [
              { label: 'Your First Agent', to: '/guides/your-first-agent' },
              { label: 'Plugin Development', to: '/guides/plugin-development' },
              { label: 'Channel Integration', to: '/guides/channel-integration' },
            ],
          },
        ],
        copyright: `Copyright © ${new Date().getFullYear()} HopClaw Contributors.`,
      },
    }),
};

module.exports = config;
