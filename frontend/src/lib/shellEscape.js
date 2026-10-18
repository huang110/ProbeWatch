/**
 * Shell string escaping helpers for safe Bash/Ash command generation.
 */
export const escapeShell = (val) => {
  if (val == null) return ''
  // Remove line breaks and escape characters that are special inside a
  // double-quoted POSIX shell argument.
  const sanitized = String(val).replace(/[\r\n]+/g, ' ')
  return sanitized.replace(/(["\\$`])/g, '\\$1')
}

// Quote the complete argument so spaces and shell metacharacters cannot turn
// generated installer commands into multiple arguments or extra commands.
export const quoteShell = (val) => `"${escapeShell(val)}"`
