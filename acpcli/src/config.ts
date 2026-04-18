/**
 * config.ts
 *
 * Reads the .mcp.json file from the workspace root and returns the first
 * HTTP MCP server config (preferring any server whose name contains "agentrq").
 */

import { readFileSync } from "node:fs";
import { resolve } from "node:path";

export interface McpServerConfig {
  name: string;
  type: "http" | "stdio";
  url?: string;
  command?: string;
  args?: string[];
  env?: Record<string, string>;
}

interface McpJson {
  mcpServers: Record<
    string,
    {
      type?: "http" | "stdio";
      url?: string;
      command?: string;
      args?: string[];
      env?: Record<string, string>;
    }
  >;
}

/**
 * Find and parse the .mcp.json config file.
 * Searches CWD and then up to 3 parent directories.
 */
export function loadMcpConfig(startDir: string = process.cwd()): McpServerConfig[] {
  const candidates = [
    resolve(startDir, ".mcp.json"),
    resolve(startDir, "..", ".mcp.json"),
    resolve(startDir, "..", "..", ".mcp.json"),
    resolve(startDir, "..", "..", "..", ".mcp.json"),
  ];

  for (const candidate of candidates) {
    try {
      const raw = readFileSync(candidate, "utf-8");
      const parsed: McpJson = JSON.parse(raw);
      const servers: McpServerConfig[] = Object.entries(
        parsed.mcpServers ?? {}
      ).map(([name, cfg]) => ({
        name,
        type: cfg.type ?? (cfg.url ? "http" : "stdio"),
        url: cfg.url,
        command: cfg.command,
        args: cfg.args,
        env: cfg.env,
      }));

      if (servers.length > 0) {
        console.error(`[config] Loaded .mcp.json from ${candidate}`);
        return servers;
      }
    } catch {
      // Not found here, try next
    }
  }

  throw new Error(
    "Could not find .mcp.json — run acpcli from your workspace root"
  );
}

/**
 * Pick the primary agentrq MCP server from the list.
 * Prefers servers with "agentrq" in the name; falls back to the first HTTP server.
 */
export function pickAgentrqServer(
  servers: McpServerConfig[]
): McpServerConfig {
  // Prefer named agentrq server
  const named = servers.find(
    (s) => s.name.toLowerCase().includes("agentrq") && s.type === "http" && s.url
  );
  if (named) return named;

  // Fall back to first HTTP server
  const http = servers.find((s) => s.type === "http" && s.url);
  if (http) return http;

  throw new Error(
    "No HTTP MCP server found in .mcp.json — expected at least one entry with type=http and url"
  );
}
