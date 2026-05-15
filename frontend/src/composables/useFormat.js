export function useFormat() {
  const toKebabCase = (str) => {
    if (!str) return '';
    return str
      .toLowerCase()
      .trim()
      .replace(/[^a-z0-9]+/g, '-')
      .replace(/^-+|-+$/g, '');
  };

  const liveKebabCase = (str) => {
    if (!str) return '';
    return str
      .toLowerCase()
      .replace(/[\s_]+/g, '-')
      .replace(/[^a-z0-9-]/g, '');
  };

  return {
    toKebabCase,
    liveKebabCase
  };
}
