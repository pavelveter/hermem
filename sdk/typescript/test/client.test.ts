import { describe, it, expect } from "vitest";
import { Client, APIError } from "./src/index";

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
