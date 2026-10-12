/**
 * Shell string escaping helper for safe Bash/Ash script parameter generation
 */
export const escapeShell = (val) => {
  if (val == null) return ''
  // 1. 移除换行符与回车符，防止注入换行破坏 Shell 命令行语义
  const sanitized = String(val).replace(/[\r\n]+/g, ' ')
  // 2. 转义反斜杠、双引号、美元符及反引号
  return sanitized.replace(/(["\\$`])/g, '\\$1')
}
