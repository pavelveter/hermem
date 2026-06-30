import { describe, it, expect, vi } from "vitest";
import { Client, APIError, SDK_VERSION } from "./src/index";

describe("Client", () => {
  it("creates a client with base URL", () => {
    const c = new Client("http://localhost:8420");
    expect(c).toBeDefined();
    expect(c.memory).toBeDefined();
    expect(c.task).toBeDefined();
    expect(c.graph).toBeDefined();
    expect(c.admin).toBeDefined();
  });

  it("creates a client with API key", () => {
    const c = new Client("http://localhost:8420", { apiKey: "test-key" });
    expect(c).toBeDefined();
  });

  it("creates a client with timeout", () => {
    const c = new Client("http://localhost:8420", { timeout: 5000 });
    expect(c).toBeDefined();
  });
});

describe("APIError", () => {
  it("formats error message", () => {
    const err = new APIError(404, "not found", "not_found");
    expect(err.message).toBe("hermem: not found (status=404)");
    expect(err.statusCode).toBe(404);
    expect(err.code).toBe("not_found");
  });

  it("formats error without code", () => {
    const err = new APIError(500, "internal error");
    expect(err.message).toBe("hermem: internal error (status=500)");
    expect(err.name).toBe("APIError");
  });
});

describe("Version Negotiation", () => {
  function mockServer(versionHeader: string | null) {
    const originalFetch = globalThis.fetch;
    globalThis.fetch = vi.fn(async () => {
      const headers = new Headers();
      if (versionHeader !== null) {
        headers.set("X-Hermem-API-Version", versionHeader);
      }
      return new Response(JSON.stringify({ status: "ok" }), {
        status: 200,
        headers,
      });
    }) as any;
    return () => {
      globalThis.fetch = originalFetch;
    };
  }

  it("same major does not trigger callback", async () => {
    const restore = mockServer("0.5.0");
    const cb = vi.fn();
    const c = new Client("http://localhost:8420", { onVersionMismatch: cb });
    await c.admin.health();
    expect(cb).not.toHaveBeenCalled();
    restore();
  });

  it("different major triggers callback", async () => {
    const restore = mockServer("1.0.0");
    const cb = vi.fn();
    const c = new Client("http://localhost:8420", { onVersionMismatch: cb });
    await c.admin.health();
    expect(cb).toHaveBeenCalledWith("1.0.0", SDK_VERSION);
    restore();
  });

  it("callback called only once", async () => {
    const restore = mockServer("1.0.0");
    const cb = vi.fn();
    const c = new Client("http://localhost:8420", { onVersionMismatch: cb });
    await c.admin.health();
    await c.admin.health();
    await c.admin.health();
    expect(cb).toHaveBeenCalledTimes(1);
    restore();
  });

  it("no header skips check", async () => {
    const restore = mockServer(null);
    const cb = vi.fn();
    const c = new Client("http://localhost:8420", { onVersionMismatch: cb });
    await c.admin.health();
    expect(cb).not.toHaveBeenCalled();
    restore();
  });

  it("SDK_VERSION is defined", () => {
    expect(SDK_VERSION).toBeDefined();
    expect(typeof SDK_VERSION).toBe("string");
  });
});
