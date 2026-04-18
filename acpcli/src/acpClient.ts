/**
 * acpClient.ts
 *
 * Implements the ACP Client interface for the agentrq workspace.
 * Routes permission requests to the MCP server.
 */

import * as acp from "@agentclientprotocol/sdk";
import type { MCPBridge } from "./mcpClient.js";
import * as fs from "node:fs/promises";
import * as path from "node:path";

export class AgentRQACPClient implements acp.Client {
  constructor(private mcpBridge: MCPBridge) {}

  async requestPermission(
    params: acp.RequestPermissionRequest
  ): Promise<acp.RequestPermissionResponse> {
    const requestId = params.toolCall.toolCallId;
    const toolTitle = params.toolCall.title ?? "Unknown Tool";
    console.error(`\n🔐 ACP Permission requested: ${toolTitle} (ID: ${requestId})`);

    const payload = {
      request_id: requestId,
      tool_name: toolTitle,
      description: toolTitle,
      input_preview: JSON.stringify(params.toolCall.rawInput ?? {}),
    };
    console.error(`[acp] Bridge Session ID: ${this.mcpBridge.getSessionId() ?? "unknown"}`);
    console.error(`[acp] Sending permission request notification:`, JSON.stringify(payload, null, 2));

    // 1. Forward the permission request to the MCP server as a notification.
    await this.mcpBridge.sendNotification(
      "notifications/claude/channel/permission_request",
      payload
    );

    // 2. Wait for the verdict from the MCP server
    console.error(`⌛ Waiting for human approval in the agentrq dashboard...`);
    
    return new Promise((resolve) => {
      const handler = (data: { requestId: string; behavior: string }) => {
        if (data.requestId === requestId) {
          // Cleanup this listener
          this.mcpBridge.off("verdict", handler);
          
          console.error(`✅ Permission verdict received: ${data.behavior}`);

          // Map "allow"/"deny" to the correct ACP option
          // ACP options usually include "allow_once", "allow_always", etc.
          const verdictMatch = data.behavior === "allow" ? "allow" : "deny";
          const option = params.options.find(o => 
            o.kind.startsWith(verdictMatch) ||
            o.name.toLowerCase().includes(verdictMatch) || 
            (verdictMatch === "allow" && (o.name.toLowerCase().includes("yes") || o.name.toLowerCase().includes("approve")))
          );

          const optionId = option?.optionId ?? params.options[0].optionId;
          console.error(`[acp] Selected permission option: ${optionId} (${option?.name ?? "default"})`);

          resolve({
            outcome: {
              outcome: "selected",
              optionId: optionId,
            },
          });
        }
      };

      this.mcpBridge.on("verdict", handler);
    });
  }

  async sessionUpdate(params: acp.SessionNotification): Promise<void> {
    const update = params.update;

    switch (update.sessionUpdate) {
      case "agent_message_chunk":
        if (update.content.type === "text") {
          process.stdout.write(update.content.text);
        }
        break;
      case "tool_call":
        console.error(`\n🔧 [ACP Agent] Tool call: ${update.title} (${update.status})`);
        break;
      default:
        break;
    }
  }

  async writeTextFile(
    params: acp.WriteTextFileRequest
  ): Promise<acp.WriteTextFileResponse> {
    const filePath = path.resolve(process.cwd(), params.path);
    console.error(`[acp] Writing file: ${params.path}`);
    
    try {
      await fs.mkdir(path.dirname(filePath), { recursive: true });
      await fs.writeFile(filePath, params.content, "utf8");
      console.error(`[acp] File written successfully: ${params.path}`);
      return {};
    } catch (err: any) {
      console.error(`[acp] Error writing file ${params.path}:`, err.message);
      throw err;
    }
  }

  async readTextFile(
    params: acp.ReadTextFileRequest
  ): Promise<acp.ReadTextFileResponse> {
    const filePath = path.resolve(process.cwd(), params.path);
    console.error(`[acp] Reading file: ${params.path}`);
    
    try {
      const content = await fs.readFile(filePath, "utf8");
      return { content };
    } catch (err: any) {
      console.error(`[acp] Error reading file ${params.path}:`, err.message);
      throw err;
    }
  }
}
