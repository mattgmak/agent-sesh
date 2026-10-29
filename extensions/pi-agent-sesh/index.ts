import type {
	AgentSettledEvent,
	AgentStartEvent,
	BeforeAgentStartEvent,
	ExtensionAPI,
	ExtensionContext,
	MessageStartEvent,
	SessionShutdownEvent,
	SessionInfoChangedEvent,
	SessionStartEvent,
	ToolExecutionEndEvent,
	ToolExecutionStartEvent,
} from "@earendil-works/pi-coding-agent";
import { execFile } from "node:child_process";
import { mkdir, readdir, readFile, rename, stat, writeFile } from "node:fs/promises";
import { homedir } from "node:os";
import { basename, dirname, join } from "node:path";
import { promisify } from "node:util";
import { withRegistryFileLock } from "./registry-lock.ts";

const execFileAsync = promisify(execFile);

/** Bound on tmux refresh-client; it must never hold a caller for long. */
const refreshTimeoutMs = 1000;
/** Branch lookups per worktree are reused briefly to skip a git exec per event. */
const branchCacheMs = 5000;
const branchCache = new Map<string, { branch?: string; at: number }>();

/** Cross-extension channels emitted by pi-interactive-subagents and ctx_bg. */
export const CH_SUBAGENTS_RUNNING = "agent-sesh:subagents-running:v1";
export const CH_BG_JOBS_RUNNING = "agent-sesh:background-jobs-running:v1";
/** An activity file older than this belongs to a subagent that already ended. */
export const subagentActivityFreshMs = 120_000;

export type AgentSeshStatus =
	| "unknown"
	| "idle"
	| "working"
	| "tool_call"
	| "halted"
	| "awaiting_input"
	// Display-only states. The picker resolves them from the running counters.
	| "subagents_running"
	| "bg_jobs_running";

export interface AgentSeshSession {
	id: string;
	tmux_target: string;
	tmux_session?: string;
	tmux_window?: string;
	tmux_pane?: string;
	cwd: string;
	branch?: string;
	title: string;
	pi_session_name?: string;
	last_prompt?: string;
	last_prompt_at?: string;
	status: AgentSeshStatus;
	tool_name?: string;
	model?: string;
	subagents_running?: number;
	bg_jobs_running?: number;
	agent: "pi";
	updated_at: string;
}

interface RegistryFile {
	version: 1;
	sessions: AgentSeshSession[];
}

function registryPath(): string {
	const stateHome =
		process.env.XDG_STATE_HOME ?? join(homedir(), ".local", "state");
	return join(stateHome, "agent-sesh", "sessions.json");
}

function registryLockPath(): string {
	return `${registryPath()}.lock`;
}

async function ensureRegistryDir(): Promise<void> {
	await mkdir(dirname(registryPath()), { recursive: true });
}

async function withRegistryLock<T>(fn: () => Promise<T>): Promise<T> {
	await ensureRegistryDir();
	return withRegistryFileLock(registryLockPath(), fn);
}

function sessionUpdatedAfter(
	a: AgentSeshSession,
	b: AgentSeshSession,
): boolean {
	const at = Date.parse(a.updated_at);
	const bt = Date.parse(b.updated_at);
	if (!Number.isNaN(at) && !Number.isNaN(bt)) {
		return at > bt;
	}
	if (!Number.isNaN(at)) {
		return true;
	}
	if (!Number.isNaN(bt)) {
		return false;
	}
	return a.id > b.id;
}

const activeStatuses: ReadonlySet<AgentSeshStatus> = new Set([
	"working",
	"tool_call",
	"awaiting_input",
	"unknown",
]);

/** Reconcile registry status when pi reloads the extension. */
export function reconcileStatusOnReload(
	isIdle: boolean,
	currentStatus?: AgentSeshStatus,
): { status?: AgentSeshStatus; tool_name?: string | null } {
	if (!isIdle) {
		return {};
	}
	if (!currentStatus || !activeStatuses.has(currentStatus)) {
		return {};
	}
	return { status: "halted", tool_name: null };
}

export interface StatusTransitionOptions {
	/** Allow halted → working when a new agent run is starting. */
	resumeFromHalt?: boolean;
	/** Only resume when the halt predates this epoch ms (agent_start time). */
	resumeIfHaltedBefore?: number;
}

