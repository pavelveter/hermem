import { Plugin } from "@opencode/plugin"
import { execFileSync } from "node:child_process"
import { createHash } from "node:crypto"
import { appendFileSync, mkdirSync, readFileSync, readdirSync, rmSync, statSync, writeFileSync } from "node:fs"
import { homedir } from "node:os"
import { dirname, join } from "node:path"

// hermem-habits — keeps a durable profile of HOW this developer works in
// hermem and feeds it back into every session.
//
// Capture: on session.idle, one small extraction call reads the new part of the
// transcript and stores only durable preferences ("use uv, not pip") as hermem
// memories with a `habit:` id and category `opinion`.
// Use: on session.created the stored habits are attached to the session as the
// InstructionEntry `hermem-habits`, so they render in every project without
// touching AGENTS.md.
//
// hermem is driven through its CLI, not through MCP: a plugin cannot call MCP
// tools, and the CLI is the write path that embeds content (the vector index is
// process-local, so a fresh CLI invocation always reads the current database).
//
// Environment overrides:
//   HERMEM_BIN               hermem binary (default ~/.local/bin/hermem)
//   HERMEM_HABITS_MODEL      provider/model for extraction (default: session model)
//   HERMEM_HABITS_DISABLED   set to 1/true/yes/off to turn capture and injection off
//   HERMEM_HABITS_LOG        log file (default ~/.local/state/opencode/hermem-habits.log)

const HERMEM = process.env.HERMEM_BIN ?? join(homedir(), ".local", "bin", "hermem")
const DISABLED = /^(1|true|yes|off)$/i.test(process.env.HERMEM_HABITS_DISABLED ?? "")
const MODEL = (process.env.HERMEM_HABITS_MODEL ?? "").trim()
const LOG = process.env.HERMEM_HABITS_LOG ?? join(homedir(), ".local", "state", "opencode", "hermem-habits.log")

const INSTRUCTION_KEY = "hermem-habits"
const HABIT_PREFIX = "habit:"
const HABIT_CATEGORY = "opinion"
// Fixed query keeps the habit set stable between sessions; habits are then
// filtered by their id prefix.
const SEARCH_QUERY = "user's coding preferences, tooling choices and working habits"
// ponytail: the habit list is a top-K vector query filtered by id prefix, so a
// knowledge base larger than this window can hide the least similar habits.
// The exact fix is a prefix listing command in hermem's CLI.
const SEARCH_TOP_K = 200
const MAX_INJECTED = 30
const MAX_HABIT_CHARS = 240
const MAX_MESSAGE_CHARS = 2000
const MAX_DIGEST_CHARS = 12000
const MIN_DIGEST_CHARS = 400
const DEDUP_SIMILARITY = 0.92
const COOLDOWN_MS = 20000
const HERMEM_TIMEOUT_MS = 30000
const CLAIM_DIR = join(dirname(LOG), "hermem-habits-claims")
const CLAIM_STALE_MS = 10 * 60 * 1000
const CLAIM_KEEP_MS = 7 * 24 * 60 * 60 * 1000

type Habit = { id: string; content: string; similarity: number }

// The server keeps several live plugin instances for one location, and a
// turn-ending event reaches every one of them. In-process state is per
// instance, so a capture is claimed through the filesystem: exactly one
// instance pays for the extraction of a given transcript.
function claimCapture(key: string): boolean {
  try {
    mkdirSync(CLAIM_DIR, { recursive: true })
  } catch {
    return true // fail open: storing twice beats never storing
  }
  const file = join(CLAIM_DIR, key)
  try {
    writeFileSync(file, `claimed:${Date.now()}`, { flag: "wx" })
    return true
  } catch (error: any) {
    if (error?.code !== "EEXIST") return true
  }
  let content = ""
  try { content = readFileSync(file, "utf8") } catch { return true }
  const failed = content.startsWith("failed:")
  const staleMs = failed ? 24 * 60 * 60 * 1000 : CLAIM_STALE_MS
  const ts = Number(content.split(":")[1] ?? 0)
  if (Date.now() - ts < staleMs) return false
  // A crashed capture must not block this transcript forever.
  try { writeFileSync(file, `claimed:${Date.now()}`) } catch { return false }
  return true
}

