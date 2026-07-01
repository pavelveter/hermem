/** Test helpers for the Hermem TypeScript SDK. */

import { vi, expect } from "vitest";

export interface MockFetchOptions {
  method: string;
  path: string;
  query?: Record<string, string>;
  respBody?: unknown;
  status?: number;
}

/**
 * Mocks globalThis.fetch so subsequent SDK calls hit a fake endpoint.
 * Returns a restore function that reinstates the original fetch.
 *
 * Usage::
 *
 *   const restore = mockFetch({ method: "POST", path: "/store", status: 204 });
 *   try {
 *     const c = new Client("http://localhost:8420");
 *     await c.memory.store({ id: "x", category: "y", content: "z" });
 *   } finally {
 *     restore();
 *   }
 */
export function mockFetch(opts: MockFetchOptions): () => void {
  const { method, path, query, respBody, status = 200 } = opts;
  const original = globalThis.fetch;
  globalThis.fetch = vi.fn(async (url: RequestInfo | URL, init?: RequestInit) => {
    const u = new URL(url.toString());
    expect(u.pathname).toBe(path);
    expect(init?.method ?? "GET").toBe(method);
    if (query) {
      for (const [k, v] of Object.entries(query)) {
        expect(u.searchParams.get(k)).toBe(v);
      }
    }
    if (respBody !== null && respBody !== undefined) {
      return new Response(JSON.stringify(respBody), {
        status,
        headers: { "Content-Type": "application/json" },
      });
    }
    return new Response(null, { status });
  }) as typeof fetch;
  return () => {
    globalThis.fetch = original;
  };
}