/** Skip stale active-status writes after agent_settled left the pane halted. */
export function shouldApplyStatusTransition(
	currentStatus: AgentSeshStatus,
	nextStatus: AgentSeshStatus,
	opts?: StatusTransitionOptions,
): boolean {
	if (currentStatus !== "halted") {
		return true;
	}
	if (nextStatus === "working") {
		return opts?.resumeFromHalt === true;
	}
	return nextStatus === "halted" || nextStatus === "idle";
}

/** Resolve registry status for upsertSession, honoring halted guards. */
export function resolveUpsertStatus(
	existing: AgentSeshSession | undefined,
	partialStatus: AgentSeshStatus | undefined,
	opts?: StatusTransitionOptions,
): AgentSeshStatus {
	const fallback = existing?.status ?? "idle";
	if (partialStatus === undefined) {
		return fallback;
	}
	// agent_settled must always win over in-flight active-status writes.
	if (partialStatus === "halted") {
		return "halted";
	}
	const current = existing?.status ?? "idle";
	if (!shouldApplyStatusTransition(current, partialStatus, opts)) {
		return current;
	}
	return partialStatus;
}

/** Text from a user message once it enters the agent loop. */
export function extractUserPromptText(message: {
	role: string;
	content?: unknown;
}): string | undefined {
	if (message.role !== "user") {
		return undefined;
	}
	const { content } = message;
	if (typeof content === "string") {
		const trimmed = content.trim();
		return trimmed || undefined;
	}
	if (!Array.isArray(content)) {
		return undefined;
	}
	const parts: string[] = [];
	for (const part of content) {
		if (
			part &&
			typeof part === "object" &&
			"type" in part &&
			part.type === "text" &&
			"text" in part &&
			typeof part.text === "string"
		) {
			const text = part.text.trim();
			if (text) {
				parts.push(text);
			}
		}
	}
	const combined = parts.join("\n").trim();
	return combined || undefined;
}

export function haltedBlocksResume(
	session: AgentSeshSession,
	status: AgentSeshStatus,
	opts?: StatusTransitionOptions,
): boolean {
	if (session.status !== "halted" || status !== "working") {
		return false;
	}
	if (!opts?.resumeFromHalt) {
		return true;
	}
	if (opts.resumeIfHaltedBefore === undefined) {
		return false;
	}
	const haltedAt = Date.parse(session.updated_at);
	if (Number.isNaN(haltedAt)) {
		return false;
	}
	return haltedAt >= opts.resumeIfHaltedBefore;
}

export function mergeSessions(
	...lists: AgentSeshSession[][]
): AgentSeshSession[] {
	const byTarget = new Map<string, AgentSeshSession>();
	for (const list of lists) {
		for (const session of list) {
			const target = session.tmux_target.trim();
			if (!target) {
				continue;
			}
			const existing = byTarget.get(target);
			if (!existing || sessionUpdatedAfter(session, existing)) {
				byTarget.set(target, session);
			}
		}
	}

	// One row per pi session. A session registered from a pane it no longer runs
	// in would otherwise leave its previous pane's row behind, so the picker
	// renders the same named session twice and one entry targets the wrong pane.
	const byID = new Map<string, AgentSeshSession>();
	const anonymous: AgentSeshSession[] = [];
	for (const session of byTarget.values()) {
		const id = session.id.trim();
		if (!id) {
			anonymous.push(session);
			continue;
		}
		const existing = byID.get(id);
		if (!existing || !sessionUpdatedAfter(existing, session)) {
			byID.set(id, session);
		}
	}
	return [...byID.values(), ...anonymous];
}

async function readRegistry(): Promise<RegistryFile> {
	try {
		const raw = await readFile(registryPath(), "utf8");
		return JSON.parse(raw) as RegistryFile;
	} catch {
		return { version: 1, sessions: [] };
	}
}

async function refreshTmuxStatusBar(): Promise<void> {
	if (!process.env.TMUX) {
		return;
	}
	try {
		// Bounded: a hung tmux server must not stall the caller.
		await execFileAsync("tmux", ["refresh-client", "-S"], {
			timeout: refreshTimeoutMs,
		});
	} catch {
		// best-effort; status bar still updates on status-interval
	}
}

