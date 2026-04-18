/**
 * mcpClient.ts
 *
 * Connects to the agentrq MCP server using the MCP TypeScript SDK.
 * Listens for 'notifications/claude/channel' and handles tool calls.
 */

import { Client } from "@modelcontextprotocol/sdk/client/index.js";
import { StreamableHTTPClientTransport } from "@modelcontextprotocol/sdk/client/streamableHttp.js";
import { EventEmitter } from "node:events";
import { z } from "zod";
import type { McpServerConfig } from "./config.js";

export class MCPBridge extends EventEmitter {
  private client: Client;
  private transport: StreamableHTTPClientTransport;

  public getSessionId(): string | undefined {
    return (this.transport as any)._sessionId;
  }

  constructor(private config: McpServerConfig) {
    super();
    if (!config.url) {
      throw new Error(`MCP server ${config.name} has no URL`);
    }

    const url = new URL(config.url);
    this.transport = new StreamableHTTPClientTransport(url);
    this.client = new Client(
      {
        name: "acpcli-bridge",
        version: "0.1.0",
      },
      {
        capabilities: {},
      }
    );
  }

  private isConnected = false;
  private isConnecting = false;

  async connect() {
    if (this.isConnecting || this.isConnected) return;
    this.isConnecting = true;

    let attempt = 0;
    const initialDelay = 1000;
    const maxDelay = 30000;

    while (!this.isConnected) {
      try {
        await this._connectOnce();
        this.isConnected = true;
        this.isConnecting = false;
        console.error(`[mcp] Connected to ${this.config.name}`);
      } catch (error) {
        const delay = Math.min(initialDelay * Math.pow(2, attempt), maxDelay);
        console.error(
          `[mcp] Connection failed to ${this.config.name}. Retrying in ${delay / 1000}s...`
        );
        await new Promise((resolve) => setTimeout(resolve, delay));
        attempt++;
      }
    }
  }

  private async _connectOnce() {
    if (this.transport) {
      await this.transport.close().catch(() => {});
    }

    const url = new URL(this.config.url);
    this.transport = new StreamableHTTPClientTransport(url);
    
    // Set up transport hooks for disconnection
    this.transport.onclose = () => {
      if (this.isConnected) {
        console.error(`[mcp] Connection to ${this.config.name} lost.`);
        this.isConnected = false;
        this.connect(); // Start reconnection loop
      }
    };

    this.transport.onerror = (error) => {
      console.error(`[mcp] Transport error:`, error.message || error);
    };

    await this.client.connect(this.transport);

    // Set notification handler for directives from the MCP server.
    this.client.setNotificationHandler(
      z.object({
        method: z.literal("notifications/claude/channel"),
        params: z.object({
          content: z.string(),
          meta: z.any().optional(),
        }),
      }),
      (notification) => {
        console.error("[mcp] Received channel notification");
        const { content, meta } = notification.params;
        this.emit("task", { content, meta });
      }
    );

    // Set notification handler for permission verdicts
    this.client.setNotificationHandler(
      z.object({
        method: z.literal("notifications/claude/channel/permission"),
        params: z.object({
          request_id: z.string(),
          behavior: z.string(), // "allow" | "deny"
        }),
      }),
      (notification) => {
        console.error("[mcp] Received permission verdict");
        const { request_id, behavior } = notification.params;
        this.emit("verdict", { requestId: request_id, behavior });
      }
    );
  }

  private async ensureConnected() {
    if (this.isConnected) return;
    if (!this.isConnecting) {
      this.connect();
    }
    
    // Wait up to 10 seconds for connection
    let waited = 0;
    while (!this.isConnected && waited < 10000) {
      await new Promise(resolve => setTimeout(resolve, 500));
      waited += 500;
    }

    if (!this.isConnected) {
      throw new Error(`MCP not connected after 10s timeout`);
    }
  }

  async callTool(name: string, args: any = {}) {
    await this.ensureConnected();
    return await this.client.callTool({
      name,
      arguments: args,
    });
  }

  async sendNotification(method: string, params: any) {
    await this.ensureConnected();
    await this.client.notification({
      method,
      params,
    });
  }

  async close() {
    await this.client.close();
  }
}
