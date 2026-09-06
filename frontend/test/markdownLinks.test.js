import { describe, it, expect, vi } from 'vitest';

import {
  COPY_TEXT_ATTR,
  FILE_LINK_ATTR,
  FileLinkAction,
  copyLinkTarget,
  copyTargetFromEvent,
  writeClipboard,
  fileLinkAction,
  fileLinkFromEvent,
  filePathFromUrl,
  followFileLink,
} from '../src/composables/useMarkdownLinks';
import { renderMarkdown } from '../src/utils/markdown';

describe('filePathFromUrl', () => {
  it('decodes an ordinary local file URL', () => {
    expect(filePathFromUrl('file:///Users/mt/.gemini/brain/skills_support_plan.md'))
      .toBe('/Users/mt/.gemini/brain/skills_support_plan.md');
  });

  it('decodes percent-escapes so the path is the one on disk', () => {
    expect(filePathFromUrl('file:///Users/mt/My%20Notes/a%2Bb.md')).toBe('/Users/mt/My Notes/a+b.md');
  });

  it('treats localhost as this machine', () => {
    expect(filePathFromUrl('file://localhost/etc/hosts')).toBe('/etc/hosts');
  });

  it('refuses a URL naming another machine', () => {
    expect(filePathFromUrl('file://fileserver/share/report.pdf')).toBe('');
  });

  it('refuses schemes that are not file', () => {
    expect(filePathFromUrl('https://example.com/a.md')).toBe('');
    expect(filePathFromUrl('javascript:alert(1)')).toBe('');
  });

  it('returns empty for junk', () => {
    expect(filePathFromUrl('not a url')).toBe('');
    expect(filePathFromUrl(null)).toBe('');
    expect(filePathFromUrl(undefined)).toBe('');
  });

  it('keeps a malformed escape rather than losing the link', () => {
    expect(filePathFromUrl('file:///tmp/100%.md')).toBe('/tmp/100%.md');
  });

  it('drops the slash the URL form adds to a Windows drive path', () => {
    expect(filePathFromUrl('file:///C:/Users/mt/plan.md')).toBe('C:/Users/mt/plan.md');
  });
});

describe('fileLinkAction', () => {
  it('opens through the shell on the desktop', () => {
    expect(fileLinkAction({ isDesktop: true, bridge: { open: () => {} } })).toBe(FileLinkAction.Open);
  });

  it('copies in the browser, which cannot open a local file', () => {
    expect(fileLinkAction({ isDesktop: false, bridge: undefined })).toBe(FileLinkAction.Copy);
  });

  it('copies on a desktop build whose bridge predates the feature', () => {
    expect(fileLinkAction({ isDesktop: true, bridge: {} })).toBe(FileLinkAction.Copy);
  });
});

describe('followFileLink', () => {
  const desktop = (open) => ({ isDesktop: true, bridge: { open } });

  it('opens the file and says nothing when it worked', async () => {
    const open = vi.fn().mockResolvedValue({ ok: true, revealed: false });
    const result = await followFileLink('file:///Users/mt/plan.md', desktop(open));

    expect(open).toHaveBeenCalledWith('file:///Users/mt/plan.md');
    expect(result).toEqual({ tone: 'success', message: '' });
  });

  it('explains when the shell revealed the file instead of opening it', async () => {
    const result = await followFileLink(
      'file:///Users/mt/install.sh',
      desktop(vi.fn().mockResolvedValue({ ok: true, revealed: true }))
    );

    expect(result.tone).toBe('info');
    expect(result.message).toContain('/Users/mt/install.sh');
    expect(result.message).toContain('file manager');
  });

  it('reports what the shell said went wrong', async () => {
    const result = await followFileLink(
      'file:///Users/mt/gone.md',
      desktop(vi.fn().mockResolvedValue({ ok: false, error: 'No such file: /Users/mt/gone.md' }))
    );

    expect(result).toEqual({ tone: 'error', message: 'No such file: /Users/mt/gone.md' });
  });

  it('reports a bridge that threw', async () => {
    const result = await followFileLink(
      'file:///Users/mt/plan.md',
      desktop(vi.fn().mockRejectedValue(new Error('bridge is gone')))
    );

    expect(result).toEqual({ tone: 'error', message: 'bridge is gone' });
  });

  it('names the file when the bridge threw something with no message', async () => {
    const result = await followFileLink(
      'file:///Users/mt/plan.md',
      desktop(vi.fn().mockRejectedValue('nope'))
    );

    expect(result.tone).toBe('error');
    expect(result.message).toContain('/Users/mt/plan.md');
  });

  it('falls back to a generic message when the shell gave no reason', async () => {
    const result = await followFileLink(
      'file:///Users/mt/plan.md',
      desktop(vi.fn().mockResolvedValue({ ok: false }))
    );

    expect(result.tone).toBe('error');
    expect(result.message).toContain('/Users/mt/plan.md');
  });

  it('copies the path in the browser and says where it went', async () => {
    const copyText = vi.fn().mockResolvedValue(undefined);
    const result = await followFileLink('file:///Users/mt/plan.md', { isDesktop: false, copyText });

    expect(copyText).toHaveBeenCalledWith('/Users/mt/plan.md');
    expect(result.tone).toBe('info');
    expect(result.message).toContain('clipboard');
    expect(result.message).toContain('/Users/mt/plan.md');
  });

  it('still shows the path when the clipboard is denied', async () => {
    const result = await followFileLink('file:///Users/mt/plan.md', {
      isDesktop: false,
      copyText: vi.fn().mockRejectedValue(new Error('denied')),
    });

    expect(result.tone).toBe('info');
    expect(result.message).toContain('/Users/mt/plan.md');
  });

  it('refuses a link that names no local file', async () => {
    const open = vi.fn();
    const result = await followFileLink('file://fileserver/share/x.md', desktop(open));

    expect(open).not.toHaveBeenCalled();
    expect(result.tone).toBe('error');
  });
});

