import { Sun, Moon, Desktop } from '@phosphor-icons/react'

export function ThemeToggle({ theme = 'system', onThemeChange, compact = true }) {
  if (compact) {
    return (
      <div className="theme-switcher-compact" role="group" aria-label="主题模式切换">
        <button
          type="button"
          className={`theme-btn ${theme === 'light' ? 'active' : ''}`}
          onClick={() => onThemeChange && onThemeChange('light')}
          title="白天主题 (浅色)"
          aria-label="白天浅色主题"
        >
          <Sun size={14} weight={theme === 'light' ? 'fill' : 'regular'} />
        </button>
        <button
          type="button"
          className={`theme-btn ${theme === 'dark' ? 'active' : ''}`}
          onClick={() => onThemeChange && onThemeChange('dark')}
          title="夜晚主题 (深色)"
          aria-label="夜晚深色主题"
        >
          <Moon size={14} weight={theme === 'dark' ? 'fill' : 'regular'} />
        </button>
        <button
          type="button"
          className={`theme-btn ${theme === 'system' ? 'active' : ''}`}
          onClick={() => onThemeChange && onThemeChange('system')}
          title="系统跟随 (自动跟随操作系统深浅色)"
          aria-label="系统跟随主题"
        >
          <Desktop size={14} weight={theme === 'system' ? 'fill' : 'regular'} />
        </button>
      </div>
    )
  }

  // 展开卡片式（用于系统设置页面）
  return (
    <div className="theme-card-grid" role="radiogroup" aria-label="主题偏好设置">
      <button
        type="button"
        className={`theme-choice-card ${theme === 'light' ? 'selected' : ''}`}
        onClick={() => onThemeChange && onThemeChange('light')}
        role="radio"
        aria-checked={theme === 'light'}
      >
        <div className="theme-choice-icon text-amber">
          <Sun size={22} weight={theme === 'light' ? 'fill' : 'regular'} />
        </div>
        <div className="theme-choice-text">
          <strong>白天主题 (Light)</strong>
          <span>纯白清爽面板，高对比度字体，适合日间光亮环境</span>
        </div>
      </button>

      <button
        type="button"
        className={`theme-choice-card ${theme === 'dark' ? 'selected' : ''}`}
        onClick={() => onThemeChange && onThemeChange('dark')}
        role="radio"
        aria-checked={theme === 'dark'}
      >
        <div className="theme-choice-icon text-blue">
          <Moon size={22} weight={theme === 'dark' ? 'fill' : 'regular'} />
        </div>
        <div className="theme-choice-text">
          <strong>夜晚主题 (Dark)</strong>
          <span>黑曜石暗色，柔和防眩光，MJJ 经典极客夜间体验</span>
        </div>
      </button>

      <button
        type="button"
        className={`theme-choice-card ${theme === 'system' ? 'selected' : ''}`}
        onClick={() => onThemeChange && onThemeChange('system')}
        role="radio"
        aria-checked={theme === 'system'}
      >
        <div className="theme-choice-icon text-mint">
          <Desktop size={22} weight={theme === 'system' ? 'fill' : 'regular'} />
        </div>
        <div className="theme-choice-text">
          <strong>系统跟随 (Auto)</strong>
          <span>自动匹配 Windows / macOS 系统的深色或浅色设置</span>
        </div>
      </button>
    </div>
  )
}
