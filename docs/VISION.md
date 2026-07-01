# Hermem Vision

## Goal

Transform Hermem from a graph memory database into a **reasoning memory platform** — a system where AI agents don't just store facts, but understand, reason about, and evolve their knowledge over time.

---

## Core Pillars

### 1. Belief Engine

The Belief Engine tracks the confidence and trustworthiness of every fact in the knowledge graph. Not all knowledge is equally reliable — a fact observed directly is more trustworthy than one inferred through five hops. The Belief Engine models this explicitly.

**What it enables**: Confidence-weighted retrieval, trust-aware ranking, and automatic belief revision when new evidence contradicts existing knowledge.

**Dependencies**: Contradiction Engine (for detecting conflicts), Episodic Memory (for temporal context).

### 2. Contradiction Engine

When two facts conflict, the system must decide: keep both (as a contradiction), merge them (preferring the newer), or archive the old. The Contradiction Engine implements detection, scoring, and resolution strategies.

**What it enables**: Knowledge that self-corrects. Conflicting information is surfaced rather than silently overwritten.

**Dependencies**: Belief Engine (for confidence-based resolution), Episodic Memory (for temporal ordering).

### 3. Episodic Memory

Episodes are time-stamped records of what happened, when, and why. They provide the temporal backbone for retrieval, allowing queries like "what did we discuss about X last week?" or "when was this fact last validated?"

**What it enables**: Temporal queries, provenance tracking, and session-aware retrieval.

**Dependencies**: None (foundational).

### 4. Semantic Compression

As the knowledge graph grows, older or less-relevant facts must be compressed without losing essential information. Summary nodes, cluster compression, and memory condensation keep the graph manageable.

**What it enables**: Scalability beyond thousands of entities, graceful memory aging.

**Dependencies**: Belief Engine (for confidence-based compression), Episodic Memory (for recency signals).

### 5. Long-Term Agent Memory

The ultimate goal: agents that remember not just facts, but their own history of decisions, goals, and self-reflections. This pillar transforms Hermem from a passive store into an active cognitive substrate.

**What it enables**: Goal-aware planning, decision history, personality consistency, self-improvement loops.

**Dependencies**: All other pillars.

---

## Moonshot Goals

These are long-term research directions that may or may not ship, but inform the architecture:

- **Memory-driven planning**: Agents plan actions based on what they remember, not just what they're told.
- **Memory-driven reasoning**: Deductive and abductive reasoning over the knowledge graph.
- **Memory-driven self-improvement**: Agents that learn from their own failures and adjust their behavior.
- **Autonomous memory ecosystem**: Multiple agents sharing and evolving a collective memory.

---

## Architecture Principles

1. **Single binary, zero infrastructure** — SQLite + embeddings in one executable.
2. **Graph-native** — knowledge lives as connected entities, not isolated documents.
3. **Confidence-aware** — every fact carries trust and recency signals.
4. **Contradiction-tolerant** — conflicts are surfaced, not hidden.
5. **Temporal** — time is a first-class dimension in retrieval and ranking.
6. **Agent-first** — designed for autonomous agents, not just human users.