describe('renderMarkdown', () => {
  it('keeps a local file link reachable, which the sanitizer would otherwise strip', () => {
    const html = renderMarkdown('[plan](file:///Users/mt/skills%20support.md)');

    // The bug: DOMPurify drops a `file:` href, leaving an anchor that does nothing.
    expect(html).not.toContain('href');
    expect(html).toContain(`${FILE_LINK_ATTR}="file:///Users/mt/skills%20support.md"`);
    expect(html).toContain('class="md-file-link"');
    expect(html).toContain('>plan</a>');
  });

  it('titles the link with the path, since the text is usually just a filename', () => {
    expect(renderMarkdown('[plan](file:///Users/mt/deep/skills%20support.md)'))
      .toContain('title="/Users/mt/deep/skills support.md"');
  });

  it('renders markdown inside the link text', () => {
    expect(renderMarkdown('[**plan**](file:///a.md)')).toContain('<strong>plan</strong>');
  });

  it('leaves ordinary links to the default renderer', () => {
    const html = renderMarkdown('[docs](https://agentrq.com/docs)');

    expect(html).toContain('href="https://agentrq.com/docs"');
    expect(html).not.toContain(FILE_LINK_ATTR);
  });

  it('still refuses a javascript: link', () => {
    expect(renderMarkdown('[x](javascript:alert(1))')).not.toContain('javascript:');
  });

  it('escapes a quote in the file URL rather than breaking out of the attribute', () => {
    const html = renderMarkdown('[x](file:///tmp/a"onmouseover="alert(1).md)');

    // The quote has to stay inside the attribute value; escaped, the rest of
    // the URL is an odd filename and not a second attribute.
    const anchor = new DOMParser().parseFromString(html, 'text/html').querySelector('a');
    expect(anchor.getAttributeNames()).not.toContain('onmouseover');
    expect(anchor.getAttribute(FILE_LINK_ATTR)).toBe('file:///tmp/a"onmouseover="alert(1).md');
  });

  it('leaves plain text alone', () => {
    expect(renderMarkdown('just words')).toContain('just words');
  });

  it('renders nothing for nothing', () => {
    expect(renderMarkdown('')).toBe('');
    expect(renderMarkdown(null)).toBe('');
    expect(renderMarkdown(undefined)).toBe('');
  });

  it('shows angle-bracket text rather than swallowing it as markup', () => {
    // CommonMark reads `<Item>` as raw inline HTML, which the sanitizer then
    // drops as an unknown tag.
    expect(renderMarkdown('a List<Item> of things')).toContain('List&lt;Item&gt;');
  });

  it('prefers the title the markdown itself gives the link', () => {
    expect(renderMarkdown('[plan](file:///Users/mt/plan.md "The plan")'))
      .toContain('title="The plan"');
  });

  it('titles a file link on another machine with the URL, having no path', () => {
    // `file://fileserver/share/x.md` names no path on this computer, so there
    // is nothing better to show than the URL itself.
    const html = renderMarkdown('[report](file://fileserver/share/report.pdf)');

    expect(html).toContain('title="file://fileserver/share/report.pdf"');
  });
});