function pruneClaims() {
  try {
    for (const name of readdirSync(CLAIM_DIR)) {
      const path = join(CLAIM_DIR, name)
      if (Date.now() - statSync(path).mtimeMs > CLAIM_KEEP_MS) rmSync(path, { force: true })
    }
  } catch {
    // no claims yet
  }
}

const capturing = new Set<string>()
const published = new Set<string>()
const lastCapture = new Map<string, number>()

function log(kind: string, fields: Record<string, unknown> = {}) {
  try {
    appendFileSync(LOG, `${new Date().toISOString()} ${kind} ${JSON.stringify(fields)}\n`)
  } catch {
    // logging must never break a session
  }
}

function clip(text: string, max: number): string {
  return text.length > max ? text.slice(0, max) : text
}

function sha1(text: string): string {
  return createHash("sha1").update(text).digest("hex")
}

function normalize(text: string): string {
  return text.trim().replace(/\s+/g, " ").replace(/\.$/, "").toLowerCase()
}

// hermem <args> with an optional JSON payload on stdin; returns parsed stdout.
function hermem(args: string[], payload?: unknown): unknown {
  const out = execFileSync(HERMEM, args, {
    input: payload === undefined ? "" : JSON.stringify(payload),
    encoding: "utf8",
    timeout: HERMEM_TIMEOUT_MS,
    stdio: ["pipe", "pipe", "pipe"],
  })
  const trimmed = String(out).trim()
  return trimmed ? JSON.parse(trimmed) : null
}

function search(query: string, topK: number): Array<{ entity?: { id?: string; content?: string }; similarity?: number }> {
  const rows = hermem(["memory", "search"], { query, top_k: topK })
  return Array.isArray(rows) ? (rows as any[]) : []
}

function listHabits(): Habit[] {
  const habits: Habit[] = []
  for (const row of search(SEARCH_QUERY, SEARCH_TOP_K)) {
    const id = String(row?.entity?.id ?? "")
    const content = String(row?.entity?.content ?? "").trim()
    if (!id.startsWith(HABIT_PREFIX) || !content) continue
    habits.push({ id, content, similarity: Number(row?.similarity ?? 0) })
  }
  habits.sort((a, b) => b.similarity - a.similarity)
  return habits
}

function similarHabitExists(content: string): boolean {
  const needle = normalize(content)
  for (const row of search(content, 3)) {
    const id = String(row?.entity?.id ?? "")
    if (!id.startsWith(HABIT_PREFIX)) continue
    if (normalize(String(row?.entity?.content ?? "")) === needle) return true
    if (Number(row?.similarity ?? 0) >= DEDUP_SIMILARITY) return true
  }
  return false
}

function storeHabit(content: string): string {
  const id = HABIT_PREFIX + sha1(normalize(content)).slice(0, 16)
  hermem(["memory", "store"], { id, category: HABIT_CATEGORY, content })
  return id
}

function habitBlock(habits: Habit[]): string {
  if (habits.length === 0) return ""
  const lines = habits.slice(0, MAX_INJECTED).map((habit) => `- ${habit.content}`)
  return [
    "User's durable working preferences, captured by hermem from earlier sessions:",
    ...lines,
    "Apply them unless the user says otherwise in this session.",
  ].join("\n")
}

// Keep only the human-readable part of the transcript: tool traffic and
// reasoning say nothing about durable preferences and would drown the prompt.
function buildDigest(messages: any[]): string {
  const lines: string[] = []
  for (const message of messages) {
    if (message?.type === "user") {
      const text = String(message.text ?? "").trim()
      if (text) lines.push(`USER: ${clip(text, MAX_MESSAGE_CHARS)}`)
      continue
    }
    if (message?.type !== "assistant") continue
    const parts = Array.isArray(message.content) ? message.content : []
    const text = parts
      .filter((part: any) => part?.type === "text")
      .map((part: any) => String(part.text ?? ""))
      .join("\n")
      .trim()
    if (text) lines.push(`ASSISTANT: ${clip(text, MAX_MESSAGE_CHARS)}`)
  }
  const tail = lines.slice(-40).join("\n")
  return tail.length > MAX_DIGEST_CHARS ? tail.slice(-MAX_DIGEST_CHARS) : tail
}

