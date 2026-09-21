/* Everything inkcap needs to print this site. The rest of the build is
 * github.com/1broseidon/inkcap, shared with the other chain.sh manuals. */
export default {
  name: 'cymbal',
  url: 'https://cymbal.sh',
  repo: '1broseidon/cymbal',
  tagline: 'code navigation for coding agents',
  built: 'Built with tree-sitter and SQLite',
  accent: {
    light: { accent: '#34558C', soft: '#E3E9F3' },
    dark: { accent: '#8AAEE0', soft: '#1A2436' },
    terminal: { prompt: '#7E9FD4', key: '#A8C2EA' },
  },
  /* The VitePress site that used to live at chain.sh/cymbal/ is gone, but
   * its URLs are in the wild and chain.sh forwards /cymbal/* here. GitHub
   * Pages has no server-side redirects, so the 404 page carries the map:
   * everything that folded into the manual lands on its anchor; what stayed
   * in the repo points at the file on GitHub. Unmapped paths 404 honestly. */
  moved: {
    '/reference/commands': 'https://github.com/1broseidon/cymbal/blob/main/docs/reference/commands.md',
    '/guide/library': 'https://github.com/1broseidon/cymbal/blob/main/docs/guide/library.md',
    '/guide/agent-native': '/#for-agents',
    '/guide/getting-started': '/#quickstart',
    '/AGENT_HOOKS': 'https://github.com/1broseidon/cymbal/blob/main/HOOKS.md',
    '/changelog': 'https://github.com/1broseidon/cymbal/blob/main/CHANGELOG.md',
    '/guide': '/#quickstart',
    '/reference': '/#commands',
  },
}
