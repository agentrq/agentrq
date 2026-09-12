// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.

import { describe, it, expect, vi } from 'vitest';

import { WebMCPStatus, getModelContext, registerTools } from '../src/webmcp/modelContext';

const workingContext = () => ({ registerTool: vi.fn().mockResolvedValue(undefined) });

describe('getModelContext', () => {
  it('prefers document.modelContext, the current spelling', () => {
    const onDocument = workingContext();
    const onNavigator = workingContext();

    expect(getModelContext({ document: { modelContext: onDocument }, navigator: { modelContext: onNavigator } }))
      .toBe(onDocument);
  });

  it('still finds navigator.modelContext, which older browsers have', () => {
    // Deprecated in Chromium 150, but a browser that has not moved yet is
    // still a browser with WebMCP.
    const onNavigator = workingContext();

    expect(getModelContext({ document: {}, navigator: { modelContext: onNavigator } })).toBe(onNavigator);
  });

  it('reports nothing where the browser has no WebMCP', () => {
    // The ordinary case, and not an error.
    expect(getModelContext({ document: {}, navigator: {} })).toBeNull();
  });

  it('survives a context with neither global, as in a worker', () => {
    expect(getModelContext({ document: undefined, navigator: undefined })).toBeNull();
  });

  it('rejects an object that cannot register a tool', () => {
    // Reporting tools against a stub would claim capabilities that do not exist.
    expect(getModelContext({ document: { modelContext: {} }, navigator: {} })).toBeNull();
    expect(getModelContext({ document: { modelContext: { registerTool: 'nope' } }, navigator: {} })).toBeNull();
  });
});

describe('registerTools', () => {
  const tools = [
    { name: 'listWorkspaces', execute: () => {} },
    { name: 'createTask', execute: () => {} },
  ];

  it('registers every tool and says which', async () => {
    const context = workingContext();

    const result = await registerTools(tools, { context });

    expect(result.status).toBe(WebMCPStatus.Registered);
    expect(result.registered).toEqual(['listWorkspaces', 'createTask']);
    expect(result.refused).toEqual([]);
    expect(context.registerTool).toHaveBeenCalledTimes(2);
  });

  it('passes the abort signal, which is how a tool is withdrawn', async () => {
    const context = workingContext();
    const signal = new AbortController().signal;

    await registerTools(tools, { context, signal });

    expect(context.registerTool).toHaveBeenCalledWith(tools[0], { signal });
  });

  it('omits the options bag when there is no signal to pass', async () => {
    const context = workingContext();

    await registerTools([tools[0]], { context });

    expect(context.registerTool).toHaveBeenCalledWith(tools[0], {});
  });

  it('does nothing at all where WebMCP is unsupported', async () => {
    const result = await registerTools(tools, { context: null });

    expect(result).toEqual({ status: WebMCPStatus.Unsupported, registered: [], refused: [] });
  });

  it('keeps the rest of the catalogue when one tool is refused', async () => {
    // A schema the browser dislikes should cost that one tool, not the page.
    const context = {
      registerTool: vi.fn()
        .mockRejectedValueOnce(new Error('InvalidStateError: bad schema'))
        .mockResolvedValueOnce(undefined),
    };

    const result = await registerTools(tools, { context });

    expect(result.status).toBe(WebMCPStatus.Registered);
    expect(result.registered).toEqual(['createTask']);
    expect(result.refused).toEqual([{ name: 'listWorkspaces', reason: 'InvalidStateError: bad schema' }]);
  });

  it('describes a thrown non-error too', async () => {
    const context = { registerTool: vi.fn().mockRejectedValue('nope') };

    const { refused } = await registerTools([tools[0]], { context });

    expect(refused).toEqual([{ name: 'listWorkspaces', reason: 'nope' }]);
  });

  it('separates a browser that refused everything from one that has no WebMCP', async () => {
    // Only one of the two is something the deployment can fix.
    const context = { registerTool: vi.fn().mockRejectedValue(new Error('NotAllowedError')) };

    const result = await registerTools(tools, { context });

    expect(result.status).toBe(WebMCPStatus.Refused);
    expect(result.registered).toEqual([]);
  });

  it('is not "refused" when there was nothing to register', async () => {
    const result = await registerTools([], { context: workingContext() });

    expect(result.status).toBe(WebMCPStatus.Registered);
  });

  it('looks the browser up itself when given no context', async () => {
    // The production path: no injection, so it must find document.modelContext.
    const context = workingContext();
    document.modelContext = context;
    try {
      const result = await registerTools([tools[0]]);
      expect(result.registered).toEqual(['listWorkspaces']);
    } finally {
      delete document.modelContext;
    }
  });
});
