import test from "node:test"
import assert from "node:assert/strict"
import { createRequire, syncBuiltinESMExports } from "node:module"

import { CymbalPlugin } from "./cymbal-opencode.js"

// OpenCode loads every export of a plugin file as a plugin, so the plugin
// exports only CymbalPlugin and its helpers are tested through the hooks.

const childProcess = createRequire(import.meta.url)("node:child_process")

// Unset in every notification test, so the host environment cannot leak in.
const QUIET_ENV = { CYMBAL_NO_UPDATE_NOTIFIER: undefined, DISPLAY: undefined, WAYLAND_DISPLAY: undefined }

// fakeShell stands in for OpenCode's Bun $: each `cymbal hook <name>` call
// prints outputs[name], and every call is recorded.
function fakeShell(outputs = {}) {
  const calls = []
  const $ = (strings, ...values) => {
    const command = strings.join("").trim()
    const name = command.split(/\s+/)[2]
    calls.push({ name, command, values })
    const result = {
      quiet: () => result,
      nothrow: () => result,
      text: async () => outputs[name] ?? "",
    }
    return result
  }
  return { $, calls }
}

// withEnvironment runs fn with process.platform and environment variables
// set, and spawn recorded instead of run, then restores all three.
async function withEnvironment({ platform = process.platform, env = {} }, fn) {
  const spawned = []
  const originalSpawn = childProcess.spawn
  const originalPlatform = Object.getOwnPropertyDescriptor(process, "platform")
  const originalEnv = {}
  const setEnv = (key, value) => {
    if (value === undefined) delete process.env[key]
    else process.env[key] = value
  }

  childProcess.spawn = (command, args) => {
    spawned.push({ command, args })
    return { once() {}, unref() {} }
  }
  syncBuiltinESMExports()
  Object.defineProperty(process, "platform", { ...originalPlatform, value: platform })
  for (const [key, value] of Object.entries(env)) {
    originalEnv[key] = process.env[key]
    setEnv(key, value)
  }
  try {
    await fn(spawned)
  } finally {
    childProcess.spawn = originalSpawn
    syncBuiltinESMExports()
    Object.defineProperty(process, "platform", originalPlatform)
    for (const [key, value] of Object.entries(originalEnv)) setEnv(key, value)
  }
}

// transformWithUpdate runs the chat transform hook while cymbal reports an
// update. Each call uses a new version, since the plugin announces a version once.
let updateVersion = 0
async function transformWithUpdate(notice) {
  updateVersion += 1
  const shell = fakeShell({
    notify: JSON.stringify({ notify: true, latestVersion: `9.9.${updateVersion}`, ...notice }),
  })
  const hooks = await CymbalPlugin({ $: shell.$ })
  await hooks["experimental.chat.system.transform"]({}, { system: [] })
  return shell.calls
}

test("the chat transform adds cymbal's reminder to the system prompt", async () => {
  await withEnvironment({ env: { CYMBAL_NO_UPDATE_NOTIFIER: "1" } }, async () => {
    for (const [reminder, want] of [["  use cymbal search\n", ["use cymbal search"]], [" \n", []]]) {
      const hooks = await CymbalPlugin({ $: fakeShell({ remind: reminder }).$ })
      const output = { system: [] }
      await hooks["experimental.chat.system.transform"]({}, output)
      assert.deepStrictEqual(output.system, want)
    }
  })
})

test("an update on macOS runs osascript with the text escaped for AppleScript", async () => {
  await withEnvironment({ platform: "darwin", env: QUIET_ENV }, async (spawned) => {
    await transformWithUpdate({ title: 'Say "hi"', body: 'Run "cymbal" \\ now\nplease\r\n' })
    assert.deepStrictEqual(spawned, [
      {
        command: "osascript",
        args: ["-e", 'display notification "Run \\"cymbal\\" \\\\ now please  " with title "Say \\"hi\\""'],
      },
    ])
  })
})

test("an update on Linux uses notify-send only when there is a display", async () => {
  const notifySend = {
    command: "notify-send",
    args: ["--app-name=cymbal", "--urgency=normal", "--expire-time=10000", "--", "Update", "Body"],
  }
  for (const [display, want] of [
    [{}, []],
    [{ DISPLAY: ":0" }, [notifySend]],
    [{ WAYLAND_DISPLAY: "wayland-0" }, [notifySend]],
  ]) {
    await withEnvironment({ platform: "linux", env: { ...QUIET_ENV, ...display } }, async (spawned) => {
      await transformWithUpdate({ title: "Update", body: "Body" })
      assert.deepStrictEqual(spawned, want, JSON.stringify(display))
    })
  }
})

