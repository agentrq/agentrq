import { describe, it, expect } from 'vitest';

import {
  SCOPE,
  availableScopes,
  describeAsk,
  scopeLabel,
  toGrant,
  useExtensionGrant,
  validateGrant,
} from '../src/composables/useExtensionGrant';

/**
 * Two conversations, and only one of them scales. The sentence about full
 * machine access is on every install, because there is no sandbox; the
 * permission list grows with what the manifest actually asked for.
 */

const manifest = (mcp) => ({ name: 'linear', mcp });

describe('describeAsk', () => {
  it('says nothing was asked when nothing was', () => {
    expect(describeAsk(manifest()).level).toBe('none');
    expect(describeAsk(manifest({ workspace: [], supervisor: [] })).level).toBe('none');
    expect(describeAsk(undefined).level).toBe('none');
  });

  it('reads a workspace-only ask', () => {
    const ask = describeAsk(manifest({ workspace: ['getTask'] }));

    expect(ask.level).toBe('workspace');
    expect(ask.workspace).toEqual(['getTask']);
  });

  it('reads any supervisor tool as the higher ask', () => {
    expect(describeAsk(manifest({ supervisor: ['listAllTasks'] })).level).toBe('supervisor');
  });

  // The field is the author describing their own extension. Nothing enforces it
  // — extensions are trusted code and can open any socket — so it must never
  // read as a restriction, and it is kept out of the permission lists for that
  // reason alone.
  it('reads the declared hosts as a claim by the author, not a permission', () => {
    const ask = describeAsk({ ...manifest({ workspace: ['getTask'] }), net: ['hooks.slack.com'] });

    expect(ask.net).toEqual(['hooks.slack.com']);
    expect(ask.networkClaim).toBe('Its author says it contacts hooks.slack.com.');
    // Not in either permission list, and not something a grant can narrow.
    expect(ask.workspace).toEqual(['getTask']);
    expect(toGrant(ask, { scope: SCOPE.workspace, workspaceId: 'ws1' })).not.toHaveProperty('net');
  });

  it('says nothing about the network when the manifest claimed nothing', () => {
    const ask = describeAsk(manifest({ workspace: ['getTask'] }));

    expect(ask.net).toEqual([]);
    // An empty sentence rather than "contacts nothing", which would be a promise.
    expect(ask.networkClaim).toBe('');
  });

  it('always carries the sentence about the machine', () => {
    // The emptiest screen must not describe the least-restrained code: with no
    // permission list at all, this sentence is the whole of what is said.
    for (const mcp of [undefined, { workspace: ['getTask'] }, { supervisor: ['x'] }]) {
      expect(describeAsk(manifest(mcp)).machineAccess).toContain('full access to your computer');
    }
  });
});

describe('availableScopes', () => {
  it('offers nothing to an extension that asked for nothing', () => {
    expect(availableScopes(describeAsk(manifest()))).toEqual([]);
  });

  // Everything below is asked from inside a workspace unless it says otherwise.
  // The screen extensions are actually installed from is not, which is what the
  // last block in this describe is about.
  const inWorkspace = { workspaceId: 'ws1' };

  it('does not offer the account to an extension that cannot use it', () => {
    // Offering a grant nothing would use invites somebody to hand over the
    // account for no reason at all.
    expect(availableScopes(describeAsk(manifest({ workspace: ['getTask'] })), inWorkspace)).toEqual([
      SCOPE.workspace,
      SCOPE.selected,
    ]);
  });

  it('offers the whole ladder to an extension that asked for the supervisor', () => {
    expect(availableScopes(describeAsk(manifest({ supervisor: ['listAllTasks'] })), inWorkspace)).toEqual([
      SCOPE.workspace,
      SCOPE.selected,
      SCOPE.supervisor,
    ]);
  });
});

/**
 * The screen extensions are actually installed from.
 *
 * It lives in the sidebar and belongs to no workspace, so "this workspace" had
 * no referent — and choosing it produced a grant with an empty workspace list.
 * Not a narrow grant: an inert one, refusing every call with "not granted
 * access to that workspace" for a workspace the user believed they had allowed.
 */
