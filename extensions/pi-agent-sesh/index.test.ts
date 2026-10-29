import assert from "node:assert/strict";
import { mkdir, mkdtemp, readFile, rm, writeFile } from "node:fs/promises";
import { tmpdir } from "node:os";
import { dirname, join } from "node:path";
import test from "node:test";
import {
	extractUserPromptText,
	haltedBlocksResume,
	isRunningSubagentActivity,
	reconcileStatusOnReload,
	resolveBgJobsRunning,
	resolvePiSessionName,
	resolveSubagentsRunning,
	resolveUpsertStatus,
	mergeSessions,
	setStatus,
	shouldApplyStatusTransition,
	writeRunningCounters,
	type AgentSeshSession,
} from "./index.ts";

test("resolvePiSessionName trims, clears, and keeps stored names", () => {
	assert.equal(resolvePiSessionName("  plan review  "), "plan review");
	assert.equal(resolvePiSessionName(undefined, "  stored  "), "stored");
	assert.equal(resolvePiSessionName(undefined, undefined), undefined);
	assert.equal(resolvePiSessionName(null, "stored"), undefined);
	assert.equal(resolvePiSessionName("   ", "stored"), undefined);
});

test("mergeSessions drops stale rows for one pi session", () => {
	const base = {
		cwd: "/tmp",
		title: "masta",
		pi_session_name: "masta",
		status: "idle",
		agent: "pi",
		updated_at: "2026-10-16T08:18:21.499Z",
	} as const;
	const stale = {
		...base,
		id: "01a13dbe",
		tmux_target: "%175",
	} as unknown as AgentSeshSession;
	const current = {
		...base,
		id: "01a13dbe",
		tmux_target: "%11",
		updated_at: "2026-10-16T08:24:43.119Z",
	} as unknown as AgentSeshSession;
	const other = {
		...base,
		id: "01a14328",
		tmux_target: "%112",
	} as unknown as AgentSeshSession;

	const merged = mergeSessions([stale, other], [current]);
	assert.equal(merged.length, 2);
	assert.equal(merged.find((s) => s.id === "01a13dbe")?.tmux_target, "%11");
	assert.equal(merged.find((s) => s.id === "01a14328")?.tmux_target, "%112");
});

test("reconcileStatusOnReload downgrades active statuses when idle", () => {
	assert.deepEqual(reconcileStatusOnReload(true, "working"), {
		status: "halted",
		tool_name: null,
	});
	assert.deepEqual(reconcileStatusOnReload(true, "tool_call"), {
		status: "halted",
		tool_name: null,
	});
});

test("reconcileStatusOnReload leaves halted and idle unchanged", () => {
	assert.deepEqual(reconcileStatusOnReload(true, "halted"), {});
	assert.deepEqual(reconcileStatusOnReload(true, "idle"), {});
});

test("reconcileStatusOnReload skips when agent is busy", () => {
	assert.deepEqual(reconcileStatusOnReload(false, "working"), {});
});

test("shouldApplyStatusTransition blocks working over halted by default", () => {
	assert.equal(shouldApplyStatusTransition("halted", "working"), false);
});

test("shouldApplyStatusTransition allows working over halted with resumeFromHalt", () => {
	assert.equal(
		shouldApplyStatusTransition("halted", "working", { resumeFromHalt: true }),
		true,
	);
});

test("shouldApplyStatusTransition blocks stale tool states over halted", () => {
	assert.equal(shouldApplyStatusTransition("halted", "tool_call"), false);
	assert.equal(shouldApplyStatusTransition("halted", "awaiting_input"), false);
});

test("resolveUpsertStatus blocks working over halted by default", () => {
	assert.equal(
		resolveUpsertStatus(
			{ status: "halted" } as Parameters<typeof resolveUpsertStatus>[0],
			"working",
		),
		"halted",
	);
});

test("resolveUpsertStatus allows working over halted with resumeFromHalt", () => {
	assert.equal(
		resolveUpsertStatus(
			{ status: "halted" } as Parameters<typeof resolveUpsertStatus>[0],
			"working",
			{ resumeFromHalt: true },
		),
		"working",
	);
});

test("resolveUpsertStatus always applies halted", () => {
	assert.equal(
		resolveUpsertStatus(
			{ status: "working" } as Parameters<typeof resolveUpsertStatus>[0],
			"halted",
		),
		"halted",
	);
});

test("resolveUpsertStatus blocks active writes over halted", () => {
	assert.equal(
		resolveUpsertStatus(
			{ status: "halted" } as Parameters<typeof resolveUpsertStatus>[0],
			"tool_call",
		),
		"halted",
	);
});

test("shouldApplyStatusTransition allows halted to clear or acknowledge", () => {
	assert.equal(shouldApplyStatusTransition("halted", "halted"), true);
	assert.equal(shouldApplyStatusTransition("halted", "idle"), true);
});

test("haltedBlocksResume rejects stale agent_start after newer halt", () => {
	const session = {
		status: "halted",
		updated_at: "2026-09-03T09:20:00.000Z",
	} as AgentSeshSession;
	assert.equal(
		haltedBlocksResume(session, "working", {
			resumeFromHalt: true,
			resumeIfHaltedBefore: Date.parse("2026-09-03T09:19:00.000Z"),
		}),
		true,
	);
});

test("haltedBlocksResume allows resume when halt predates agent_start", () => {
	const session = {
		status: "halted",
		updated_at: "2026-09-03T09:19:00.000Z",
	} as AgentSeshSession;
	assert.equal(
		haltedBlocksResume(session, "working", {
			resumeFromHalt: true,
			resumeIfHaltedBefore: Date.parse("2026-09-03T09:20:00.000Z"),
		}),
		false,
	);
});