function buildPrompt(digest: string, existing: Habit[]): string {
  const known = existing.length > 0 ? existing.map((habit) => `- ${habit.content}`).join("\n") : "(none)"
  return [
    "You maintain a durable profile of a software developer's working preferences.",
    "",
    "From the transcript below, extract ONLY durable, reusable facts about HOW this",
    "person works: tool and library choices, environment setup, workflow conventions,",
    "code style, review and verification habits, communication preferences.",
    "Ignore anything that belongs to the current task, repository, bug or one-off request.",
    "",
    "Answer with a JSON array of strings and nothing else. Each string is one short",
    "imperative rule for an AI coding assistant, written in the language of the user's",
    'messages, for example: "Use uv instead of pip for Python dependencies and environments."',
    "",
    "Rules:",
    "- Only what the USER stated or clearly demonstrated. Never the assistant's suggestions.",
    "- Skip anything already covered by the existing rules, including paraphrases.",
    "- Return [] when the transcript carries nothing durable.",
    "",
    "Existing rules:",
    known,
    "",
    "Transcript:",
    digest,
  ].join("\n")
}

function parseHabits(text: string): string[] {
  const start = text.indexOf("[")
  const end = text.lastIndexOf("]")
  if (start === -1 || end <= start) return []
  let parsed: unknown
  try {
    parsed = JSON.parse(text.slice(start, end + 1))
  } catch {
    return []
  }
  if (!Array.isArray(parsed)) return []
  const seen = new Set<string>()
  const habits: string[] = []
  for (const item of parsed) {
    if (typeof item !== "string") continue
    const content = item.trim().replace(/\s+/g, " ")
    if (!content || content.length > MAX_HABIT_CHARS) continue
    const key = normalize(content)
    if (seen.has(key)) continue
    seen.add(key)
    habits.push(content)
  }
  return habits
}

function parseModelRef(value: string): { providerID: string; id: string } | undefined {
  const slash = value.indexOf("/")
  if (slash <= 0 || slash === value.length - 1) return undefined
  return { providerID: value.slice(0, slash), id: value.slice(slash + 1) }
}

async function extractRules(ctx: any, sessionID: string, prompt: string): Promise<string> {
  let model: { providerID: string; id: string } | undefined = MODEL ? parseModelRef(MODEL) : undefined
  if (!model) {
    const info = await ctx.session.get({ sessionID })
    const selected = info?.model
    if (selected?.providerID && selected?.id) model = { providerID: selected.providerID, id: selected.id }
  }
  // A session-less generate keeps this call out of the transcript (and so out
  // of the idle loop); it is experimental, so fall back to session.generate.
  const experimental = ctx.experimental?.generate
  if (model && typeof experimental?.text === "function") {
    const result = await experimental.text({ model, prompt })
    if (result?.text) return String(result.text)
  }
  const result = await ctx.session.generate({ sessionID, prompt })
  return String(result?.text ?? "")
}

