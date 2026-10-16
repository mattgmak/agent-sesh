import assert from "node:assert/strict";
import test from "node:test";
import {
	extractUserPromptText,
	haltedBlocksResume,
	reconcileStatusOnReload,
	resolvePiSessionName,
	resolveUpsertStatus,
	mergeSessions,
	shouldApplyStatusTransition,
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