test("an update on Windows runs PowerShell with single quotes doubled", async () => {
  await withEnvironment({ platform: "win32", env: QUIET_ENV }, async (spawned) => {
    await transformWithUpdate({ title: "O'Reilly", body: "Line 1\nLine 2" })
    assert.deepStrictEqual(spawned, [
      {
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
            "$notify.BalloonTipTitle = 'O''Reilly'",
            "$notify.BalloonTipText = 'Line 1\nLine 2'",
            "$notify.ShowBalloonTip(10000)",
            "Start-Sleep -Milliseconds 11000",
            "$notify.Dispose()",
          ].join("; "),
        ],
      },
    ])
  })
})

test("no notification on other platforms, or for a notice cymbal did not complete", async () => {
  await withEnvironment({ platform: "freebsd", env: QUIET_ENV }, async (spawned) => {
    await transformWithUpdate({ title: "Update", body: "Body" })
    assert.deepStrictEqual(spawned, [])
  })
  await withEnvironment({ platform: "darwin", env: QUIET_ENV }, async (spawned) => {
    for (const notice of [
      { notify: false, title: "Update", body: "Body" },
      {},
      { title: "Only title" },
      { body: "Only body" },
      { title: 1, body: "Body" },
    ]) {
      await transformWithUpdate(notice)
    }
    assert.deepStrictEqual(spawned, [])
  })
})

test("each version is announced once", async () => {
  await withEnvironment({ platform: "darwin", env: QUIET_ENV }, async (spawned) => {
    const shell = fakeShell({
      notify: JSON.stringify({ notify: true, latestVersion: "8.0.0", title: "Update", body: "Body" }),
    })
    const hooks = await CymbalPlugin({ $: shell.$ })
    await hooks["experimental.chat.system.transform"]({}, { system: [] })
    await hooks["experimental.chat.system.transform"]({}, { system: [] })
    assert.equal(spawned.length, 1)
  })
})

test("CYMBAL_NO_UPDATE_NOTIFIER turns off the update check", async () => {
  for (const [value, checks] of [
    ["1", false],
    ["true", false],
    ["yes", false],
    [" ON ", false],
    [undefined, true],
    ["0", true],
    ["false", true],
    ["no", true],
    ["off", true],
  ]) {
    await withEnvironment(
      { platform: "darwin", env: { ...QUIET_ENV, CYMBAL_NO_UPDATE_NOTIFIER: value } },
      async (spawned) => {
        const calls = await transformWithUpdate({ title: "Update", body: "Body" })
        assert.equal(calls.some((call) => call.name === "notify"), checks, `CYMBAL_NO_UPDATE_NOTIFIER=${value}`)
        assert.equal(spawned.length, checks ? 1 : 0)
      },
    )
  }
})

test("tool.execute.before shows cymbal's nudge before bash and on Grep", async () => {
  await withEnvironment({ platform: "linux" }, async () => {
    const shell = fakeShell({ nudge: JSON.stringify({ suggest: "cymbal search Foo", why: "it's indexed" }) })
    const hooks = await CymbalPlugin({ $: shell.$ })
    const notice = `cymbal nudge: cymbal search Foo — it'"'"'s indexed`

    const bash = { args: { command: "grep -rn Foo ." } }
    await hooks["tool.execute.before"]({ tool: "bash" }, bash)
    assert.equal(bash.args.command, `printf '%s\n' '${notice}' >&2; grep -rn Foo .`)
    assert.deepStrictEqual(JSON.parse(await shell.calls[0].values[0].text()), {
      tool_name: "bash",
      tool_input: { command: "grep -rn Foo ." },
    })

    const grep = { args: { pattern: "Foo" } }
    await hooks["tool.execute.before"]({ tool: "Grep" }, grep)
    assert.equal(grep.notice, notice)
    assert.deepStrictEqual(grep.args, { pattern: "Foo" })
  })
})

test("tool.execute.before leaves other tools, Windows and unusable nudges alone", async () => {
  const nudge = JSON.stringify({ suggest: "cymbal search Foo", why: "indexed" })
  await withEnvironment({ platform: "linux" }, async () => {
    const shell = fakeShell({ nudge })
    const hooks = await CymbalPlugin({ $: shell.$ })
    await hooks["tool.execute.before"]({ tool: "Read" }, { args: { filePath: "a.go" } })
    assert.deepStrictEqual(shell.calls, [])

    for (const output of ["", "not json", JSON.stringify({ suggest: "cymbal search Foo" })]) {
      const bash = { args: { command: "ls" } }
      await (await CymbalPlugin({ $: fakeShell({ nudge: output }).$ }))["tool.execute.before"]({ tool: "bash" }, bash)
      assert.equal(bash.args.command, "ls", JSON.stringify(output))
    }
  })
  await withEnvironment({ platform: "win32" }, async () => {
    const shell = fakeShell({ nudge })
    const bash = { args: { command: "ls" } }
    await (await CymbalPlugin({ $: shell.$ }))["tool.execute.before"]({ tool: "bash" }, bash)
    assert.equal(bash.args.command, "ls")
    assert.deepStrictEqual(shell.calls, [])
  })
})
