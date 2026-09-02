import assert from "node:assert/strict";
import test from "node:test";
import {
	extractUserPromptText,
	reconcileStatusOnReload,
	shouldApplyStatusTransition,
} from "./index.ts";

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

test("shouldApplyStatusTransition blocks stale writes over halted", () => {
	assert.equal(shouldApplyStatusTransition("halted", "working"), false);
	assert.equal(shouldApplyStatusTransition("halted", "tool_call"), false);
	assert.equal(shouldApplyStatusTransition("halted", "awaiting_input"), false);
});

test("shouldApplyStatusTransition allows halted to clear or acknowledge", () => {
	assert.equal(shouldApplyStatusTransition("halted", "halted"), true);
	assert.equal(shouldApplyStatusTransition("halted", "idle"), true);
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