test("shouldApplyStatusTransition allows normal in-run updates", () => {
	assert.equal(shouldApplyStatusTransition("working", "tool_call"), true);
	assert.equal(shouldApplyStatusTransition("tool_call", "working"), true);
	assert.equal(shouldApplyStatusTransition("idle", "working"), true);
});

test("extractUserPromptText reads string and multipart user messages", () => {
	assert.equal(
		extractUserPromptText({
			role: "user",
			content: "  fix tests  ",
		}),
		"fix tests",
	);
	assert.equal(
		extractUserPromptText({
			role: "user",
			content: [
				{ type: "text", text: "line one" },
				{ type: "text", text: "line two" },
			],
		}),
		"line one\nline two",
	);
});

test("extractUserPromptText ignores non-user and empty messages", () => {
	assert.equal(
		extractUserPromptText({
			role: "assistant",
			content: [{ type: "text", text: "hello" }],
		}),
		undefined,
	);
	assert.equal(
		extractUserPromptText({
			role: "user",
			content: "   ",
		}),
		undefined,
	);
});

test("resolveSubagentsRunning accepts a payload for this session", () => {
	assert.equal(
		resolveSubagentsRunning(
			{ parentSessionId: "s1", running: 2, stalled: 0 },
			"s1",
		),
		2,
	);
	assert.equal(
		resolveSubagentsRunning({ running: 1, stalled: 0 }, "s1"),
		1,
	);
});

test("resolveSubagentsRunning ignores a foreign session", () => {
	assert.equal(
		resolveSubagentsRunning(
			{ parentSessionId: "other", running: 3, stalled: 0 },
			"s1",
		),
		undefined,
	);
});

test("resolveSubagentsRunning clamps negatives and rejects junk", () => {
	assert.equal(
		resolveSubagentsRunning({ parentSessionId: "s1", running: -2 }, "s1"),
		0,
	);
	assert.equal(resolveSubagentsRunning({ running: 1 }, undefined), undefined);
	assert.equal(resolveSubagentsRunning(null, "s1"), undefined);
	assert.equal(resolveSubagentsRunning({ running: "2" }, "s1"), undefined);
});

test("resolveBgJobsRunning trusts the payload count on start and stop", () => {
	assert.equal(
		resolveBgJobsRunning({ jobId: "j1", phase: "start", active: 2 }),
		2,
	);
	assert.equal(
		resolveBgJobsRunning({ jobId: "j1", phase: "stop", active: 1 }),
		1,
	);
});

test("resolveBgJobsRunning clamps negatives and rejects unknown phases", () => {
	assert.equal(
		resolveBgJobsRunning({ jobId: "j1", phase: "stop", active: -1 }),
		0,
	);
	assert.equal(
		resolveBgJobsRunning({ jobId: "j1", phase: "pause", active: 1 }),
		undefined,
	);
	assert.equal(resolveBgJobsRunning({ phase: "start" }), undefined);
});

test("isRunningSubagentActivity needs a fresh non-done file", () => {
	const now = Date.parse("2026-10-29T04:00:00.000Z");
	assert.equal(isRunningSubagentActivity({ phase: "active" }, now - 1000, now), true);
	assert.equal(isRunningSubagentActivity({ phase: "waiting" }, now - 1000, now), true);
	assert.equal(isRunningSubagentActivity({ phase: "done" }, now - 1000, now), false);
	assert.equal(
		isRunningSubagentActivity({ phase: "active" }, now - 121_000, now),
		false,
	);
});

test("counter writes and status writes keep every other field", async () => {
	const dir = await mkdtemp(join(tmpdir(), "agent-sesh-counters-"));
	const previous = process.env.XDG_STATE_HOME;
	process.env.XDG_STATE_HOME = dir;
	const registryFile = join(dir, "agent-sesh", "sessions.json");
	await mkdir(dirname(registryFile), { recursive: true });
	const row = {
		id: "s1",
		tmux_target: "%1",
		cwd: "/tmp",
		title: "live",
		status: "working",
		agent: "pi",
		updated_at: "2026-10-29T03:00:00.000Z",
		subagents_running: 2,
		bg_jobs_running: 1,
	} as const;
	await writeFile(
		registryFile,
		JSON.stringify({ version: 1, sessions: [row] }),
		"utf8",
	);
	try {
		await writeRunningCounters("s1", { bg_jobs_running: 3 });
		const afterCounter = JSON.parse(
			await readFile(registryFile, "utf8"),
		) as { sessions: AgentSeshSession[] };
		const counted = afterCounter.sessions[0];
		assert.equal(counted.bg_jobs_running, 3);
		assert.equal(counted.subagents_running, 2);
		assert.equal(counted.status, "working");
		// A counter write must not outrank a newer status row.
		assert.equal(counted.updated_at, "2026-10-29T03:00:00.000Z");

		await writeRunningCounters("s1", { subagents_running: -4 });
		await setStatus("s1", "tool_call", "bash");
		const afterStatus = JSON.parse(
			await readFile(registryFile, "utf8"),
		) as { sessions: AgentSeshSession[] };
		const stored = afterStatus.sessions.find(
			(session) => session.id === "s1",
		);
		assert.equal(stored?.status, "tool_call");
		assert.equal(stored?.tool_name, "bash");
		assert.equal(stored?.subagents_running, 0);
		assert.equal(stored?.bg_jobs_running, 3);
		assert.equal(stored?.title, "live");
		assert.equal(stored?.tmux_target, "%1");
	} finally {
		await rm(dir, { recursive: true, force: true });
		if (previous === undefined) {
			delete process.env.XDG_STATE_HOME;
		} else {
			process.env.XDG_STATE_HOME = previous;
		}
	}
});
