import { describe, it, expect } from "vitest";
import { Client, APIError } from "../src/index";
import { mockFetch } from "./helpers";

describe("MemoryClient", () => {
  it("Store sends POST /store", async () => {
    const restore = mockFetch({ method: "POST", path: "/store", status: 204 });
    try {
      const c = new Client("http://localhost:8420");
      await c.memory.store({ id: "paris", category: "world", content: "Paris is the capital of France" });
    } finally {
      restore();
    }
  });

  it("Search returns parsed SearchResult[]", async () => {
    const restore = mockFetch({
      method: "POST",
      path: "/search",
      respBody: [
        { entity: { id: "paris", category: "world", content: "x", archived: false }, similarity: 0.95 },
      ],
    });
    try {
      const c = new Client("http://localhost:8420");
      const got = await c.memory.search({ query: "capital", top_k: 5 });
      expect(got).toHaveLength(1);
      expect(got[0].entity.id).toBe("paris");
      expect(got[0].similarity).toBeCloseTo(0.95);
    } finally {
      restore();
    }
  });

  it("Retrieve returns parsed RetrievalResult", async () => {
    const restore = mockFetch({
      method: "POST",
      path: "/retrieve",
      respBody: { seed_nodes: [{ entity: { id: "paris", category: "world", content: "x", archived: false } }] },
    });
    try {
      const c = new Client("http://localhost:8420");
      const got = await c.memory.retrieve({ seed_ids: ["paris"], max_depth: 2 });
      expect(got.seed_nodes).toHaveLength(1);
      expect(got.seed_nodes[0].entity.id).toBe("paris");
    } finally {
      restore();
    }
  });

  it("Query returns parsed QueryResponse", async () => {
    const restore = mockFetch({
      method: "POST",
      path: "/query",
      respBody: { context: "Paris is the capital of France." },
    });
    try {
      const c = new Client("http://localhost:8420");
      const got = await c.memory.query({ query: "capital", top_k: 5 });
      expect(got.context).toBe("Paris is the capital of France.");
    } finally {
      restore();
    }
  });

  it("Explain returns parsed RetrievalResult", async () => {
    const restore = mockFetch({
      method: "POST",
      path: "/query/explain",
      respBody: { seed_nodes: [{ entity: { id: "paris", category: "world", content: "x", archived: false } }] },
    });
    try {
      const c = new Client("http://localhost:8420");
      const got = await c.memory.explain({ query: "capital", top_k: 5 });
      expect(got.seed_nodes).toHaveLength(1);
    } finally {
      restore();
    }
  });

  it("Ingest sends POST /ingest", async () => {
    const restore = mockFetch({ method: "POST", path: "/ingest", status: 204 });
    try {
      const c = new Client("http://localhost:8420");
      await c.memory.ingest({ dialog: "I love Paris." });
    } finally {
      restore();
    }
  });

  it("Edge sends POST /edge", async () => {
    const restore = mockFetch({ method: "POST", path: "/edge", status: 204 });
    try {
      const c = new Client("http://localhost:8420");
      await c.memory.edge({ source_id: "a", target_id: "b", relation_type: "knows", auto_create: true });
    } finally {
      restore();
    }
  });

  it("ReEmbed returns parsed ReEmbedResult", async () => {
    const restore = mockFetch({
      method: "POST",
      path: "/admin/re-embed",
      respBody: {
        total_entities: 100, re_embedded: 100, skipped: 0, failed: 0,
        elapsed: "1.0s", old_dim: 384, new_dim: 384, batches: 1,
      },
    });
    try {
      const c = new Client("http://localhost:8420");
      const got = await c.memory.reEmbed({ dim: 384 });
      expect(got.total_entities).toBe(100);
      expect(got.old_dim).toBe(384);
      expect(got.new_dim).toBe(384);
    } finally {
      restore();
    }
  });

  it("Error response propagates as APIError", async () => {
    const restore = mockFetch({
      method: "POST",
      path: "/store",
      status: 400,
      respBody: { error: "validation failed", code: "invalid" },
    });
    try {
      const c = new Client("http://localhost:8420");
      await expect(c.memory.store({ id: "x", category: "y", content: "z" })).rejects.toMatchObject({
        statusCode: 400,
        code: "invalid",
        message: expect.stringContaining("validation failed"),
      });
    } finally {
      restore();
    }
  });
});
