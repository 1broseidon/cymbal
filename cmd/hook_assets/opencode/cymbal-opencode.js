export default async ({ $ }) => ({
  "experimental.chat.system.transform": async (_input, output) => {
    try {
      const reminder = await $`cymbal hook remind --format=text --update=if-stale`.text()
      const text = reminder.trim()
      if (text) output.system.push(text)
    } catch (error) {
      void error
    }
  },
  "tool.execute.before": async (input, output) => {
    if (input.tool !== "bash") return
    if (!output.args || typeof output.args.command !== "string") return

    if (process.platform === "win32") return

    try {
      const payload = new Response(
        JSON.stringify({
          tool_name: "bash",
          tool_input: { command: output.args.command },
        }),
      )
      const raw = await $`cymbal hook nudge --format=json < ${payload}`.quiet().nothrow().text()
      const text = raw.trim()
      if (!text) return

      const result = JSON.parse(text)
      if (typeof result.suggest !== "string" || typeof result.why !== "string") return

      const notice = `cymbal nudge: ${result.suggest} — ${result.why}`.replaceAll("'", `'"'"'`)
      output.args.command = `printf '%s\n' '${notice}' >&2; ${output.args.command}`
    } catch (error) {
      void error
    }
  },
})
