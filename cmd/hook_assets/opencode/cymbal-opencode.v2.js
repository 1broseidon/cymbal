import { spawn } from "node:child_process";

// Dual V1/V2 OpenCode plugin. OpenCode 1.x calls server(), 2.x calls setup().
// This file ships as a static go:embed asset without a bundle step, so it must
// not import @opencode/* at runtime: the plain object { id, setup, server } is
// accepted by both majors (object entrypoints require OpenCode >= 1.18.29 on
// the 1.x side).

const notifiedUpdateVersions = new Set();

function updateNotifierDisabled() {
	const value = String(process.env.CYMBAL_NO_UPDATE_NOTIFIER ?? "")
		.trim()
		.toLowerCase();
	return value === "1" || value === "true" || value === "yes" || value === "on";
}

function appleScriptString(value) {
	return String(value)
		.replaceAll("\\", "\\\\")
		.replaceAll('"', '\\"')
		.replaceAll("\r", " ")
		.replaceAll("\n", " ");
}

function powerShellSingleQuotedString(value) {
	return String(value).replaceAll("'", "''");
}

function buildNotificationCommand(platform, notice, env) {
	if (
		!notice ||
		typeof notice.title !== "string" ||
		typeof notice.body !== "string"
	)
		return null;

	if (platform === "darwin") {
		return {
			command: "osascript",
			args: [
				"-e",
				`display notification "${appleScriptString(notice.body)}" with title "${appleScriptString(notice.title)}"`
			]
		};
	}

	if (platform === "linux") {
		const hasDisplay = Boolean(
			env && (env.DISPLAY !== undefined || env.WAYLAND_DISPLAY !== undefined)
		);
		if (!hasDisplay) return null;

		return {
			command: "notify-send",
			args: [
				"--app-name=cymbal",
				"--urgency=normal",
				"--expire-time=10000",
				"--",
				notice.title,
				notice.body
			]
		};
	}

	if (platform === "win32") {
		const title = powerShellSingleQuotedString(notice.title);
		const body = powerShellSingleQuotedString(notice.body);
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
					"$notify.Dispose()"
				].join("; ")
			]
		};
	}

	return null;
}

async function showNativeNotification(notice) {
	const spec = buildNotificationCommand(process.platform, notice, process.env);
	if (!spec) return;

	try {
		const child = spawn(spec.command, spec.args, {
			detached: true,
			stdio: "ignore",
			windowsHide: true
		});
		child.once("error", () => {});
		child.unref();
	} catch (error) {
		void error;
	}
}

// announceUpdate parses a `cymbal hook notify` payload and shows the native
// notification once per version.
async function announceUpdate(raw) {
	const payload = JSON.parse(raw.trim() || "{}");
	if (!payload.notify || !payload.latestVersion) return;
	if (notifiedUpdateVersions.has(payload.latestVersion)) return;

	notifiedUpdateVersions.add(payload.latestVersion);
	await showNativeNotification({
		title: payload.title,
		body: payload.body
	});
}

// OpenCode V2 has no Bun `$`; ctx.shell is only a create.before hook surface,
// not a runner. Every cymbal hook call goes through child_process instead.
// Resolves to the child's stdout, or "" when cymbal cannot be started
// (mirroring the V1 asset's `.quiet().nothrow().text()`).
function runCymbalHook(args, { input } = {}) {
	return new Promise(resolve => {
		const child = spawn("cymbal", args, { windowsHide: true });
		let stdout = "";
		child.stdout?.on("data", chunk => {
			stdout += String(chunk);
		});
		child.once("error", () => resolve(""));
		child.once("close", () => resolve(stdout));
		if (input !== undefined) child.stdin?.end(input);
	});
}

async function notifyUpdateFromCymbal() {
	if (updateNotifierDisabled()) return;

	try {
		await announceUpdate(
			await runCymbalHook(["hook", "notify", "--format=json", "--update=cache"])
		);
	} catch (error) {
		void error;
	}
}

