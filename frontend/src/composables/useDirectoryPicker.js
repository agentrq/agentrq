/**
 * Choosing a working directory with the operating system's folder chooser.
 *
 * Only the desktop shell can do this. A browser deliberately never reveals an
 * absolute filesystem path — `webkitdirectory` and `showDirectoryPicker` both
 * hand back a folder *name* and nothing more — so on the web the field is typed
 * into, and saying so is better than a button that cannot work.
 *
 * The bridge is checked as well as the platform, because those are two
 * different questions. A desktop build whose preload predates this feature is
 * still "desktop", but has no `chooseDirectory` to call: optional chaining then
 * resolves to undefined and the click does nothing at all, which is
 * indistinguishable from a broken button. Checking here means the button is not
 * offered unless it can actually work.
 */

export const PickerUnavailable = {
  /** The browser build, which cannot produce an absolute path. */
  Web: 'web',
  /** A desktop build whose bridge does not expose the chooser. */
  Bridge: 'bridge',
};

export const PICKER_MESSAGES = {
  [PickerUnavailable.Web]: 'Folder browsing is available in the desktop app. Type the path here instead.',
  [PickerUnavailable.Bridge]:
    'This desktop app could not open its folder chooser. Restarting it usually fixes this.',
};

/**
 * @param {{ isDesktop: boolean, bridge: unknown }} context
 * @returns {{ available: boolean, reason: string }}
 */
export function directoryPickerState({ isDesktop, bridge }) {
  if (!isDesktop) return { available: false, reason: PickerUnavailable.Web };
  if (typeof bridge?.chooseDirectory !== 'function') {
    return { available: false, reason: PickerUnavailable.Bridge };
  }
  return { available: true, reason: '' };
}

/**
 * Open the chooser.
 *
 * @returns {Promise<string>} the chosen path, or '' when the dialog was
 *          dismissed — so a cancelled choice never clears what is already set.
 * @throws {Error} with a message worth showing when it cannot be opened
 */
export async function chooseDirectory({ isDesktop, bridge, currentPath = '' }) {
  const { available, reason } = directoryPickerState({ isDesktop, bridge });
  if (!available) throw new Error(PICKER_MESSAGES[reason]);

  const chosen = await bridge.chooseDirectory(currentPath);
  return typeof chosen === 'string' ? chosen : '';
}

/**
 * An example path for the platform the person is on, so a Windows user is not
 * shown a Unix path to mentally translate before they can start.
 */
export function workingDirectoryPlaceholder(osPlatform) {
  if (osPlatform === 'win32') return 'C:\\Users\\you\\Code\\project';
  if (osPlatform === 'linux') return '/home/you/code/project';
  return '/Users/you/Code/project';
}