async function writeRegistryUnlocked(file: RegistryFile): Promise<void> {
	const path = registryPath();
	const tmp = `${path}.tmp`;
	const body = `${JSON.stringify(file, null, 2)}\n`;
	await writeFile(tmp, body, "utf8");
	await rename(tmp, path);
}

async function updateRegistry(
	mutate: (file: RegistryFile) => AgentSeshSession[],
): Promise<void> {
	await withRegistryLock(async () => {
		const latest = await readRegistry();
		const nextSessions = mutate(latest);
		const merged = mergeSessions(latest.sessions, nextSessions);
		await writeRegistryUnlocked({ version: 1, sessions: merged });
	});
	// Outside the flock: peers poll the lock, so a slow tmux exec here would
	// stall every other pi session for up to the lock timeout.
	await refreshTmuxStatusBar();
}

// display-message without -t resolves against the attached client's current
// pane, not the pane issuing the command, so a background pane registers the
// pane the user happens to be looking at. -t pins the query to our own pane.
function tmuxPaneArgs(): string[] {
	const pane = process.env.TMUX_PANE?.trim();
	return pane ? ["-t", pane] : [];
}

interface PaneFacts {
	target: string;
	session?: string;
	window?: string;
	pane?: string;
}

// tmuxPaneFacts resolves pane id, session name and window/pane indexes in one
// display-message call instead of three serial ones.
async function tmuxPaneFacts(): Promise<PaneFacts | undefined> {
	if (!process.env.TMUX) {
		return undefined;
	}
	try {
		const { stdout } = await execFileAsync("tmux", [
			"display-message",
			"-p",
			...tmuxPaneArgs(),
			"#{pane_id}\t#{session_name}\t#{window_index}\t#{pane_index}",
		]);
		const [target, session, window, pane] = stdout.trim().split("\t");
		if (!target) {
			return undefined;
		}
		return {
			target,
			session: session || undefined,
			window: window || undefined,
			pane: pane || undefined,
		};
	} catch {
		return undefined;
	}
}

async function tmuxTarget(): Promise<string | null> {
	if (!process.env.TMUX) {
		return null;
	}
	try {
		const { stdout } = await execFileAsync("tmux", [
			"display-message",
			"-p",
			...tmuxPaneArgs(),
			"#{pane_id}",
		]);
		return stdout.trim() || null;
	} catch {
		return null;
	}
}

async function tmuxPaneCwd(): Promise<string | undefined> {
	if (!process.env.TMUX) {
		return undefined;
	}
	try {
		const { stdout } = await execFileAsync("tmux", [
			"display-message",
			"-p",
			...tmuxPaneArgs(),
			"#{pane_current_path}",
		]);
		const cwd = stdout.trim();
		return cwd || undefined;
	} catch {
		return undefined;
	}
}

async function gitBranch(cwd: string): Promise<string | undefined> {
	const key = cwd.trim();
	if (!key) {
		return undefined;
	}
	const cached = branchCache.get(key);
	if (cached && Date.now() - cached.at < branchCacheMs) {
		return cached.branch;
	}
	try {
		const { stdout } = await execFileAsync("git", [
			"-C",
			key,
			"rev-parse",
			"--abbrev-ref",
			"HEAD",
		]);
		const branch = stdout.trim() || undefined;
		branchCache.set(key, { branch, at: Date.now() });
		return branch;
	} catch {
		branchCache.set(key, { branch: undefined, at: Date.now() });
		return undefined;
	}
}

function modelLabel(ctx: ExtensionContext): string | undefined {
	const model = ctx.model;
	if (!model) {
		return undefined;
	}
	if (model.name) {
		return model.name;
	}
	if (model.provider && model.id) {
		return `${model.provider}/${model.id}`;
	}
	return model.id;
}

function piSessionName(pi: ExtensionAPI): string | undefined {
	const name = pi.getSessionName?.();
	const trimmed = name?.trim();
	return trimmed || undefined;
}

/** `undefined` keeps the stored name, `null` (cleared) drops it. */
export function resolvePiSessionName(
	next: string | null | undefined,
	existing?: string,
): string | undefined {
	const trimmed = (next === undefined ? existing : next)?.trim();
	return trimmed || undefined;
}

