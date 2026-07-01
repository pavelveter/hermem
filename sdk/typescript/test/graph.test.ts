import { describe, it, expect } from "vitest";
import { Client, APIError } from "../src/index";
import { mockFetch } from "./helpers";

describe("GraphClient", () => {
  it("Verify sends GET /graph/verify", async () => {
    const restore = mockFetch({
      method: "GET",
      path: "/graph/verify",
      respBody: { issues: [] },
    });
    try {
      const c = new Client("http://localhost:8420");
      const got = await c.graph.verify();
      expect(got.issues).toEqual([]);
    } finally {
      restore();
    }
  });

  it("Contradictions sends GET /contradictions (no entity id)", async () => {
    const restore = mockFetch({
      method: "GET",
      path: "/contradictions",
      respBody: [{ source_id: "a", source_content: "x", target_id: "b", target_content: "y" }],
    });
    try {
      const c = new Client("http://localhost:8420");
      const got = await c.graph.contradictions("");
      expect(got).toHaveLength(1);
      expect(got[0].source_id).toBe("a");
    } finally {
      restore();
    }
  });

  it("Contradictions appends ?id=<encoded> when entityId is given", async () => {
    const restore = mockFetch({
      method: "GET",
      path: "/contradictions",
      query: { id: "paris" },
      respBody: [],
    });
    try {
      const c = new Client("http://localhost:8420");
      await c.graph.contradictions("paris");
    } finally {
      restore();
    }
  });

  it("ConnectedComponents sends GET /connected-components (no min_size)", async () => {
    const restore = mockFetch({
      method: "GET",
      path: "/connected-components",
      respBody: [{ ids: ["a", "b", "c"], size: 3, avg_degree: 1.5 }],
    });
    try {
      const c = new Client("http://localhost:8420");
      const got = await c.graph.connectedComponents(0);
      expect(got).toHaveLength(1);
      expect(got[0].size).toBe(3);
    } finally {
      restore();
    }
  });

  it("ConnectedComponents appends ?min_size=N when minSize > 0", async () => {
    const restore = mockFetch({
      method: "GET",
      path: "/connected-components",
      query: { min_size: "2" },
      respBody: [],
    });
    try {
      const c = new Client("http://localhost:8420");
      await c.graph.connectedComponents(2);
    } finally {
      restore();
    }
  });

  it("Communities sends GET /communities", async () => {
    const restore = mockFetch({
      method: "GET",
      path: "/communities",
      respBody: { communities: [], global_modularity: 0.0, total_communities: 0 },
    });
    try {
      const c = new Client("http://localhost:8420");
      const got = await c.graph.communities({});
      expect(got.total_communities).toBe(0);
    } finally {
      restore();
    }
  });

  it("Timeline sends GET /timeline (no limit)", async () => {
    const restore = mockFetch({
      method: "GET",
      path: "/timeline",
      respBody: [{ id: "t1", category: "world", content: "x", created_at: "2024-01-01T00:00:00Z" }],
    });
    try {
      const c = new Client("http://localhost:8420");
      const got = await c.graph.timeline(0);
      expect(got).toHaveLength(1);
    } finally {
      restore();
    }
  });

  it("Timeline appends ?limit=N when limit > 0", async () => {
    const restore = mockFetch({
      method: "GET",
      path: "/timeline",
      query: { limit: "10" },
      respBody: [],
    });
    try {
      const c = new Client("http://localhost:8420");
      await c.graph.timeline(10);
    } finally {
      restore();
    }
  });

  it("Provenance sends GET /provenance", async () => {
    const restore = mockFetch({
      method: "GET",
      path: "/provenance",
      respBody: [{ id: "e1", category: "world", content: "x", archived: false }],
    });
    try {
      const c = new Client("http://localhost:8420");
      const got = await c.graph.provenance({});
      expect(got).toHaveLength(1);
    } finally {
      restore();
    }
  });

  it("RecoveryPlan appends ?id=<taskId>", async () => {
    const restore = mockFetch({
      method: "GET",
      path: "/recovery-plan",
      query: { id: "task-1" },
      respBody: [{ id: "rb-1", category: "task", content: "recover", archived: false }],
    });
    try {
      const c = new Client("http://localhost:8420");
      const got = await c.graph.recoveryPlan("task-1");
      expect(got).toHaveLength(1);
      expect(got[0].id).toBe("rb-1");
    } finally {
      restore();
    }
  });

  it("Error response propagates as APIError", async () => {
    const restore = mockFetch({
      method: "GET",
      path: "/graph/verify",
      status: 500,
      respBody: { error: "db down" },
    });
    try {
      const c = new Client("http://localhost:8420");
      await expect(c.graph.verify()).rejects.toMatchObject({
        statusCode: 500,
        message: expect.stringContaining("db down"),
      });
    } finally {
      restore();
    }
  });
});
