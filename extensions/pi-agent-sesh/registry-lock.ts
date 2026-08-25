import { spawn, type ChildProcess } from "node:child_process";
import { once } from "node:events";
import { fileURLToPath } from "node:url";
import { dirname, join } from "node:path";

// Must match internal/registry/lock.go (flock on path+".lock").
export const lockPollMs = 25;
export const lockTimeoutMs = 5000;
const lockReleaseTimeoutMs = 1000;

let activeHolder: ChildProcess | null = null;

process.once("exit", () => {
	if (activeHolder && activeHolder.exitCode === null) {
		activeHolder.kill("SIGKILL");
	}
});

const flockHelper = join(
	dirname(fileURLToPath(import.meta.url)),
	"flock-hold.py",
);

function pythonCommand(): string {
	return process.env.AGENT_SESH_PYTHON ?? "python3";
}

async function spawnLockHolder(lockPath: string): Promise<ChildProcess> {
	return spawn(pythonCommand(), [
		flockHelper,
		lockPath,
		String(lockTimeoutMs / 1000),
		String(lockPollMs / 1000),
	], {
		stdio: ["pipe", "pipe", "inherit"],
	});
}

async function tryAcquireLock(lockPath: string): Promise<ChildProcess | null> {
	const child = await spawnLockHolder(lockPath);
	const stdout = child.stdout;
	if (!stdout) {
		child.kill();
		return null;
	}
	stdout.setEncoding("utf8");
	return new Promise((resolve) => {
		let buf = "";
		const onData = (chunk: string) => {
			buf += chunk;
			if (!buf.includes("ready")) {
				return;
			}
			cleanup();
			stdout.resume();
			resolve(child);
		};
		const onExit = () => {
			cleanup();
			resolve(null);
		};
		const cleanup = () => {
			stdout.off("data", onData);
			child.off("exit", onExit);
		};
		stdout.on("data", onData);
		child.on("exit", onExit);
	});
}

async function releaseLockHolder(holder: ChildProcess): Promise<void> {
	const stdin = holder.stdin;
	if (stdin && !stdin.destroyed) {
		stdin.end();
	}

	const deadline = Date.now() + lockReleaseTimeoutMs;
	while (Date.now() < deadline && holder.exitCode === null) {
		await new Promise((resolve) => setTimeout(resolve, lockPollMs));
	}
	if (holder.exitCode !== null) {
		return;
	}

	holder.kill("SIGTERM");
	await Promise.race([
		once(holder, "exit"),
		new Promise((resolve) => setTimeout(resolve, lockPollMs * 4)),
	]);
	if (holder.exitCode === null) {
		holder.kill("SIGKILL");
	}
}

export async function withRegistryFileLock<T>(
	lockPath: string,
	fn: () => Promise<T>,
): Promise<T> {
	const deadline = Date.now() + lockTimeoutMs;
	while (Date.now() < deadline) {
		const holder = await tryAcquireLock(lockPath);
		if (!holder) {
			await new Promise((resolve) => setTimeout(resolve, lockPollMs));
			continue;
		}
		activeHolder = holder;
		try {
			return await fn();
		} finally {
			activeHolder = null;
			await releaseLockHolder(holder);
		}
	}
	throw new Error(`timed out waiting for registry lock ${lockPath}`);
}
