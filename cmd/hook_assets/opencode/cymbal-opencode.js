import { spawn } from "node:child_process"

const notifiedUpdateVersions = new Set()

function updateNotifierDisabled() {
  const value = String(process.env.CYMBAL_NO_UPDATE_NOTIFIER ?? "").trim().toLowerCase()
  return value === "1" || value === "true" || value === "yes" || value === "on"
}

function appleScriptString(value) {
  return String(value)
    .replaceAll("\\", "\\\\")
    .replaceAll('"', '\\"')
    .replaceAll("\r", " ")
    .replaceAll("\n", " ")
}

function powerShellSingleQuotedString(value) {
  return String(value).replaceAll("'", "''")
}

function buildNotificationCommand(platform, notice, env) {
  if (!notice || typeof notice.title !== "string" || typeof notice.body !== "string") return null

  if (platform === "darwin") {
    return {
      command: "osascript",
      args: [
        "-e",
        `display notification "${appleScriptString(notice.body)}" with title "${appleScriptString(notice.title)}"`,
      ],
    }
  }

  if (platform === "linux") {
    const hasDisplay = Boolean(env && (env.DISPLAY !== undefined || env.WAYLAND_DISPLAY !== undefined))
    if (!hasDisplay) return null

    return {
      command: "notify-send",
      args: [
        "--app-name=cymbal",
        "--urgency=normal",
        "--expire-time=10000",
        "--",
        notice.title,
        notice.body,
      ],
    }
  }

  if (platform === "win32") {
    const title = powerShellSingleQuotedString(notice.title)
    const body = powerShellSingleQuotedString(notice.body)
    return {
      command: "powershell.exe",
      args: [
        "-NoProfile",
        "-WindowStyle",
        "Hidden",
        "-Command",
        [
          "Add-Type -AssemblyName System.Windows.Forms",
          "Add-Type -AssemblyName System.Drawing",
          "$notify = New-Object System.Windows.Forms.NotifyIcon",
          "$notify.Icon = [System.Drawing.SystemIcons]::Information",
          "$notify.Visible = $true",
          `$notify.BalloonTipTitle = '${title}'`,
          `$notify.BalloonTipText = '${body}'`,
          "$notify.ShowBalloonTip(10000)",
          "Start-Sleep -Milliseconds 11000",
          "$notify.Dispose()",
        ].join("; "),
      ],
    }
  }

  return null
}

async function showNativeNotification(notice) {
  const spec = buildNotificationCommand(process.platform, notice, process.env)
  if (!spec) return

  try {
    const child = spawn(spec.command, spec.args, {
      detached: true,
      stdio: "ignore",
      windowsHide: true,
    })
    child.once("error", () => {})
    child.unref()
  } catch (error) {
    void error
  }
}

async function notifyUpdateFromCymbal($) {
  if (updateNotifierDisabled()) return

  try {
    const raw = await $`cymbal hook notify --format=json --update=cache`.quiet().nothrow().text()
    const payload = JSON.parse(raw.trim() || "{}")
    if (!payload.notify || !payload.latestVersion) return
    if (notifiedUpdateVersions.has(payload.latestVersion)) return

    notifiedUpdateVersions.add(payload.latestVersion)
    await showNativeNotification({
      title: payload.title,
      body: payload.body,
    })
  } catch (error) {
    void error
  }
}

export const CymbalPlugin = async ({ $ }) => ({
  "experimental.chat.system.transform": async (_input, output) => {
    try {
      const reminder = await $`cymbal hook remind --format=text --update=if-stale`.text()
      const text = reminder.trim()
      if (text) output.system.push(text)
      await notifyUpdateFromCymbal($)
    } catch (error) {
      void error
    }
  },
  "tool.execute.before": async (input, output) => {
    const nudgableTools = ["bash", "Grep", "Glob"]
    if (!nudgableTools.includes(input.tool)) return
    if (process.platform === "win32") return

    try {
      const toolInput = input.tool === "bash"
        ? { command: output.args?.command }
        : { ...output.args }
      if (input.tool === "bash" && typeof toolInput.command !== "string") return

      const payload = new Response(
        JSON.stringify({
          tool_name: input.tool === "bash" ? "bash" : input.tool,
          tool_input: toolInput,
        }),
      )
      const raw = await $`cymbal hook nudge --format=json < ${payload}`.quiet().nothrow().text()
      const text = raw.trim()
      if (!text) return

      const result = JSON.parse(text)
      if (typeof result.suggest !== "string" || typeof result.why !== "string") return

      const notice = `cymbal nudge: ${result.suggest} — ${result.why}`.replaceAll("'", `'"'"'`)
      if (input.tool === "bash") {
        output.args.command = `printf '%s\n' '${notice}' >&2; ${output.args.command}`
      } else {
        output.notice = notice
      }
    } catch (error) {
      void error
    }
  },
})