// V1 transport (Bun `$`), ported 1:1 from the V1 asset.
async function notifyUpdateFromCymbalShell($) {
	if (updateNotifierDisabled()) return;

	try {
		await announceUpdate(
			await $`cymbal hook notify --format=json --update=cache`
				.quiet()
				.nothrow()
				.text()
		);
	} catch (error) {
		void error;
	}
}

function setup(ctx) {
	if (
		!ctx ||
		typeof ctx.session?.hook !== "function" ||
		typeof ctx.tool?.hook !== "function"
	)
		return;

	// Reminder injection + update notifier. The `context` hook runs immediately
	// before each model request; its event exposes `system` (Array<SystemPart>),
	// unlike the `prompt` hook.
	ctx.session.hook("context", async event => {
		try {
			if (!event || !Array.isArray(event.system)) return;
			const raw = await runCymbalHook([
				"hook",
				"remind",
				"--format=text",
				"--update=if-stale"
			]);
			const text = raw.trim();
			if (text) event.system.push({ type: "text", text });
			await notifyUpdateFromCymbal();
		} catch (error) {
			void error;
		}
	});

	// Nudge: V2 bubblehooks report the effective tool name. The shell tool is
	// "shell" here (V1: "bash"). Grep/Glob are deliberately not nudged in V2:
	// execute.before only allows mutating event.input, and rewriting the search
	// pattern would break the search. Grep/Glob parity is deferred to a
	// follow-up PR that drains a pending hint into the context hook instead.
	ctx.tool.hook("execute.before", async event => {
		if (!event || typeof event.input !== "object" || event.input === null)
			return;

		if (event.tool !== "shell") return;
		if (process.platform === "win32") return;

		const command = event.input.command;
		if (typeof command !== "string") return;

		try {
			const payload = JSON.stringify({
				tool_name: event.tool,
				tool_input: { command }
			});
			const raw = await runCymbalHook(["hook", "nudge", "--format=json"], {
				input: payload
			});
			const text = raw.trim();
			if (!text) return;

			const result = JSON.parse(text);
			if (typeof result.suggest !== "string" || typeof result.why !== "string")
				return;

			const notice =
				`cymbal nudge: ${result.suggest} — ${result.why}`.replaceAll(
					"'",
					`'"'"'`
				);
			event.input.command = `printf '%s\n' '${notice}' >&2; ${command}`;
		} catch (error) {
			void error;
		}
	});
}

// V1 entrypoint — ported 1:1 from the V1 asset (unchanged behaviour).
async function server(ctx) {
	const { $ } = ctx ?? {};
	return {
		"experimental.chat.system.transform": async (_input, output) => {
			try {
				const reminder =
					await $`cymbal hook remind --format=text --update=if-stale`.text();
				const text = reminder.trim();
				if (text) output.system.push(text);
				await notifyUpdateFromCymbalShell($);
			} catch (error) {
				void error;
			}
		},
		"tool.execute.before": async (input, output) => {
			const nudgableTools = ["bash", "Grep", "Glob"];
			if (!nudgableTools.includes(input.tool)) return;
			if (process.platform === "win32") return;

			try {
				const toolInput =
					input.tool === "bash"
						? { command: output.args?.command }
						: { ...output.args };
				if (input.tool === "bash" && typeof toolInput.command !== "string")
					return;

				const payload = new Response(
					JSON.stringify({
						tool_name: input.tool === "bash" ? "bash" : input.tool,
						tool_input: toolInput
					})
				);
				const raw = await $`cymbal hook nudge --format=json < ${payload}`
					.quiet()
					.nothrow()
					.text();
				const text = raw.trim();
				if (!text) return;

				const result = JSON.parse(text);
				if (
					typeof result.suggest !== "string" ||
					typeof result.why !== "string"
				)
					return;

				const notice =
					`cymbal nudge: ${result.suggest} — ${result.why}`.replaceAll(
						"'",
						`'"'"'`
					);
				if (input.tool === "bash") {
					output.args.command = `printf '%s\n' '${notice}' >&2; ${output.args.command}`;
				} else {
					output.notice = notice;
				}
			} catch (error) {
				void error;
			}
		}
	};
}

export default { id: "cymbal", setup, server };