describe('fileLinkFromEvent', () => {
  // Driven through real rendered markdown, so the delegation is tested against
  // the markup the app actually produces rather than a hand-written anchor.
  const mount = (markdown) => {
    document.body.innerHTML = `<div id="root">${renderMarkdown(markdown)}</div>`;
    return document.querySelector('a');
  };

  it('finds the file link a click landed on', () => {
    const anchor = mount('[plan](file:///Users/mt/plan.md)');

    expect(fileLinkFromEvent({ type: 'click', target: anchor })).toBe('file:///Users/mt/plan.md');
  });

  it('finds it from a click on the text inside the link', () => {
    const anchor = mount('[**plan**](file:///Users/mt/plan.md)');

    expect(fileLinkFromEvent({ type: 'click', target: anchor.querySelector('strong') }))
      .toBe('file:///Users/mt/plan.md');
  });

  it('ignores a click on an ordinary link, which the browser handles', () => {
    const anchor = mount('[docs](https://agentrq.com/docs)');

    expect(fileLinkFromEvent({ type: 'click', target: anchor })).toBe('');
  });

  it('ignores a click on anything else in the app', () => {
    mount('some words');

    expect(fileLinkFromEvent({ type: 'click', target: document.querySelector('#root') })).toBe('');
  });

  it('follows the link on Enter and space, as a link should', () => {
    const anchor = mount('[plan](file:///Users/mt/plan.md)');

    expect(fileLinkFromEvent({ type: 'keydown', key: 'Enter', target: anchor })).toBe('file:///Users/mt/plan.md');
    expect(fileLinkFromEvent({ type: 'keydown', key: ' ', target: anchor })).toBe('file:///Users/mt/plan.md');
  });

  it('lets every other keystroke past, including space outside a link', () => {
    const anchor = mount('[plan](file:///Users/mt/plan.md)');

    expect(fileLinkFromEvent({ type: 'keydown', key: 'a', target: anchor })).toBe('');
    // Delegation puts this handler in front of every keystroke in the app;
    // claiming space anywhere else would stop people typing.
    document.body.innerHTML = '<textarea></textarea>';
    expect(fileLinkFromEvent({ type: 'keydown', key: ' ', target: document.querySelector('textarea') })).toBe('');
  });

  it('survives an event with nothing useful on it', () => {
    expect(fileLinkFromEvent(undefined)).toBe('');
    expect(fileLinkFromEvent({ type: 'click', target: null })).toBe('');
  });
});

describe('copy buttons', () => {
  const buttons = (markdown) => {
    document.body.innerHTML = `<div id="root">${renderMarkdown(markdown)}</div>`;
    return [...document.querySelectorAll('button')];
  };

  it('offers to copy the URL of an ordinary link', () => {
    const [button] = buttons('[docs](https://agentrq.com/docs)');

    expect(button.getAttribute(COPY_TEXT_ATTR)).toBe('https://agentrq.com/docs');
    expect(button.previousElementSibling.tagName).toBe('A');
  });

  it('offers the path, not the URL form, for a local file link', () => {
    const [button] = buttons('[plan](file:///Users/mt/skills%20support.md)');

    expect(button.getAttribute(COPY_TEXT_ATTR)).toBe('/Users/mt/skills support.md');
  });

  it('gives every link its own button', () => {
    const found = buttons('[a](https://a.test) and [b](file:///b.md) and [c](https://c.test)');

    expect(found.map((b) => b.getAttribute(COPY_TEXT_ATTR))).toEqual([
      'https://a.test',
      '/b.md',
      'https://c.test',
    ]);
  });

  it('names what it copies, for a tooltip and for a screen reader', () => {
    const [button] = buttons('[plan](file:///Users/mt/plan.md)');

    expect(button.getAttribute('title')).toBe('Copy /Users/mt/plan.md');
    expect(button.getAttribute('aria-label')).toBe('Copy /Users/mt/plan.md');
    // Inside a form it must not submit anything.
    expect(button.getAttribute('type')).toBe('button');
  });

  it('adds nothing where there is no link', () => {
    expect(buttons('just words and `code`')).toHaveLength(0);
  });

  it('falls back to the URL when a file link names no local path', () => {
    const [button] = buttons('[report](file://fileserver/share/report.pdf)');

    expect(button.getAttribute(COPY_TEXT_ATTR)).toBe('file://fileserver/share/report.pdf');
  });

  it('adds nothing to a link the sanitizer emptied', () => {
    // `javascript:` is rendered as plain text, so there is no anchor at all.
    expect(buttons('[x](javascript:alert(1))')).toHaveLength(0);
  });

  it('cannot be built out of message text', () => {
    // The buttons are DOM nodes made after sanitising, so a quote in the URL
    // stays a character in an attribute value rather than becoming markup.
    const [button] = buttons('[x](file:///tmp/a"onmouseover="alert(1).md)');

    expect(button.getAttributeNames()).not.toContain('onmouseover');
  });
});

