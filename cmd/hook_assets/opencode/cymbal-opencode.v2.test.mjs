import test from "node:test";
import assert from "node:assert/strict";
import { createRequire, syncBuiltinESMExports } from "node:module";

import plugin from "./cymbal-opencode.v2.js";

// The default export is the dual V1/V2 object: V1 calls server(), V2 calls
// setup(). Tests exercise the V2 hooks through a fake context (which records
// registrations and their disposers) plus the notification paths.

const childProcess = createRequire(import.meta.url)("node:child_process");

// Unset in every notification test, so the host environment cannot leak in.
const QUIET_ENV = {
	CYMBAL_NO_UPDATE_NOTIFIER: undefined,
	DISPLAY: undefined,
	WAYLAND_DISPLAY: undefined
};

// fakeContext stands in for OpenCode's V2 ctx: session.hook and tool.hook
// record their registrations and return a disposer, mimicking the real API.
function fakeContext() {
	const registrations = [];
	const register = kind => (name, fn) => {
		const reg = { kind, name, fn, dispose: null, disposed: false };
		reg.dispose = () => {
			reg.disposed = true;
		};
		registrations.push(reg);
		return reg.dispose;
	};
	return {
		ctx: {
			session: { hook: register("session") },
			tool: { hook: register("tool") }
		},
		registrations
	};
}

// setupRegistered sets up the plugin against a fresh fake context.
function setupRegistered() {
	const { ctx, registrations } = fakeContext();
	plugin.setup(ctx);
	return { ctx, registrations };
}

// registeredHook finds the hook with the given kind/name and fails the test
// if it was never registered.
function registeredHook(registrations, kind, name) {
	const found = registrations.find(
		reg => reg.kind === kind && reg.name === name
	);
	assert.ok(found, `expected ${kind} hook ${name} to be registered`);
	return found.fn;
}

// runContextHook invokes the context hook once on an empty system array.
async function runContextHook() {
	const { registrations } = setupRegistered();
	await registeredHook(registrations, "session", "context")({ system: [] });
}

// fakeShell stands in for OpenCode 1.x's Bun $: each `cymbal hook <name>` call
// prints outputs[name], and every call is recorded. Only the V1 server() tests
// use it — the V2 hooks go through child_process instead.
function fakeShell(outputs = {}) {
	const calls = [];
	const $ = (strings, ...values) => {
		const command = strings.join("").trim();
		const name = command.split(/\s+/)[2];
		calls.push({ name, command, values });
		const result = {
			quiet: () => result,
			nothrow: () => result,
			text: async () => outputs[name] ?? ""
		};
		return result;
	};
	return { $, calls };
}

// Each update test announces a new version, since the plugin announces a
// version once.
let updateVersion = 0;
function notifyOutput(notice) {
	updateVersion += 1;
	return JSON.stringify({
		notify: true,
		latestVersion: `9.9.${updateVersion}`,
		...notice
	});
}

// withEnvironment runs fn with process.platform and environment variables set,
// and spawn recorded and driven instead of run, then restores all three. The
// fake spawn answers `cymbal hook <name>` from outputs and records
// notifications (osascript / notify-send / powershell.exe) in the same list.
async function withEnvironment(
	{ platform = process.platform, env = {} },
	outputs,
	fn
) {
	const spawned = [];
	const originalSpawn = childProcess.spawn;
	const originalPlatform = Object.getOwnPropertyDescriptor(process, "platform");
	const originalEnv = {};
	const setEnv = (key, value) => {
		if (value === undefined) delete process.env[key];
		else process.env[key] = value;
	};

	childProcess.spawn = (command, args) => {
		const rec = { command, args };
		spawned.push(rec);

		// Notifications are fire-and-forget: detached, stdio ignored, unref'd.
		if (command !== "cymbal") return { once() {}, on() {}, unref() {} };

		const listeners = {};
		const stream = {
			on(name, fn) {
				(listeners[name] ??= []).push(fn);
				return stream;
			},
			once(name, fn) {
				(listeners[name] ??= []).push(fn);
				return stream;
			},
			emit(name, ...rest) {
				for (const fn of listeners[name] ?? []) fn(...rest);
			}
		};
		const name = args.includes("remind")
			? "remind"
			: args.includes("notify")
				? "notify"
				: args.includes("nudge")
					? "nudge"
					: "default";
		const output = outputs?.[name] ?? "";
		const child = {
			stdout: stream,
			stdin: {
				end(input) {
					rec.input = input;
				}
			},
			on: (...args) => stream.on(...args),
			once: (...args) => stream.once(...args),
			unref() {}
		};
		queueMicrotask(() => {
			stream.emit("data", output);
			stream.emit("close");
		});
		return child;
	};
	syncBuiltinESMExports();
	Object.defineProperty(process, "platform", {
		...originalPlatform,
		value: platform
	});
	for (const [key, value] of Object.entries(env)) {
		originalEnv[key] = process.env[key];
		setEnv(key, value);
	}
	try {
		await fn(spawned);
	} finally {
		childProcess.spawn = originalSpawn;
		syncBuiltinESMExports();
		Object.defineProperty(process, "platform", originalPlatform);
		for (const [key, value] of Object.entries(originalEnv)) setEnv(key, value);
	}
}

