// @ts-ignore — VitePress supports async config at runtime

async function getLatestVersion(repo: string): Promise<string | null> {
  try {
    const res = await fetch(`https://api.github.com/repos/1broseidon/${repo}/releases/latest`)
    if (!res.ok) return null
    const data = await res.json() as { tag_name: string }
    return data.tag_name ?? null
  } catch {
    return null
  }
}

export default (async () => {
  const version = await getLatestVersion('cymbal')

  return {
    title: 'cymbal',
    description: 'Agent-native code navigation',
    base: '/cymbal/',
    appearance: false,
    cleanUrls: true,
    head: [
      ['link', { rel: 'preconnect', href: 'https://fonts.googleapis.com' }],
      ['link', { rel: 'preconnect', href: 'https://fonts.gstatic.com', crossorigin: '' }],
      ['link', { href: 'https://fonts.googleapis.com/css2?family=Work+Sans:wght@300;400;700&family=JetBrains+Mono:wght@400;500&display=swap', rel: 'stylesheet' }],
    ],
    themeConfig: {
      version,
      nav: [
        { text: 'Guide', link: '/guide/getting-started' },
        { text: 'Library', link: '/guide/library' },
        { text: 'Commands', link: '/reference/commands' },
        { text: 'Changelog', link: '/changelog' },
      ],
      sidebar: [
        {
          text: 'Guide',
          items: [
            { text: 'Getting Started', link: '/guide/getting-started' },
            { text: 'Library Guide', link: '/guide/library' },
            { text: 'Agent-Native Output', link: '/guide/agent-native' },
          ],
        },
        {
          text: 'Reference',
          items: [
            { text: 'Commands', link: '/reference/commands' },
          ],
        },
        {
          text: 'Changelog',
          link: '/changelog',
        },
      ],
      socialLinks: [
        { icon: 'github', link: 'https://github.com/1broseidon/cymbal' },
      ],
      outline: { level: [2, 3] },
    },
  }
})