describe('availableScopes, with no workspace in context', () => {
  it('does not offer a rung whose name means nothing there', () => {
    const workspaceAsk = describeAsk(manifest({ workspace: ['getTask'] }));
    const accountAsk = describeAsk(manifest({ supervisor: ['listAllTasks'] }));

    expect(availableScopes(workspaceAsk)).toEqual([SCOPE.selected]);
    expect(availableScopes(accountAsk)).toEqual([SCOPE.selected, SCOPE.supervisor]);
  });

  it('still offers nothing to an extension that asked for nothing', () => {
    expect(availableScopes(describeAsk(manifest()))).toEqual([]);
  });
});

describe('validateGrant', () => {
  const ask = describeAsk(manifest({ workspace: ['getTask'], supervisor: ['listAllTasks'] }));

  it('accepts a rung the extension can use', () => {
    expect(validateGrant(ask, { scope: SCOPE.workspace, workspaceId: 'ws1' }).ok).toBe(true);
    expect(validateGrant(ask, { scope: SCOPE.supervisor }).ok).toBe(true);
  });

  it('asks for a workspace when the rung needs one', () => {
    const { ok, reason } = validateGrant(ask, { scope: SCOPE.selected, workspaces: [] });

    expect(ok).toBe(false);
    expect(reason).toBe('Choose at least one workspace.');
  });

  it('refuses the account to an extension that did not ask for it', () => {
    const narrow = describeAsk(manifest({ workspace: ['getTask'] }));

    expect(validateGrant(narrow, { scope: SCOPE.supervisor }).reason).toContain('did not ask');
  });

  it('refuses a rung that is not on the ladder', () => {
    expect(validateGrant(ask, { scope: 'everything' }).ok).toBe(false);
    expect(validateGrant(ask, {}).ok).toBe(false);
    expect(validateGrant(ask, undefined).ok).toBe(false);
  });

  it('takes a missing workspace list as an empty one', () => {
    // The middle rung with nothing chosen yet, before anyone has ticked a box.
    expect(validateGrant(ask, { scope: SCOPE.selected }).reason).toBe('Choose at least one workspace.');
  });

  it('has nothing to validate when nothing was asked for', () => {
    expect(validateGrant(describeAsk(manifest()), {}).ok).toBe(true);
  });
});

describe('validateGrant, with no workspace in context', () => {
  // The case that used to pass, and the reason the bug was invisible: an empty
  // grant looks exactly like a granted one until something is refused by it.
  it('refuses "this workspace" when there is no this workspace', () => {
    const ask = describeAsk(manifest({ workspace: ['getTask'] }));

    const { ok, reason } = validateGrant(ask, { scope: SCOPE.workspace, workspaces: [], workspaceId: '' });

    expect(ok).toBe(false);
    expect(reason).toContain('which workspaces');
  });

  it('accepts naming them instead', () => {
    const ask = describeAsk(manifest({ workspace: ['getTask'] }));

    expect(validateGrant(ask, { scope: SCOPE.selected, workspaces: ['ws1'], workspaceId: '' }).ok).toBe(true);
  });
});