export default Plugin.define({
  id: "hermem-habits",
  async setup(ctx: any) {
    if (DISABLED) {
      log("disabled", { location: ctx.location?.directory })
      return
    }

    pruneClaims()
    const abort = new AbortController()
    const instance = createHash("sha1").update(String(Math.random())).digest("hex").slice(0, 6)

    const publish = async (sessionID: string) => {
      const habits = listHabits()
      if (habits.length === 0) return
      await ctx.session.instructions.entry.put({ sessionID, key: INSTRUCTION_KEY, value: habitBlock(habits) })
      log("injected", { instance, sessionID, habits: habits.length })
    }

    const capture = async (sessionID: string) => {
      if (capturing.has(sessionID)) return
      const previous = lastCapture.get(sessionID) ?? 0
      if (Date.now() - previous < COOLDOWN_MS) return
      capturing.add(sessionID)
      try {
        const messages: any[] = await ctx.session.context({ sessionID })
        const digest = buildDigest(messages)
        const fingerprint = sha1(digest)
        const stateKey = `habits/${sessionID}`
        const state: any = await ctx.storage.get(stateKey)
        if (state?.fingerprint === fingerprint) return
        const claimKey = `${sessionID}-${fingerprint.slice(0, 16)}`
        if (!claimCapture(claimKey)) return
        // Claim this transcript before spending a model call: end-of-turn
        // events arrive several times, and a failing provider must not be
        // retried once per duplicate. A later turn produces a new digest and
        // captures the whole transcript again, so nothing is lost.
        lastCapture.set(sessionID, Date.now())
        await ctx.storage.set(stateKey, { fingerprint, at: Date.now() })
        if (digest.length < MIN_DIGEST_CHARS) {
          log("skipped-short", { instance, sessionID, chars: digest.length })
          return
        }

        const existing = listHabits()
        const prompt = buildPrompt(digest, existing)
        let answer = ""
        try {
          answer = await extractRules(ctx, sessionID, prompt)
        } catch (error) {
          try { writeFileSync(join(CLAIM_DIR, claimKey), `failed:${Date.now()}`) } catch {}
          throw error
        }
        log("extracted", { instance, sessionID, chars: digest.length, answer: clip(answer, 300) })
        const known = new Set(existing.map((habit) => normalize(habit.content)))
        const stored: string[] = []
        for (const rule of parseHabits(answer)) {
          if (known.has(normalize(rule))) continue
          if (similarHabitExists(rule)) continue
          try {
            storeHabit(rule)
            known.add(normalize(rule))
            stored.push(rule)
          } catch (error) {
            log("store-failed", { sessionID, rule, error: String(error) })
          }
        }
        await ctx.storage.set(stateKey, { fingerprint, at: Date.now(), stored })
        log("captured", { instance, sessionID, messages: messages.length, stored: stored.length })
        if (stored.length > 0) await publish(sessionID)
      } finally {
        capturing.delete(sessionID)
      }
    }

    try {
      await ctx.tool.transform((editor: any) => {
        editor.add({
          name: "hermem_habit_save",
          description:
            "Save one durable working preference about the user to hermem (session-independent, reused in every project). Use when the user states a lasting preference, e.g. 'I use uv, not pip'.",
          input: {
            type: "object",
            properties: {
              rule: {
                type: "string",
                description:
                  "Imperative rule for future sessions, e.g. 'Use uv instead of pip for Python dependencies and environments.'",
              },
            },
            required: ["rule"],
            additionalProperties: false,
          },
          execute: async (input: any, context: any) => {
            const rule = String(input?.rule ?? "").trim().replace(/\s+/g, " ")
            if (!rule) return { content: "Empty rule — nothing stored." }
            if (rule.length > MAX_HABIT_CHARS) return { content: `Rule too long (max ${MAX_HABIT_CHARS} chars).` }
            if (similarHabitExists(rule)) return { content: `Already known: ${rule}` }
            const id = storeHabit(rule)
            const sessionID = String(context?.sessionID ?? "")
            if (sessionID) await publish(sessionID).catch(() => {})
            log("tool-saved", { id, rule, sessionID })
            return { content: `Stored in hermem as ${id}: ${rule}` }
          },
        })
      })
    } catch (error) {
      log("tool-register-failed", { error: String(error) })
    }

    void (async () => {
      try {
        for await (const event of ctx.event.subscribe({ signal: abort.signal })) {
          const type = String(event?.type ?? "")
          const directory = event?.location?.directory
          if (directory && directory !== ctx.location?.directory) continue
          const sessionID = String((event as any)?.data?.sessionID ?? "")
          if (!sessionID) continue

          if (type === "session.created") {
            log("session-created", { instance, sessionID })
            if (!published.has(sessionID)) {
              published.add(sessionID)
              void publish(sessionID).catch((error) => log("publish-failed", { sessionID, error: String(error) }))
            }
            continue
          }
          // session.execution.succeeded is the end-of-turn signal the server
          // actually emits (session.idle exists in the event enum but never
          // reaches plugins in 2.2.0). It can arrive more than once per turn,
          // which the in-flight guard plus the fingerprint check absorb.
          if (type === "session.execution.succeeded") {
            if (!published.has(sessionID)) {
              published.add(sessionID)
              void publish(sessionID).catch((error) => log("publish-failed", { sessionID, error: String(error) }))
            }
            void capture(sessionID).catch((error) => log("capture-failed", { sessionID, error: String(error) }))
          }
        }
      } catch (error) {
        if (!abort.signal.aborted) log("stream-failed", { error: String(error) })
      }
    })()

    log("ready", { instance, location: ctx.location?.directory, hermem: HERMEM, model: MODEL || "session" })

    return () => {
      abort.abort()
      capturing.clear()
    }
  },
})
