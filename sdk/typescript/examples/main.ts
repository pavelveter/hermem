/**
 * Example: Hermem TypeScript SDK usage.
 *
 * Prerequisites:
 *   - Running Hermem server: hermem serve
 *   - API key (if configured): export HERMEM_API_KEY=your-key
 *   - Install: npm install hermem
 *
 * Run: npx tsx sdk/typescript/examples/main.ts
 */
import { Client, APIError } from "../src/index.js";

const baseURL = process.env.HERMEM_URL || "http://localhost:8420";
const apiKey = process.env.HERMEM_API_KEY || "";

const client = new Client(baseURL, { apiKey, timeout: 30_000 });

async function main() {
  // --- Memory ---
  console.log("=== Memory ===");

  try {
    const storeResult = await client.memory.store({
      id: "example-1",
      category: "fact",
      content: "The Hermem knowledge graph supports semantic search and multi-hop retrieval.",
    });
    console.log(`Store: ${storeResult.status}`);
  } catch (e) {
    console.log(`Store error: ${(e as Error).message}`);
  }

  try {
    const searchResult = await client.memory.search({ query: "semantic search", limit: 5 });
    console.log(`Search: ${searchResult.results.length} results`);
  } catch (e) {
    console.log(`Search error: ${(e as Error).message}`);
  }

  // --- Tasks ---
  console.log("\n=== Tasks ===");

  try {
    const task = await client.task.create({
      content: "Implement MCP server integration",
      context_ids: ["example-1"],
    });
    console.log(`Task created: ${task.id}`);

    const tasks = await client.task.list({ status: "pending" });
    console.log(`Pending tasks: ${tasks.tasks.length}`);
  } catch (e) {
    console.log(`Task error: ${(e as Error).message}`);
  }

  // --- Graph ---
  console.log("\n=== Graph ===");

  try {
    const components = await client.graph.connectedComponents({ min_size: 2 });
    console.log(`Components: ${components.length}`);
  } catch (e) {
    console.log(`Graph error: ${(e as Error).message}`);
  }

  // --- Admin ---
  console.log("\n=== Admin ===");

  try {
    const health = await client.admin.health();
    console.log(`Health: ${health.status}`);
  } catch (e) {
    console.log(`Health error: ${(e as Error).message}`);
  }

  console.log("\nDone!");
}

main().catch(console.error);