describe('copyTargetFromEvent', () => {
  const mount = (markdown) => {
    document.body.innerHTML = `<div id="root">${renderMarkdown(markdown)}</div>`;
    return document.querySelector('button');
  };

  it('finds what a clicked button copies', () => {
    const button = mount('[docs](https://agentrq.com/docs)');

    expect(copyTargetFromEvent({ type: 'click', target: button })).toBe('https://agentrq.com/docs');
  });

  it('finds it from a click on the icon inside the button', () => {
    const button = mount('[docs](https://agentrq.com/docs)');

    expect(copyTargetFromEvent({ type: 'click', target: button.querySelector('svg') }))
      .toBe('https://agentrq.com/docs');
  });

  it('ignores a click on the link itself, which is a different action', () => {
    mount('[plan](file:///Users/mt/plan.md)');

    expect(copyTargetFromEvent({ type: 'click', target: document.querySelector('a') })).toBe('');
  });

  it('leaves keystrokes alone, since a button clicks itself on Enter and space', () => {
    const button = mount('[docs](https://agentrq.com/docs)');

    expect(copyTargetFromEvent({ type: 'keydown', key: 'Enter', target: button })).toBe('');
  });

  it('survives an event with nothing useful on it', () => {
    expect(copyTargetFromEvent(undefined)).toBe('');
    expect(copyTargetFromEvent({ type: 'click', target: null })).toBe('');
  });
});

describe('copyLinkTarget', () => {
  it('copies and reports what went to the clipboard', async () => {
    const copyText = vi.fn().mockResolvedValue(undefined);
    const result = await copyLinkTarget('/Users/mt/plan.md', { copyText });

    expect(copyText).toHaveBeenCalledWith('/Users/mt/plan.md');
    expect(result).toEqual({ tone: 'success', title: 'Copied', message: '/Users/mt/plan.md' });
  });

  it('still shows the text when the clipboard is denied', async () => {
    const result = await copyLinkTarget('/Users/mt/plan.md', {
      copyText: vi.fn().mockRejectedValue(new Error('denied')),
    });

    expect(result).toEqual({ tone: 'error', title: 'Could not copy', message: '/Users/mt/plan.md' });
  });
});

describe('writeClipboard', () => {
  it('uses the shell where there is one', async () => {
    const write = vi.fn().mockResolvedValue(true);
    const writeText = vi.fn();

    await writeClipboard('/Users/mt/plan.md', { bridge: { write }, clipboard: { writeText } });

    // Not the browser's: it refuses to write from an unfocused document, which
    // is a copy button that silently does nothing.
    expect(write).toHaveBeenCalledWith('/Users/mt/plan.md');
    expect(writeText).not.toHaveBeenCalled();
  });

  it('uses the browser clipboard when there is no shell', async () => {
    const writeText = vi.fn().mockResolvedValue(undefined);

    await writeClipboard('/Users/mt/plan.md', { clipboard: { writeText } });

    expect(writeText).toHaveBeenCalledWith('/Users/mt/plan.md');
  });

  it('falls back on a desktop build whose bridge predates the feature', async () => {
    const writeText = vi.fn().mockResolvedValue(undefined);

    await writeClipboard('/x.md', { bridge: {}, clipboard: { writeText } });

    expect(writeText).toHaveBeenCalledWith('/x.md');
  });

  it('lets a failure through to the caller, which reports it', async () => {
    await expect(
      writeClipboard('/x.md', { clipboard: { writeText: vi.fn().mockRejectedValue(new Error('denied')) } })
    ).rejects.toThrow('denied');
  });
});