function sessionTitle(
	pi: ExtensionAPI,
	ctx: ExtensionContext,
	prompt?: string,
): string {
	const named = piSessionName(pi);
	if (named) {
		return named;
	}
	const trimmedPrompt = prompt?.trim();
	if (trimmedPrompt) {
		return trimmedPrompt.length > 80
			? `${trimmedPrompt.slice(0, 77)}...`
			: trimmedPrompt;
	}
	return basename(ctx.sessionManager.getCwd());
}

function normalizeToolName(
	toolName: string | null | undefined,
): string | undefined {
	if (toolName === null || toolName === undefined) {
		return undefined;
	}
	const trimmed = toolName.trim();
	return trimmed || undefined;
}

function truncateToolName(toolName: string): string {
	return toolName.length > 64 ? `${toolName.slice(0, 61)}...` : toolName;
}

function payloadRecord(payload: unknown): Record<string, unknown> | undefined {
	return payload && typeof payload === "object" && !Array.isArray(payload)
		? (payload as Record<string, unknown>)
		: undefined;
}

function runningCount(value: unknown): number | undefined {
	if (typeof value !== "number" || !Number.isFinite(value)) {
		return undefined;
	}
	return Math.max(0, Math.trunc(value));
}

/**
 * Count for the subagents channel. Undefined means ignore, because the payload
 * is malformed or belongs to another pi session.
 */
export function resolveSubagentsRunning(
	payload: unknown,
	sessionId: string | undefined,
): number | undefined {
	if (!sessionId) {
		return undefined;
	}
	const data = payloadRecord(payload);
	if (!data) {
		return undefined;
	}
	const parent = data.parentSessionId;
	if (typeof parent === "string" && parent.trim() && parent !== sessionId) {
		return undefined;
	}
	return runningCount(data.running);
}

/** Trust the payload count, so a restart or a missed stop self-corrects. */
export function resolveBgJobsRunning(payload: unknown): number | undefined {
	const data = payloadRecord(payload);
	if (!data || (data.phase !== "start" && data.phase !== "stop")) {
		return undefined;
	}
	return runningCount(data.active);
}

/** Fresh activity file that is not in its terminal phase. */
export function isRunningSubagentActivity(
	raw: unknown,
	mtimeMs: number,
	nowMs: number,
): boolean {
	const data = payloadRecord(raw);
	if (!data || data.phase === "done") {
		return false;
	}
	return nowMs - mtimeMs <= subagentActivityFreshMs;
}

export async function upsertSession(partial: {
	id: string;
	cwd: string;
	title: string;
	last_prompt?: string;
	status?: AgentSeshStatus;
	tool_name?: string | null;
	model?: string;
	pi_session_name?: string | null;
}): Promise<void> {
	const facts = await tmuxPaneFacts();
	if (!facts) {
		return;
	}
	const target = facts.target;

	const branch = await gitBranch(partial.cwd);
	const tmuxSession = facts.session;
	const paneLocation = facts.window && facts.pane ? facts : undefined;
	const now = new Date().toISOString();

	await updateRegistry((file) => {
		const existing = file.sessions.find((session) => session.id === partial.id);
		const promptUpdated = partial.last_prompt !== undefined;
		const next: AgentSeshSession = {
			id: partial.id,
			tmux_target: target,
			tmux_session: tmuxSession ?? existing?.tmux_session,
			tmux_window: paneLocation?.window ?? existing?.tmux_window,
			tmux_pane: paneLocation?.pane ?? existing?.tmux_pane,
			cwd: partial.cwd,
			branch,
			title: partial.title,
			pi_session_name: resolvePiSessionName(
				partial.pi_session_name,
				existing?.pi_session_name,
			),
			last_prompt: partial.last_prompt ?? existing?.last_prompt,
			last_prompt_at: promptUpdated ? now : existing?.last_prompt_at,
			status: resolveUpsertStatus(existing, partial.status),
			tool_name:
				partial.tool_name === null
					? undefined
					: normalizeToolName(partial.tool_name ?? existing?.tool_name),
			model: partial.model ?? existing?.model,
			subagents_running: existing?.subagents_running,
			bg_jobs_running: existing?.bg_jobs_running,
			agent: "pi",
			updated_at: now,
		};

		return [
			next,
			...file.sessions.filter(
				(session) =>
					session.id !== partial.id && session.tmux_target !== target,
			),
		];
	});
}

