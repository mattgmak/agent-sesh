import type {
	AgentSettledEvent,
	AgentStartEvent,
	BeforeAgentStartEvent,
	ExtensionAPI,
	ExtensionContext,
	SessionShutdownEvent,
	SessionStartEvent,
	ToolExecutionEndEvent,
	ToolExecutionStartEvent,
} from "@earendil-works/pi-coding-agent";
import { execFile } from "node:child_process";
import { mkdir, open, readFile, rename, unlink, writeFile } from "node:fs/promises";
import { homedir } from "node:os";
import { basename, dirname, join } from "node:path";
import { promisify } from "node:util";

const execFileAsync = promisify(execFile);

export type AgentSeshStatus =
	| "unknown"
	| "idle"
	| "working"
	| "tool_call"
	| "halted"
	| "awaiting_input";

export interface AgentSeshSession {
	id: string;
	tmux_target: string;
	tmux_session?: string;
	tmux_window?: string;
	tmux_pane?: string;
	cwd: string;
	branch?: string;
	title: string;
	last_prompt?: string;
	last_prompt_at?: string;
	status: AgentSeshStatus;
	tool_name?: string;
	model?: string;
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

const lockPollMs = 25;
const lockTimeoutMs = 5000;

async function withRegistryLock<T>(fn: () => Promise<T>): Promise<T> {
	const lockPath = registryLockPath();
	const deadline = Date.now() + lockTimeoutMs;
	while (Date.now() < deadline) {
		try {
			const handle = await open(lockPath, "wx");
			try {
				return await fn();
			} finally {
				await handle.close();
				await unlink(lockPath).catch(() => undefined);
			}
		} catch (error) {
			if ((error as NodeJS.ErrnoException).code !== "EEXIST") {
				throw error;
			}
			await new Promise((resolve) => setTimeout(resolve, lockPollMs));
		}
	}
	throw new Error(`timed out waiting for registry lock ${lockPath}`);
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

function mergeSessions(
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
	return [...byTarget.values()];
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
		await execFileAsync("tmux", ["refresh-client", "-S"]);
	} catch {
		// best-effort; status bar still updates on status-interval
	}
}

async function writeRegistryUnlocked(file: RegistryFile): Promise<void> {
	const path = registryPath();
	await mkdir(dirname(path), { recursive: true });
	const tmp = `${path}.tmp`;
	const body = `${JSON.stringify(file, null, 2)}\n`;
	await writeFile(tmp, body, "utf8");
	await rename(tmp, path);
	await refreshTmuxStatusBar();
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
}

async function tmuxSessionName(): Promise<string | undefined> {
	if (!process.env.TMUX) {
		return undefined;
	}
	try {
		const { stdout } = await execFileAsync("tmux", [
			"display-message",
			"-p",
			"#{session_name}",
		]);
		const name = stdout.trim();
		return name || undefined;
	} catch {
		return undefined;
	}
}

async function tmuxPaneLocation(): Promise<
	{ window: string; pane: string } | undefined
> {
	if (!process.env.TMUX) {
		return undefined;
	}
	try {
		const { stdout } = await execFileAsync("tmux", [
			"display-message",
			"-p",
			"#{window_index}\t#{pane_index}",
		]);
		const [window, pane] = stdout.trim().split("\t");
		if (!window || !pane) {
			return undefined;
		}
		return { window, pane };
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
			"#{pane_current_path}",
		]);
		const cwd = stdout.trim();
		return cwd || undefined;
	} catch {
		return undefined;
	}
}

async function gitBranch(cwd: string): Promise<string | undefined> {
	try {
		const { stdout } = await execFileAsync("git", [
			"-C",
			cwd,
			"rev-parse",
			"--abbrev-ref",
			"HEAD",
		]);
		const branch = stdout.trim();
		return branch || undefined;
	} catch {
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

function sessionTitle(
	pi: ExtensionAPI,
	ctx: ExtensionContext,
	prompt?: string,
): string {
	const named = pi.getSessionName?.();
	if (named && named.trim().length > 0) {
		return named.trim();
	}
	const trimmedPrompt = prompt?.trim();
	if (trimmedPrompt) {
		return trimmedPrompt.length > 80
			? `${trimmedPrompt.slice(0, 77)}...`
			: trimmedPrompt;
	}
	return basename(ctx.sessionManager.getCwd());
}

function truncateToolName(toolName: string): string {
	return toolName.length > 64 ? `${toolName.slice(0, 61)}...` : toolName;
}

export async function upsertSession(partial: {
	id: string;
	cwd: string;
	title: string;
	last_prompt?: string;
	status?: AgentSeshStatus;
	tool_name?: string | null;
	model?: string;
}): Promise<void> {
	const target = await tmuxTarget();
	if (!target) {
		return;
	}

	const branch = await gitBranch(partial.cwd);
	const tmuxSession = await tmuxSessionName();
	const paneLocation = await tmuxPaneLocation();
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
			last_prompt: partial.last_prompt ?? existing?.last_prompt,
			last_prompt_at: promptUpdated ? now : existing?.last_prompt_at,
			status: partial.status ?? existing?.status ?? "idle",
			tool_name:
				partial.tool_name === null
					? undefined
					: (partial.tool_name ?? existing?.tool_name),
			model: partial.model ?? existing?.model,
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

export async function removeSession(id: string): Promise<void> {
	await updateRegistry((file) =>
		file.sessions.filter((session) => session.id !== id),
	);
}

export async function setStatus(
	id: string,
	status: AgentSeshStatus,
	toolName?: string | null,
): Promise<void> {
	const file = await readRegistry();
	const idx = file.sessions.findIndex((entry) => entry.id === id);
	if (idx < 0) {
		const target = await tmuxTarget();
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
		const currentIdx = current.sessions.findIndex((entry) => entry.id === id);
		if (currentIdx < 0) {
			return current.sessions;
		}
		const session = { ...current.sessions[currentIdx] };
		session.status = status;
		if (toolName === null) {
			delete session.tool_name;
		} else if (toolName !== undefined) {
			session.tool_name = toolName;
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

export default function piAgentSeshExtension(pi: ExtensionAPI): void {
	let sessionId: string | undefined;
	let lastTitle = "pi session";

	function requireSessionId(ctx: ExtensionContext): string | undefined {
		return sessionId ?? ctx.sessionManager.getSessionId();
	}

	pi.on(
		"session_start",
		async (event: SessionStartEvent, ctx: ExtensionContext) => {
			sessionId = ctx.sessionManager.getSessionId();
			if (!sessionId) {
				return;
			}
			lastTitle = sessionTitle(pi, ctx);
			await upsertSession({
				id: sessionId,
				cwd: ctx.sessionManager.getCwd(),
				title: lastTitle,
				model: modelLabel(ctx),
				// Extension reloads must not clobber live working/tool_call status.
				...(event.reason === "reload" ? {} : { status: "idle" }),
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
			const prompt = event.prompt?.trim();
			lastTitle = sessionTitle(pi, ctx, event.prompt);
			await upsertSession({
				id,
				cwd: ctx.sessionManager.getCwd(),
				title: lastTitle,
				last_prompt: prompt || undefined,
				model: modelLabel(ctx),
				status: "working",
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
			await setStatus(id, "working");
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
				status: "halted",
				tool_name: null,
			});
		},
	);

	pi.on(
		"session_shutdown",
		async (_event: SessionShutdownEvent, ctx: ExtensionContext) => {
			const id = requireSessionId(ctx);
			if (!id) {
				return;
			}
			await removeSession(id);
			sessionId = undefined;
		},
	);
}