const notifications = spawned => spawned.filter(c => c.command !== "cymbal");
const hookCalls = spawned => spawned.filter(c => c.command === "cymbal");

test("the default export carries the dual V1/V2 object form", async () => {
	assert.equal(typeof plugin, "object");
	assert.equal(plugin.id, "cymbal");
	assert.equal(typeof plugin.setup, "function");
	assert.equal(typeof plugin.server, "function");

	const v1 = await plugin.server({ $: fakeShell({}).$ });
	assert.equal(typeof v1["experimental.chat.system.transform"], "function");
	assert.equal(typeof v1["tool.execute.before"], "function");
});

test("setup registers the V2 context and execute.before hooks, both disposable", () => {
	const { registrations } = setupRegistered();
	assert.deepStrictEqual(
		registrations.map(r => `${r.kind}:${r.name}`),
		["session:context", "tool:execute.before"]
	);
	for (const reg of registrations) {
		assert.equal(reg.disposed, false);
		reg.dispose();
		assert.equal(reg.disposed, true);
	}
});

test("setup no-ops defensively when ctx or its hook surfaces are missing", () => {
	assert.doesNotThrow(() => plugin.setup(undefined));
	assert.doesNotThrow(() => plugin.setup({}));
	assert.doesNotThrow(() => plugin.setup({ session: {} }));
});

test("the context hook adds cymbal's reminder as a text system part", async () => {
	for (const [reminder, want] of [
		["  use cymbal search\n", [{ type: "text", text: "use cymbal search" }]],
		[" \n", []]
	]) {
		await withEnvironment(
			{ env: { CYMBAL_NO_UPDATE_NOTIFIER: "1" } },
			{ remind: reminder },
			async () => {
				const { registrations } = setupRegistered();
				const event = { system: [] };
				await registeredHook(registrations, "session", "context")(event);
				assert.deepStrictEqual(event.system, want);
			}
		);
	}
});

test("an update on macOS runs osascript with the text escaped for AppleScript", async () => {
	await withEnvironment(
		{ platform: "darwin", env: QUIET_ENV },
		{
			notify: notifyOutput({
				title: 'Say "hi"',
				body: 'Run "cymbal" \\ now\nplease\r\n'
			})
		},
		async spawned => {
			await runContextHook();
			assert.deepStrictEqual(notifications(spawned), [
				{
					command: "osascript",
					args: [
						"-e",
						'display notification "Run \\"cymbal\\" \\\\ now please  " with title "Say \\"hi\\""'
					]
				}
			]);
		}
	);
});

test("an update on Linux uses notify-send only when there is a display", async () => {
	const notifySend = {
		command: "notify-send",
		args: [
			"--app-name=cymbal",
			"--urgency=normal",
			"--expire-time=10000",
			"--",
			"Update",
			"Body"
		]
	};
	for (const [display, want] of [
		[{}, []],
		[{ DISPLAY: ":0" }, [notifySend]],
		[{ WAYLAND_DISPLAY: "wayland-0" }, [notifySend]]
	]) {
		await withEnvironment(
			{ platform: "linux", env: { ...QUIET_ENV, ...display } },
			{ notify: notifyOutput({ title: "Update", body: "Body" }) },
			async spawned => {
				await runContextHook();
				assert.deepStrictEqual(
					notifications(spawned),
					want,
					JSON.stringify(display)
				);
			}
		);
	}
});

