import test from "node:test";
import assert from "node:assert/strict";
import { spawnSync } from "node:child_process";

const pluginURL = new URL("./cymbal-opencode.v2.js", import.meta.url).href;

// Run the actual hooks in an isolated host, substituting only the executable.
// Real OS pipes exercise backpressure and asynchronous stream errors that the
// unit tests' fake streams cannot. Bound failures so a regression cannot hang CI.
function runHook(childSource, exercise) {
	const driver = `
		import assert from "node:assert/strict";
		import { createRequire, syncBuiltinESMExports } from "node:module";
		import plugin from ${JSON.stringify(pluginURL)};
		const cp = createRequire(import.meta.url)("node:child_process");
		const originalSpawn = cp.spawn;
		const children = [];
		cp.spawn = (command, args, options) => {
			assert.equal(command, "cymbal");
			const child = originalSpawn(process.execPath,
				["-e", ${JSON.stringify(childSource)}], options);
			children.push(child);
			return child;
		};
		syncBuiltinESMExports();
		process.env.CYMBAL_NO_UPDATE_NOTIFIER = "1";
		let context, nudge;
		plugin.setup({
			session: { hook(name, fn) { context = fn; } },
			tool: { hook(name, fn) { nudge = fn; } }
		});
		const deadline = setTimeout(() => {
			for (const child of children) child.kill("SIGKILL");
			console.error("hook did not finish before deadline");
			process.exit(1);
		}, 3000);
		deadline.unref();
		${exercise}
	`;
	const result = spawnSync(process.execPath, ["--input-type=module", "-e", driver], {
		encoding: "utf8",
		timeout: 6000,
		killSignal: "SIGKILL"
	});
	assert.ifError(result.error);
	assert.equal(result.status, 0, result.stderr);
	assert.equal(result.signal, null);
}

test("V2 reminder completes despite noisy stderr and receives stdin EOF", () => {
	runHook(
		`const fs = require("node:fs");
		fs.writeSync(2, Buffer.alloc(2 * 1024 * 1024, "x"));
		if (fs.readFileSync(0, "utf8") !== "") process.exit(2);
		process.stdout.write("use cymbal search\\n");`,
		`const event = { system: [] };
		await context(event);
		assert.deepStrictEqual(event.system, [{type: "text", text: "use cymbal search"}]);`
	);
});

test("V2 nudge survives an early child exit without changing the command", {
	skip: process.platform === "win32" && "shell nudges are disabled on Windows"
}, () => {
	runHook(
		"process.exit(0);",
		`const command = "x".repeat(4 * 1024 * 1024);
		const event = { tool: "shell", input: { command } };
		await nudge(event);
		assert.equal(event.input.command, command);`
	);
});

test("V2 nudge still delivers its payload and consumes successful stdout", {
	skip: process.platform === "win32" && "shell nudges are disabled on Windows"
}, () => {
	runHook(
		`const fs = require("node:fs");
		const payload = JSON.parse(fs.readFileSync(0, "utf8"));
		if (payload.tool_name !== "shell" || payload.tool_input.command !== "grep needle .") process.exit(2);
		process.stdout.write(JSON.stringify({suggest: "cymbal search needle", why: "try indexed search"}));`,
		`const event = { tool: "shell", input: { command: "grep needle ." } };
		await nudge(event);
		assert.match(event.input.command, /cymbal nudge: cymbal search needle/);
		assert.ok(event.input.command.endsWith("; grep needle ."));`
	);
});
