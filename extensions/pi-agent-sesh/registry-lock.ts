import { spawn, type ChildProcess } from "node:child_process";
import { once } from "node:events";
import { fileURLToPath } from "node:url";
import { dirname, join } from "node:path";

// Must match internal/registry/lock.go (flock on path+".lock").
export const lockPollMs = 25;
export const lockTimeoutMs = 5000;

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
	return new Promise((resolve) => {
		const onData = (chunk: Buffer) => {
			cleanup();
			if (chunk.toString().trim() === "ready") {
				resolve(child);
				return;
			}
			child.kill();
			resolve(null);
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
		try {
			return await fn();
		} finally {
			holder.stdin?.write("x");
			holder.stdin?.end();
			await once(holder, "exit");
		}
	}
	throw new Error(`timed out waiting for registry lock ${lockPath}`);
}