export async function removeSession(
	id?: string,
	tmuxPaneTarget?: string | null,
): Promise<void> {
	const paneTarget = tmuxPaneTarget?.trim();
	if (!id && !paneTarget) {
		return;
	}

	let removed = false;
	await withRegistryLock(async () => {
		const latest = await readRegistry();
		const nextSessions = latest.sessions.filter((session) => {
			if (id && session.id === id) {
				return false;
			}
			if (paneTarget && session.tmux_target === paneTarget) {
				return false;
			}
			return true;
		});
		if (nextSessions.length === latest.sessions.length) {
			return;
		}
		await writeRegistryUnlocked({ version: 1, sessions: nextSessions });
		removed = true;
	});
	if (removed) {
		await refreshTmuxStatusBar();
	}
}

function findSessionIndex(
	sessions: AgentSeshSession[],
	id: string,
	tmuxTarget?: string | null,
): number {
	if (tmuxTarget) {
		const byTarget = sessions.findIndex(
			(entry) => entry.tmux_target === tmuxTarget,
		);
		if (byTarget >= 0) {
			return byTarget;
		}
	}
	return sessions.findIndex((entry) => entry.id === id);
}

export async function setStatus(
	id: string,
	status: AgentSeshStatus,
	toolName?: string | null,
	opts?: StatusTransitionOptions,
): Promise<void> {
	const target = await tmuxTarget();
	const file = await readRegistry();
	const idx = findSessionIndex(file.sessions, id, target);
	if (idx < 0) {
		const cwd = await tmuxPaneCwd();
		if (!target || !cwd) {
			return;
		}
		await upsertSession({
			id,
			cwd,
			title: basename(cwd),
			status,
			tool_name: toolName ?? null,
		});
		return;
	}

	await updateRegistry((current) => {
		const currentIdx = findSessionIndex(current.sessions, id, target);
		if (currentIdx < 0) {
			return current.sessions;
		}
		const session = { ...current.sessions[currentIdx] };
		if (
			haltedBlocksResume(session, status, opts) ||
			!shouldApplyStatusTransition(session.status, status, opts)
		) {
			return current.sessions;
		}
		session.status = status;
		if (toolName === null) {
			delete session.tool_name;
		} else if (toolName !== undefined) {
			const normalized = normalizeToolName(toolName);
			if (normalized) {
				session.tool_name = normalized;
			} else {
				delete session.tool_name;
			}
		} else if (status !== "tool_call") {
			delete session.tool_name;
		}
		session.updated_at = new Date().toISOString();
		return [
			session,
			...current.sessions.filter((_, i) => i !== currentIdx),
		];
	});
}

export interface RunningCounters {
	subagents_running?: number;
	bg_jobs_running?: number;
}

/**
 * Write only the two running counters. Every other field, including
 * updated_at, stays as stored, so a counter write never outranks a newer
 * status row in mergeSessions.
 */
export async function writeRunningCounters(
	id: string,
	patch: RunningCounters,
): Promise<void> {
	const subagents = runningCount(patch.subagents_running);
	const bg = runningCount(patch.bg_jobs_running);
	if (subagents === undefined && bg === undefined) {
		return;
	}
	let wrote = false;
	await withRegistryLock(async () => {
		const latest = await readRegistry();
		const idx = latest.sessions.findIndex((session) => session.id === id);
		if (idx < 0) {
			return;
		}
		const stored = latest.sessions[idx];
		const next: AgentSeshSession = {
			...stored,
			subagents_running: subagents ?? stored.subagents_running,
			bg_jobs_running: bg ?? stored.bg_jobs_running,
		};
		if (
			next.subagents_running === stored.subagents_running &&
			next.bg_jobs_running === stored.bg_jobs_running
		) {
			return;
		}
		await writeRegistryUnlocked({
			version: 1,
			sessions: [next, ...latest.sessions.filter((_, i) => i !== idx)],
		});
		wrote = true;
	});
	if (wrote) {
		await refreshTmuxStatusBar();
	}
}

/** Count running subagents from their activity files. Undefined means the
 * directory is unreadable, so the stored counter stays. */
