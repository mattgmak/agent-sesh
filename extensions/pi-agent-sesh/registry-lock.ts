import { closeSync, openSync } from "node:fs";
import koffi from "koffi";

// Must match internal/registry/lock.go (flock on path+".lock").
export const lockPollMs = 25;
export const lockTimeoutMs = 5000;

const LOCK_EX = 2;
const LOCK_NB = 4;
const LOCK_UN = 8;

const libcName =
	process.platform === "darwin" ? "libSystem.B.dylib" : "libc.so.6";
const libc = koffi.load(libcName);
const flock = libc.func("int flock(int fd, int operation)");

export async function withRegistryFileLock<T>(
	lockPath: string,
	fn: () => Promise<T>,
): Promise<T> {
	const deadline = Date.now() + lockTimeoutMs;
	while (Date.now() < deadline) {
		const fd = openSync(lockPath, "a+");
		if (flock(fd, LOCK_EX | LOCK_NB) === 0) {
			try {
				return await fn();
			} finally {
				flock(fd, LOCK_UN);
				closeSync(fd);
			}
		}
		closeSync(fd);
		await new Promise((resolve) => setTimeout(resolve, lockPollMs));
	}
	throw new Error(`timed out waiting for registry lock ${lockPath}`);
}
