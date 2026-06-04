import { defineConfig } from 'vitepress';
import { withMermaid } from 'vitepress-plugin-mermaid';

export default withMermaid(
  defineConfig({
    title: '@dotformat/cli',
    description: 'Collect, backup, restore, and diff machine configs across machines',
    lang: 'en-US',
    base: '/dotfiles/',
    appearance: 'dark',
    cleanUrls: true,
    lastUpdated: true,
    themeConfig: {
      search: {
        provider: 'local',
      },
      nav: [
        { text: 'Guide', link: '/getting-started' },
        { text: 'Commands', link: '/commands' },
        { text: 'Architecture', link: '/architecture' },
      ],
      sidebar: [
        {
          text: 'Getting Started',
          items: [
            { text: 'Introduction', link: '/' },
            { text: 'Getting Started', link: '/getting-started' },
            { text: '.dotf File Format', link: '/dotf-format' },
          ],
        },
        {
          text: 'Usage',
          items: [
            { text: 'Commands', link: '/commands' },
            { text: 'Backup and Restore', link: '/backup-restore' },
            { text: 'Sensitivity and Redaction', link: '/sensitivity' },
            { text: 'Encryption & Secret Gate', link: '/encryption' },
            { text: 'chezmoi (Hybrid Model)', link: '/chezmoi' },
            { text: 'Migration Runbook', link: '/migration' },
          ],
        },
        {
          text: 'Internals',
          items: [
            { text: 'Architecture', link: '/architecture' },
            { text: 'Config Registry', link: '/registry' },
            { text: 'Collectors', link: '/collectors' },
            { text: 'Platform Support', link: '/platform-support' },
            { text: 'Execution Flows', link: '/flows' },
            { text: 'Behavior Reference', link: '/behavior-reference' },
          ],
        },
        {
          text: 'Project',
          items: [
            { text: 'Roadmap', link: '/roadmap' },
          ],
        },
      ],
      socialLinks: [{ icon: 'github', link: 'https://github.com/doguyilmaz/dotfiles' }],
    },
  }),
);
