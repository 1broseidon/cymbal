import { spawn } from "node:child_process"

export const notifiedUpdateVersions = new Set()

export function updateNotifierDisabled() {
  const value = String(process.env.CYMBAL_NO_UPDATE_NOTIFIER ?? "").trim().toLowerCase()
  return value === "1" || value === "true" || value === "yes" || value === "on"
}

export function parseUpdateNotice(text) {
  if (typeof text !== "string") return null

  const normalized = text.replace(/\r\n?/g, "\n")
  const match = normalized.match(
    /(?:^|\n)cymbal update:\n  A newer version is available: ([^\n]+)\n  Run: ([^\n]+)\n(?:  If you can run shell commands here, run it now\.(?:\n|$))?/,
  )
  if (!match) return null

  const version = match[1].trim()
  const command = match[2].trim()
  if (!version || !command) return null

  return {
    version,
    command,
    title: `cymbal update available`,
    body: `A newer version is available: ${version}. Run: ${command}`,
  }
}

export function appleScriptString(value) {
  return String(value)
    .replaceAll("\\", "\\\\")
    .replaceAll('"', '\\"')
    .replaceAll("\r", " ")
    .replaceAll("\n", " ")
}

function powerShellSingleQuotedString(value) {
  return String(value).replaceAll("'", "''")
}

export function buildNotificationCommand(platform, notice, env) {
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

export async function showNativeNotification(notice) {
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

export async function notifyUpdateFromReminder(text) {
  if (updateNotifierDisabled()) return

  const notice = parseUpdateNotice(text)
  if (!notice) return
  if (notifiedUpdateVersions.has(notice.version)) return

  notifiedUpdateVersions.add(notice.version)
  await showNativeNotification(notice)
}

export default async ({ $ }) => ({
  "experimental.chat.system.transform": async (_input, output) => {
    try {
      const reminder = await $`cymbal hook remind --format=text --update=if-stale`.text()
      const text = reminder.trim()
      if (text) {
        output.system.push(text)
        await notifyUpdateFromReminder(text)
      }
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