describe('toGrant', () => {
  const ask = describeAsk(manifest({ workspace: ['getTask'], supervisor: ['listAllTasks'] }));

  it('carries the workspace that was in view at the narrowest rung', () => {
    const grant = toGrant(ask, { scope: SCOPE.workspace, workspaceId: 'ws1' });

    expect(grant.workspaces).toEqual(['ws1']);
    expect(grant.tools.supervisor).toEqual([]);
  });

  it('carries the chosen list at the middle rung', () => {
    const grant = toGrant(ask, { scope: SCOPE.selected, workspaces: ['ws1', 'ws2'] });

    expect(grant.workspaces).toEqual(['ws1', 'ws2']);
  });

  it('stores no workspace list at the supervisor rung', () => {
    // Not an empty list and not every id: storing one would imply it had been
    // consulted, and at that rung nothing consults it.
    const grant = toGrant(ask, { scope: SCOPE.supervisor });

    expect(grant.workspaces).toEqual([]);
    expect(grant.tools.supervisor).toEqual(['listAllTasks']);
  });

  it('records that the widest rung covers workspaces that do not exist yet', () => {
    // A standing future grant is meaningfully broader than the same list frozen
    // at install, and the screen has to be able to say which was given.
    expect(toGrant(ask, { scope: SCOPE.supervisor }).includesFutureWorkspaces).toBe(true);
    expect(toGrant(ask, { scope: SCOPE.workspace, workspaceId: 'ws1' }).includesFutureWorkspaces).toBe(
      false,
    );
  });

  it('withholds the supervisor tools from anything below that rung', () => {
    // Granting the tools without the scope would be a grant that reads narrow
    // and behaves wide.
    const grant = toGrant(ask, { scope: SCOPE.selected, workspaces: ['ws1'] });

    expect(grant.tools.supervisor).toEqual([]);
  });

  it('gives an extension that asked for nothing a grant that reaches nothing', () => {
    const grant = toGrant(describeAsk(manifest()), {});

    expect(grant.scope).toBe(SCOPE.workspace);
    expect(grant.tools).toEqual({ workspace: [], supervisor: [] });
    expect(grant.workspaces).toEqual([]);
  });
});

describe('scopeLabel', () => {
  it('says what each rung means, in a sentence', () => {
    expect(scopeLabel(SCOPE.workspace, { workspaceName: 'payments-api' })).toBe('payments-api only');
    expect(scopeLabel(SCOPE.workspace)).toBe('this workspace only');
    expect(scopeLabel(SCOPE.selected)).toBe('Selected workspaces');
    // Spelled out, because it is the part people would otherwise assume away.
    expect(scopeLabel(SCOPE.supervisor)).toBe('All workspaces, including ones you create later');
  });
});

describe('useExtensionGrant', () => {
  it('starts on the narrowest rung it can', () => {
    // A default that pre-selects the widest grant is a default that gets
    // accepted.
    const grant = useExtensionGrant({
      manifest: manifest({ workspace: ['getTask'], supervisor: ['listAllTasks'] }),
      workspaceId: 'ws1',
    });

    expect(grant.scope.value).toBe(SCOPE.workspace);
    expect(grant.valid.value).toBe(true);
  });

  it('reports what is missing rather than silently refusing', () => {
    const grant = useExtensionGrant({ manifest: manifest({ workspace: ['getTask'] }), workspaceId: 'ws1' });

    grant.scope.value = SCOPE.selected;

    expect(grant.valid.value).toBe(false);
    expect(grant.problem.value).toBe('Choose at least one workspace.');

    grant.selected.value = ['ws2'];
    expect(grant.valid.value).toBe(true);
    // And the message clears rather than lingering under a now-valid choice.
    expect(grant.problem.value).toBe('');
    expect(grant.grant.value.workspaces).toEqual(['ws2']);
  });

  it('offers no rungs, and is valid, for an extension that asked for nothing', () => {
    const grant = useExtensionGrant({ manifest: manifest(), workspaceId: 'ws1' });

    expect(grant.scopes.value).toEqual([]);
    expect(grant.valid.value).toBe(true);
    expect(grant.ask.value.level).toBe('none');
  });

  it('follows the chosen rung into the label and the grant', () => {
    const grant = useExtensionGrant({
      manifest: manifest({ workspace: ['getTask'], supervisor: ['listAllTasks'] }),
      workspaceId: 'ws1',
    });

    grant.scope.value = SCOPE.supervisor;

    expect(grant.label.value).toContain('including ones you create later');
    expect(grant.grant.value.tools.supervisor).toEqual(['listAllTasks']);
  });

  it('copes with being given nothing at all', () => {
    const grant = useExtensionGrant();

    expect(grant.ask.value.level).toBe('none');
    expect(grant.workspaces).toEqual([]);
  });
});
