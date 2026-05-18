/** @type {import('@docusaurus/plugin-content-docs').SidebarsConfig} */
const sidebars = {
  docs: [
    'intro',
    {
      type: 'category',
      label: 'Getting Started',
      items: [
        'getting-started/installation',
        'getting-started/quick-start',
        'getting-started/configuration',
      ],
    },
    {
      type: 'category',
      label: 'Guides',
      items: [
        'guides/your-first-agent',
        'guides/plugin-development',
        'guides/channel-integration',
        'guides/web-dashboard',
      ],
    },
    'i18n',
    {
      type: 'category',
      label: 'Architecture',
      link: { type: 'generated-index' },
      items: [],
    },
    {
      type: 'category',
      label: 'API Reference',
      link: { type: 'generated-index' },
      items: [],
    },
  ],
};

module.exports = sidebars;
