import test from "node:test"
import assert from "node:assert/strict"

import {
  appleScriptString,
  buildNotificationCommand,
  parseUpdateNotice,
  updateNotifierDisabled,
} from "./cymbal-opencode.js"

test("parseUpdateNotice extracts version and command from valid block", () => {
  const text = [
    "hello",
    "cymbal update:",
    "  A newer version is available: 1.2.3",
    "  Run: cymbal update",
    "  If you can run shell commands here, run it now.",
    "",
  ].join("\n")

  assert.deepStrictEqual(parseUpdateNotice(text), {
    version: "1.2.3",
    command: "cymbal update",
    title: "cymbal update available",
    body: "A newer version is available: 1.2.3. Run: cymbal update",
  })
})

test("parseUpdateNotice returns null for text without update block", () => {
  assert.equal(parseUpdateNotice("nothing to see here"), null)
})

test("parseUpdateNotice returns null for empty or invalid input", () => {
  assert.equal(parseUpdateNotice(""), null)
  assert.equal(parseUpdateNotice(null), null)
  assert.equal(parseUpdateNotice(undefined), null)
  assert.equal(parseUpdateNotice("cymbal update:\n  Run: missing version"), null)
})

test("buildNotificationCommand returns osascript for darwin", () => {
  assert.deepStrictEqual(
    buildNotificationCommand(
      "darwin",
      { title: "Update", body: 'Run "cymbal" \\ now\nplease' },
      {},
    ),
    {
      command: "osascript",
      args: [
        "-e",
        'display notification "Run \\\"cymbal\\\" \\\\ now please" with title "Update"',
      ],
    },
  )
})

test("buildNotificationCommand returns null for linux without DISPLAY or WAYLAND_DISPLAY", () => {
  assert.equal(
    buildNotificationCommand("linux", { title: "Update", body: "Body" }, {}),
    null,
  )
})

test("buildNotificationCommand returns notify-send for linux with DISPLAY", () => {
  assert.deepStrictEqual(
    buildNotificationCommand("linux", { title: "Update", body: "Body" }, { DISPLAY: ":0" }),
    {
      command: "notify-send",
      args: [
        "--app-name=cymbal",
        "--urgency=normal",
        "--expire-time=10000",
        "--",
        "Update",
        "Body",
      ],
    },
  )
})

test("buildNotificationCommand returns notify-send for linux with WAYLAND_DISPLAY", () => {
  assert.deepStrictEqual(
    buildNotificationCommand("linux", { title: "Update", body: "Body" }, { WAYLAND_DISPLAY: "wayland-0" }),
    {
      command: "notify-send",
      args: [
        "--app-name=cymbal",
        "--urgency=normal",
        "--expire-time=10000",
        "--",
        "Update",
        "Body",
      ],
    },
  )
})

test("buildNotificationCommand returns powershell for win32", () => {
  assert.deepStrictEqual(
    buildNotificationCommand("win32", { title: "O'Reilly", body: "Line 1\nLine 2" }, {}),
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
  )
})

test("buildNotificationCommand returns null for unsupported platform", () => {
  assert.equal(
    buildNotificationCommand("freebsd", { title: "Update", body: "Body" }, {}),
    null,
  )
})

test("buildNotificationCommand returns null for invalid notice object", () => {
  assert.equal(buildNotificationCommand("darwin", null, {}), null)
  assert.equal(buildNotificationCommand("darwin", {}, {}), null)
  assert.equal(buildNotificationCommand("darwin", { title: "Only title" }, {}), null)
  assert.equal(buildNotificationCommand("darwin", { body: "Only body" }, {}), null)
})

test("updateNotifierDisabled returns true for enabled disable values", () => {
  const original = process.env.CYMBAL_NO_UPDATE_NOTIFIER

  try {
    for (const value of ["1", "true", "yes", "on"]) {
      process.env.CYMBAL_NO_UPDATE_NOTIFIER = value
      assert.equal(updateNotifierDisabled(), true)
    }
  } finally {
    if (original === undefined) {
      delete process.env.CYMBAL_NO_UPDATE_NOTIFIER
    } else {
      process.env.CYMBAL_NO_UPDATE_NOTIFIER = original
    }
  }
})

test("updateNotifierDisabled returns false for unset env", () => {
  const original = process.env.CYMBAL_NO_UPDATE_NOTIFIER

  try {
    delete process.env.CYMBAL_NO_UPDATE_NOTIFIER
    assert.equal(updateNotifierDisabled(), false)
  } finally {
    if (original === undefined) {
      delete process.env.CYMBAL_NO_UPDATE_NOTIFIER
    } else {
      process.env.CYMBAL_NO_UPDATE_NOTIFIER = original
    }
  }
})

test("updateNotifierDisabled returns false for disabled-looking values", () => {
  const original = process.env.CYMBAL_NO_UPDATE_NOTIFIER

  try {
    for (const value of ["0", "false", "no", "off"]) {
      process.env.CYMBAL_NO_UPDATE_NOTIFIER = value
      assert.equal(updateNotifierDisabled(), false)
    }
  } finally {
    if (original === undefined) {
      delete process.env.CYMBAL_NO_UPDATE_NOTIFIER
    } else {
      process.env.CYMBAL_NO_UPDATE_NOTIFIER = original
    }
  }
})

test("appleScriptString escapes backslashes, quotes, and newlines", () => {
  assert.equal(appleScriptString('path \\ "quoted"\nline\r\nnext'), 'path \\\\ \\\"quoted\\\" line  next')
})
