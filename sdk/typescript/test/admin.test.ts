import { describe, it, expect } from "vitest";
import { Client, APIError } from "../src/index";
import { mockFetch } from "./helpers";

describe("AdminClient", () => {
  it("MigrateStatus sends GET /db/migrate", async () => {
    const restore = mockFetch({
      method: "GET",
      path: "/db/migrate",
      respBody: [{ name: "001_init", applied: true, checksum_match: true }],
    });
    try {
      const c = new Client("http://localhost:8420");
      const got = await c.admin.migrateStatus();
      expect(got).toHaveLength(1);
      expect(got[0].name).toBe("001_init");
      expect(got[0].applied).toBe(true);
    } finally {
      restore();
    }
  });

  it("Schema sends GET /db/schema", async () => {
    const restore = mockFetch({
      method: "GET",
      path: "/db/schema",
      respBody: { stored: "abc", current: "abc", drift_detected: false },
    });
    try {
      const c = new Client("http://localhost:8420");
      const got = await c.admin.schema();
      expect(got.stored).toBe(got.current);
      expect(got.drift_detected).toBe(false);
    } finally {
      restore();
    }
  });

  it("VerifyDB sends GET /db/verify", async () => {
    const restore = mockFetch({ method: "GET", path: "/db/verify", status: 204 });
    try {
      const c = new Client("http://localhost:8420");
      await c.admin.verifyDB();
    } finally {
      restore();
    }
  });

  it("Rollback sends POST /db/rollback", async () => {
    const restore = mockFetch({ method: "POST", path: "/db/rollback", status: 204 });
    try {
      const c = new Client("http://localhost:8420");
      await c.admin.rollback();
    } finally {
      restore();
    }
  });

  it("Health sends GET /health", async () => {
    const restore = mockFetch({
      method: "GET",
      path: "/health",
      respBody: { status: "ok" },
    });
    try {
      const c = new Client("http://localhost:8420");
      const got = await c.admin.health();
      expect(got.status).toBe("ok");
    } finally {
      restore();
    }
  });

  it("Ready sends GET /health/ready", async () => {
    const restore = mockFetch({
      method: "GET",
      path: "/health/ready",
      respBody: { status: "ready", latency_ms: 12, checks: undefined },
    });
    try {
      const c = new Client("http://localhost:8420");
      const got = await c.admin.ready();
      expect(got.status).toBe("ready");
      expect(got.latency_ms).toBe(12);
    } finally {
      restore();
    }
  });

  it("Error response propagates as APIError", async () => {
    const restore = mockFetch({
      method: "GET",
      path: "/db/verify",
      status: 503,
      respBody: { error: "db unavailable", code: "db_down" },
    });
    try {
      const c = new Client("http://localhost:8420");
      await expect(c.admin.verifyDB()).rejects.toMatchObject({
        statusCode: 503,
        code: "db_down",
      });
    } finally {
      restore();
    }
  });
});