async function countRunningSubagents(
	sessionDir: string,
	sessionId: string,
	nowMs: number,
): Promise<number | undefined> {
	const dir = join(sessionDir, "artifacts", sessionId, "subagent-activity");
	let names: string[];
	try {
		names = await readdir(dir);
	} catch {
		return undefined;
	}
	let count = 0;
	for (const name of names) {
		if (!name.endsWith(".json")) {
			continue;
		}
		const file = join(dir, name);
		try {
			const [raw, stats] = await Promise.all([
				readFile(file, "utf8"),
				stat(file),
			]);
			if (isRunningSubagentActivity(JSON.parse(raw), stats.mtimeMs, nowMs)) {
				count++;
			}
		} catch {
			continue;
		}
	}
	return count;
}

/**
 * Counters to write when a session starts. The process is new, so it owns zero
 * background jobs. That zero is a fact. A missing subagent seed keeps the
 * stored value.
 */
export function sessionStartCounters(
	subagentSeed: number | undefined,
): RunningCounters {
	const patch: RunningCounters = { bg_jobs_running: 0 };
	const seed = runningCount(subagentSeed);
	if (seed !== undefined) {
		patch.subagents_running = seed;
	}
	return patch;
}

export default function piAgentSeshExtension(pi: ExtensionAPI): void {
	let sessionId: string | undefined;
	let lastTitle = "pi session";
	let currentCtx: ExtensionContext | undefined;
	const unsubscribers: Array<() => void> = [];

	// No ctx.isIdle() gate on the counter writes: a stop event lands after the
	// agent settled, and the count must still fall.
	async function writeSubagentCount(payload: unknown): Promise<void> {
		const id = currentCtx?.sessionManager.getSessionId();
		const running = resolveSubagentsRunning(payload, id);
		if (!id || running === undefined) {
			return;
		}
		await writeRunningCounters(id, { subagents_running: running });
	}

	async function writeBgCount(payload: unknown): Promise<void> {
		const id = currentCtx?.sessionManager.getSessionId();
		const pending = resolveBgJobsRunning(payload);
		if (!id || pending === undefined) {
			return;
		}
		await writeRunningCounters(id, { bg_jobs_running: pending });
	}

	// A session_shutdown without a reload would otherwise leave the counters
	// dead, so every start re-subscribes when nothing is registered.
	function subscribeChannelEvents(): void {
		if (unsubscribers.length > 0) {
			return;
		}
		const events = pi.events;
		if (!events || typeof events.on !== "function") {
			return;
		}
		unsubscribers.push(
			events.on(CH_SUBAGENTS_RUNNING, (payload) => {
				writeSubagentCount(payload).catch(() => {});
			}),
			events.on(CH_BG_JOBS_RUNNING, (payload) => {
				writeBgCount(payload).catch(() => {});
			}),
		);
	}

	function unsubscribeChannelEvents(): void {
		for (const off of unsubscribers.splice(0)) {
			try {
				off();
			} catch {
				// a dead bus must not block shutdown
			}
		}
	}

	function requireSessionId(ctx: ExtensionContext): string | undefined {
		return sessionId ?? ctx.sessionManager.getSessionId();
	}

	pi.on(
		"session_start",
		async (event: SessionStartEvent, ctx: ExtensionContext) => {
			currentCtx = ctx;
			subscribeChannelEvents();
			sessionId = ctx.sessionManager.getSessionId();
			if (!sessionId) {
				return;
			}
			lastTitle = sessionTitle(pi, ctx);
			const target = await tmuxTarget();
			const file = await readRegistry();
			const idx = findSessionIndex(file.sessions, sessionId, target);
			const currentStatus =
				idx >= 0 ? file.sessions[idx].status : undefined;
			const statusPatch =
				event.reason === "reload"
					? reconcileStatusOnReload(ctx.isIdle(), currentStatus)
					: { status: "idle" as const };
			await upsertSession({
				id: sessionId,
				cwd: ctx.sessionManager.getCwd(),
				title: lastTitle,
				model: modelLabel(ctx),
				pi_session_name: piSessionName(pi),
				...statusPatch,
			});
			// Seed both counters. Subagents rebuild from activity files. Background
			// jobs die with the old process, so the start value is zero.
			let subagentSeed: number | undefined;
			try {
				const sessionDir = ctx.sessionManager.getSessionDir();
				subagentSeed = await countRunningSubagents(
					sessionDir,
					sessionId,
					Date.now(),
				);
			} catch {
				// older pi builds lack getSessionDir; channel events self-correct
			}
			await writeRunningCounters(
				sessionId,
				sessionStartCounters(subagentSeed),
			);
		},
	);

	pi.on(
		"session_info_changed",
		async (event: SessionInfoChangedEvent, ctx: ExtensionContext) => {
			const id = requireSessionId(ctx);
			if (!id) {
				return;
			}
			await upsertSession({
				id,
				cwd: ctx.sessionManager.getCwd(),
				title: lastTitle,
				model: modelLabel(ctx),
				pi_session_name: event.name ?? null,
			});
		},
	);

	pi.on(
		"before_agent_start",
		async (event: BeforeAgentStartEvent, ctx: ExtensionContext) => {
			const id = requireSessionId(ctx);
			if (!id) {
				return;
			}
			// pi >= 1.2 fires this for custom-message runs too; those carry no prompt.
			const prompt = "prompt" in event ? event.prompt : undefined;
			if (!prompt) {
				return;
			}
			lastTitle = sessionTitle(pi, ctx, prompt);
			await upsertSession({
				id,
				cwd: ctx.sessionManager.getCwd(),
				title: lastTitle,
				model: modelLabel(ctx),
				pi_session_name: piSessionName(pi),
			});
		},
	);

	pi.on(
		"message_start",
		async (event: MessageStartEvent, ctx: ExtensionContext) => {
			const id = requireSessionId(ctx);
			const prompt = extractUserPromptText(event.message);
			if (!id || !prompt) {
				return;
			}
			await upsertSession({
				id,
				cwd: ctx.sessionManager.getCwd(),
				title: lastTitle,
				last_prompt: prompt,
				model: modelLabel(ctx),
				pi_session_name: piSessionName(pi),
			});
		},
	);

	pi.on(
		"tool_execution_end",
		async (_event: ToolExecutionEndEvent, ctx: ExtensionContext) => {
			const id = requireSessionId(ctx);
			if (!id) {
				return;
			}
			// Parallel tools + async registry writes: a late tool_execution_end can
			// finish after agent_settled and overwrite halted with working.
			if (ctx.isIdle()) {
				return;
			}
			await setStatus(id, "working");
		},
	);

	pi.on(
		"tool_execution_start",
		async (event: ToolExecutionStartEvent, ctx: ExtensionContext) => {
			const id = requireSessionId(ctx);
			if (!id) {
				return;
			}
			// Late tool_execution_start can finish after agent_settled.
			if (ctx.isIdle()) {
				return;
			}
			if (event.toolName === "ask_user_question") {
				await setStatus(id, "awaiting_input");
				return;
			}
			if (!event.toolName) {
				return;
			}
			await setStatus(id, "tool_call", truncateToolName(event.toolName));
		},
	);

	pi.on(
		"agent_start",
		async (_event: AgentStartEvent, ctx: ExtensionContext) => {
			const id = requireSessionId(ctx);
			if (!id) {
				return;
			}
			// agent_start handlers can finish after agent_settled; do not revive working.
			if (ctx.isIdle()) {
				return;
			}
			const startedAt = Date.now();
			await setStatus(id, "working", undefined, {
				resumeFromHalt: true,
				resumeIfHaltedBefore: startedAt,
			});
		},
	);

	pi.on(
		"agent_settled",
		async (_event: AgentSettledEvent, ctx: ExtensionContext) => {
			const id = requireSessionId(ctx);
			if (!id) {
				return;
			}
			await upsertSession({
				id,
				cwd: ctx.sessionManager.getCwd(),
				title: lastTitle,
				model: modelLabel(ctx),
				pi_session_name: piSessionName(pi),
				status: "halted",
				tool_name: null,
			});
		},
	);

	pi.on(
		"session_shutdown",
		async (_event: SessionShutdownEvent, ctx: ExtensionContext) => {
			unsubscribeChannelEvents();
			currentCtx = undefined;
			const id = requireSessionId(ctx);
			const target = await tmuxTarget();
			if (!id && !target) {
				return;
			}
			await removeSession(id, target);
			sessionId = undefined;
		},
	);

	subscribeChannelEvents();
}