test("an update on Windows runs PowerShell with single quotes doubled", async () => {
	await withEnvironment(
		{ platform: "win32", env: QUIET_ENV },
		{ notify: notifyOutput({ title: "O'Reilly", body: "Line 1\nLine 2" }) },
		async spawned => {
			await runContextHook();
			assert.deepStrictEqual(notifications(spawned), [
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
							"$notify.Dispose()"
						].join("; ")
					]
				}
			]);
		}
	);
});

test("no notification on other platforms, or for a notice cymbal did not complete", async () => {
	await withEnvironment(
		{ platform: "freebsd", env: QUIET_ENV },
		{ notify: notifyOutput({ title: "Update", body: "Body" }) },
		async spawned => {
			await runContextHook();
			assert.deepStrictEqual(notifications(spawned), []);
		}
	);

	for (const notice of [
		{ notify: false, title: "Update", body: "Body" },
		{},
		{ title: "Only title" },
		{ body: "Only body" },
		{ title: 1, body: "Body" }
	]) {
		await withEnvironment(
			{ platform: "darwin", env: QUIET_ENV },
			{ notify: notifyOutput(notice) },
			async spawned => {
				await runContextHook();
				assert.deepStrictEqual(
					notifications(spawned),
					[],
					JSON.stringify(notice)
				);
			}
		);
	}
});

test("each version is announced once", async () => {
	await withEnvironment(
		{ platform: "darwin", env: QUIET_ENV },
		{
			notify: JSON.stringify({
				notify: true,
				latestVersion: "8.0.0",
				title: "Update",
				body: "Body"
			})
		},
		async spawned => {
			const { registrations } = setupRegistered();
			const hook = registeredHook(registrations, "session", "context");
			await hook({ system: [] });
			await hook({ system: [] });
			assert.equal(notifications(spawned).length, 1);
		}
	);
});

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
		["off", true]
	]) {
		await withEnvironment(
			{
				platform: "darwin",
				env: { ...QUIET_ENV, CYMBAL_NO_UPDATE_NOTIFIER: value }
			},
			{ notify: notifyOutput({ title: "Update", body: "Body" }) },
			async spawned => {
				await runContextHook();
				assert.equal(
					notifications(spawned).length,
					checks ? 1 : 0,
					`CYMBAL_NO_UPDATE_NOTIFIER=${value}`
				);
			}
		);
	}
});

test("execute.before nudges the shell tool by prefixing its command", async () => {
	await withEnvironment(
		{ platform: "linux", env: QUIET_ENV },
		{
			nudge: JSON.stringify({
				suggest: "cymbal search Foo",
				why: "it's indexed"
			})
		},
		async spawned => {
			const { registrations } = setupRegistered();
			const hook = registeredHook(registrations, "tool", "execute.before");
			const notice = `cymbal nudge: cymbal search Foo — it'"'"'s indexed`;

			const event = { tool: "shell", input: { command: "grep -rn Foo ." } };
			await hook(event);
			assert.equal(
				event.input.command,
				`printf '%s\n' '${notice}' >&2; grep -rn Foo .`
			);
			assert.deepStrictEqual(hookCalls(spawned), [
				{
					command: "cymbal",
					args: ["hook", "nudge", "--format=json"],
					input: JSON.stringify({
						tool_name: "shell",
						tool_input: { command: "grep -rn Foo ." }
					})
				}
			]);
		}
	);
});

test("execute.before leaves grep/glob and other tools alone", async () => {
	await withEnvironment(
		{ platform: "linux", env: QUIET_ENV },
		{ nudge: JSON.stringify({ suggest: "cymbal search Foo", why: "indexed" }) },
		async spawned => {
			const { registrations } = setupRegistered();
			const hook = registeredHook(registrations, "tool", "execute.before");
			for (const tool of [
				"grep",
				"glob",
				"Grep",
				"Glob",
				"Read",
				"weird-tool"
			]) {
				const event = { tool, input: { pattern: "Foo" } };
				await hook(event);
				assert.deepStrictEqual(event.input, { pattern: "Foo" }, tool);
			}
			assert.deepStrictEqual(hookCalls(spawned), []);
		}
	);
});

