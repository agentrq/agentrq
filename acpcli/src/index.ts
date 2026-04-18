/**
 * index.ts
 *
 * The main entry point for acpcli.
 * Orchestrates the bridge between the ACP Agent and the agentrq MCP Server.
 */

import { spawn } from "node:child_process";
import { Writable, Readable } from "node:stream";
import * as acp from "@agentclientprotocol/sdk";
import { loadMcpConfig, pickAgentrqServer } from "./config.js";
import { MCPBridge } from "./mcpClient.js";
import { AgentRQACPClient } from "./acpClient.js";

async function main() {
  const args = process.argv.slice(2);
  
  // Find where the command starts (after -- if provided, or just all args)
  const cmdStartIndex = args.indexOf("--");
  const acpCmdArgs = cmdStartIndex !== -1 ? args.slice(cmdStartIndex + 1) : args;

  if (acpCmdArgs.length === 0) {
    console.error("Usage: acpcli -- <acp-server-command> [args...]");
    console.error("Example: acpcli -- gemini --acp");
    process.exit(1);
  }

  // 1. Load MCP Config
  const configs = loadMcpConfig();
  const agentrqConfig = pickAgentrqServer(configs);

  // 2. Initialize MCP Bridge
  const mcpBridge = new MCPBridge(agentrqConfig);
  await mcpBridge.connect();

  // 3. Spawn ACP Agent Subprocess
  const [cmd, ...cmdArgs] = acpCmdArgs;
  console.error(`[acp] Spawning agent: ${cmd} ${cmdArgs.join(" ")}`);
  
  const agentProcess = spawn(cmd, cmdArgs, {
    stdio: ["pipe", "pipe", "inherit"],
    env: { ...process.env, ...agentrqConfig.env }
  });

  const input = Writable.toWeb(agentProcess.stdin!);
  const output = Readable.toWeb(agentProcess.stdout!) as ReadableStream<Uint8Array>;

  // 4. Create ACP Connection
  const acpClient = new AgentRQACPClient(mcpBridge);
  const stream = acp.ndJsonStream(input, output);
  const connection = new acp.ClientSideConnection((_agent) => acpClient, stream);

  try {
    // 5. Initialize ACP Connection
    const initResult = await connection.initialize({
      protocolVersion: acp.PROTOCOL_VERSION,
      clientCapabilities: {
        fs: {
          readTextFile: true,
          writeTextFile: true,
        },
      },
    });

    console.error(`[acp] Connected to agent (protocol v${initResult.protocolVersion})`);

    // 6. Create ACP Session
    // We pass our agentrq MCP server to the agent so it can use our tools directly.
    // The McpServer object must follow the ACP protocol schema (type, name, url, headers).
    const sessionResult = await connection.newSession({
      cwd: process.cwd(),
      mcpServers: [{
        type: "http",
        name: agentrqConfig.name,
        url: agentrqConfig.url!,
        headers: []
      }],
    });

    const sessionId = sessionResult.sessionId;
    console.error(`[acp] Created session: ${sessionId}`);

    // Bridge: MCP -> ACP
    // When the MCP server sends a notification to 'notifications/claude/channel', 
    // it contains a new task content.
    mcpBridge.on("task", async ({ content }) => {
      console.error("\n[bridge] Incoming task from MCP server. Forwarding to ACP agent...");
      try {
        const result = await connection.prompt({
          sessionId,
          prompt: [{ type: "text", text: content }]
        });
        
        console.error(`\n[acp] Agent completed task. Reason: ${result.stopReason}`);

        // Loop: When a task is complete, automatically ask for the next one
        await checkForNextTask(mcpBridge, connection, sessionId);
      } catch (err) {
        console.error("[acp] Error during prompt execution:", err);
      }
    });

    // Initial check for a pending task
    await checkForNextTask(mcpBridge, connection, sessionId);

    // Keep the process alive
    await new Promise(() => {});

  } catch (error) {
    console.error("[acpcli] Error:", error);
  } finally {
    agentProcess.kill();
    await mcpBridge.close();
    process.exit(0);
  }
}

/**
 * Checks for the next pending task using the 'getNextTask' tool on the MCP server.
 * If found, sends it to the ACP agent.
 */
async function checkForNextTask(mcpBridge: MCPBridge, connection: acp.ClientSideConnection, sessionId: string) {
  console.error("[bridge] Checking for next task via MCP server...");
  try {
    const result = await mcpBridge.callTool("getNextTask");
    
    if (result.isError) {
      console.error("[mcp] Error getting next task:", result.content);
      return;
    }

    const content = result.content[0] as { type: string, text: string };
    if (content && content.text && !content.text.includes("no pending tasks exist")) {
      console.error(`[bridge] Found task: "${content.text.slice(0, 50).replace(/\n/g, " ")}..."`);
      
      const promptResult = await connection.prompt({
        sessionId,
        prompt: [{ type: "text", text: content.text }]
      });

      console.error(`\n[acp] Agent completed with: ${promptResult.stopReason}`);
      
      // Recursively check for next task
      await checkForNextTask(mcpBridge, connection, sessionId);
    } else {
      console.error("[bridge] No pending tasks available.");
    }
  } catch (err) {
    console.error("[bridge] Failed to check for next task:", err);
  }
}

main().catch(err => {
  console.error("[fatal]", err);
  process.exit(1);
});
