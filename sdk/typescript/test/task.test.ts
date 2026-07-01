import { describe, it, expect } from "vitest";
import { Client, APIError } from "../src/index";
import { mockFetch } from "./helpers";

describe("TaskClient", () => {
  it("Create sends POST /task/create", async () => {
    const restore = mockFetch({
      method: "POST",
      path: "/task/create",
      respBody: { id: "task-1", status: "pending" },
    });
    try {
      const c = new Client("http://localhost:8420");
      const got = await c.task.create({ content: "do thing" });
      expect(got.id).toBe("task-1");
      expect(got.status).toBe("pending");
    } finally {
      restore();
    }
  });

  it("Status sends POST /task/status", async () => {
    const restore = mockFetch({ method: "POST", path: "/task/status", status: 204 });
    try {
      const c = new Client("http://localhost:8420");
      await c.task.status({ id: "task-1", status: "done" });
    } finally {
      restore();
    }
  });

  it("List sends POST /task/list", async () => {
    const restore = mockFetch({
      method: "POST",
      path: "/task/list",
      respBody: { tasks: [{ id: "t1", category: "task", content: "x", archived: false }] },
    });
    try {
      const c = new Client("http://localhost:8420");
      const got = await c.task.list({ status: "pending" });
      expect(got.tasks).toHaveLength(1);
      expect(got.tasks[0].id).toBe("t1");
    } finally {
      restore();
    }
  });

  it("Show sends POST /task/show", async () => {
    const restore = mockFetch({
      method: "POST",
      path: "/task/show",
      respBody: {
        entity: { id: "t1", category: "task", content: "x", archived: false },
        blocked_by: [],
        recovers_via: [],
      },
    });
    try {
      const c = new Client("http://localhost:8420");
      const got = await c.task.show({ id: "t1" });
      expect(got.entity.id).toBe("t1");
      expect(got.blocked_by).toEqual([]);
    } finally {
      restore();
    }
  });

  it("Dep sends POST /task/dep", async () => {
    const restore = mockFetch({ method: "POST", path: "/task/dep", status: 204 });
    try {
      const c = new Client("http://localhost:8420");
      await c.task.dep({ source_id: "a", target_id: "b", add: true });
    } finally {
      restore();
    }
  });

  it("Tree sends POST /task/tree", async () => {
    const restore = mockFetch({
      method: "POST",
      path: "/task/tree",
      respBody: { tree: "root\n  t1\n  t2" },
    });
    try {
      const c = new Client("http://localhost:8420");
      const got = await c.task.tree({ goal_id: "g1" });
      expect(got.tree).toContain("root");
      expect(got.tree).toContain("t1");
    } finally {
      restore();
    }
  });

  it("Rollback sends POST /task/rollback", async () => {
    const restore = mockFetch({
      method: "POST",
      path: "/task/rollback",
      respBody: { rollback_task_id: "rb-1" },
    });
    try {
      const c = new Client("http://localhost:8420");
      const got = await c.task.rollback({ id: "t1" });
      expect(got.rollback_task_id).toBe("rb-1");
    } finally {
      restore();
    }
  });

  it("Executable sends POST /task/executable", async () => {
    const restore = mockFetch({
      method: "POST",
      path: "/task/executable",
      respBody: { tasks: [{ id: "t1", category: "task", content: "x", archived: false }] },
    });
    try {
      const c = new Client("http://localhost:8420");
      const got = await c.task.executable({});
      expect(got.tasks).toHaveLength(1);
    } finally {
      restore();
    }
  });

  it("Next is an alias for Executable (same /task/executable path)", async () => {
    const restore = mockFetch({
      method: "POST",
      path: "/task/executable",
      respBody: { tasks: [] },
    });
    try {
      const c = new Client("http://localhost:8420");
      const got = await c.task.next({});
      expect(got.tasks).toEqual([]);
    } finally {
      restore();
    }
  });

  it("Error response propagates as APIError", async () => {
    const restore = mockFetch({
      method: "POST",
      path: "/task/create",
      status: 403,
      respBody: { error: "forbidden", code: "forbidden" },
    });
    try {
      const c = new Client("http://localhost:8420");
      await expect(c.task.create({ content: "x" })).rejects.toMatchObject({
        statusCode: 403,
        code: "forbidden",
      });
    } finally {
      restore();
    }
  });
});