test("execute.before leaves Windows and unusable nudges alone", async () => {
	const nudge = JSON.stringify({
		suggest: "cymbal search Foo",
		why: "indexed"
	});
	for (const output of [
		"",
		"not json",
		JSON.stringify({ suggest: "cymbal search Foo" })
	]) {
		await withEnvironment(
			{ platform: "linux", env: QUIET_ENV },
			{ nudge: output },
			async () => {
				const { registrations } = setupRegistered();
				const event = { tool: "shell", input: { command: "ls" } };
				await registeredHook(registrations, "tool", "execute.before")(event);
				assert.equal(event.input.command, "ls", JSON.stringify(output));
			}
		);
	}
	await withEnvironment(
		{ platform: "win32", env: QUIET_ENV },
		{ nudge },
		async spawned => {
			const { registrations } = setupRegistered();
			const event = { tool: "shell", input: { command: "ls" } };
			await registeredHook(registrations, "tool", "execute.before")(event);
			assert.equal(event.input.command, "ls");
			assert.deepStrictEqual(hookCalls(spawned), []);
		}
	);
});

test("V2 hooks no-op defensively when event fields are absent", async () => {
	await withEnvironment(
		{ env: { CYMBAL_NO_UPDATE_NOTIFIER: "1" } },
		{},
		async () => {
			const { registrations } = setupRegistered();
			const context = registeredHook(registrations, "session", "context");
			await context(undefined);
			await context({});
			await context({ system: "not-an-array" });

			const execute = registeredHook(registrations, "tool", "execute.before");

			await execute(undefined);
			await execute({ tool: "shell" });
			await execute({ tool: "shell", input: {} });
			await execute({ tool: "shell", input: { command: 42 } });
			await execute({ tool: "grep", input: { pattern: "Foo" } });
		}
	);
});

test("server() keeps the V1 bash nudge behaviour unchanged", async () => {
	await withEnvironment(
		{ platform: "linux", env: QUIET_ENV },
		{ nudge: JSON.stringify({ suggest: "cymbal search Foo", why: "indexed" }) },
		async spawned => {
			const shell = fakeShell({
				nudge: JSON.stringify({ suggest: "cymbal search Foo", why: "indexed" })
			});
			const hooks = await plugin.server({ $: shell.$ });
			const notice = `cymbal nudge: cymbal search Foo — indexed`;

			const bash = { args: { command: "ls" } };
			await hooks["tool.execute.before"]({ tool: "bash" }, bash);
			assert.equal(bash.args.command, `printf '%s\n' '${notice}' >&2; ls`);
			assert.deepStrictEqual(
				JSON.parse(await shell.calls[0].values[0].text()),
				{
					tool_name: "bash",
					tool_input: { command: "ls" }
				}
			);
			assert.deepStrictEqual(hookCalls(spawned), []);
		}
	);
});

test("server() keeps the V1 Grep/Glob nudge notice behaviour unchanged", async () => {
	await withEnvironment(
		{ platform: "linux", env: QUIET_ENV },
		{
			nudge: JSON.stringify({
				suggest: "cymbal search Foo",
				why: "it's indexed"
			})
		},
		async spawned => {
			const shell = fakeShell({
				nudge: JSON.stringify({
					suggest: "cymbal search Foo",
					why: "it's indexed"
				})
			});
			const hooks = await plugin.server({ $: shell.$ });
			const notice = `cymbal nudge: cymbal search Foo — it'"'"'s indexed`;

			const grep = { args: { pattern: "Foo" } };
			await hooks["tool.execute.before"]({ tool: "Grep" }, grep);
			assert.equal(grep.notice, notice);
			assert.deepStrictEqual(grep.args, { pattern: "Foo" });

			const glob = { args: { pattern: "**/*.go" } };
			await hooks["tool.execute.before"]({ tool: "Glob" }, glob);
			assert.equal(glob.notice, notice);
			assert.deepStrictEqual(glob.args, { pattern: "**/*.go" });

			assert.deepStrictEqual(hookCalls(spawned), []);
		}
	);
});

test("server() keeps the V1 transform reminder behaviour unchanged", async () => {
	const shell = fakeShell({ remind: "  use cymbal search\n" });
	const hooks = await plugin.server({ $: shell.$ });
	const output = { system: [] };
	await hooks["experimental.chat.system.transform"]({}, output);
	assert.deepStrictEqual(output.system, ["use cymbal search"]);
});
