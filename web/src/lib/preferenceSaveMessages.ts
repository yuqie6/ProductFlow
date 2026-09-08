import type { Locale } from "./i18n";

const messages = {
  "zh-CN": {
    "saving": "正在保存偏好…",
    "saved": "偏好已保存",
    "failed": "偏好未保存，当前设置保持不变。请重试。"
  },
  "en-US": {
    "saving": "Saving preferences…",
    "saved": "Preferences saved",
    "failed": "Preferences were not saved. Your current settings remain in place. Please retry."
  },
  "ja-JP": {
    "saving": "設定を保存中…",
    "saved": "設定を保存しました",
    "failed": "設定を保存できませんでした。現在の設定は維持されています。再試行してください。"
  },
  "vi-VN": {
    "saving": "Đang lưu tùy chọn…",
    "saved": "Đã lưu tùy chọn",
    "failed": "Chưa lưu được tùy chọn. Cài đặt hiện tại được giữ nguyên. Vui lòng thử lại."
  }
} as const;

export function preferenceSaveMessage(locale: Locale, key: keyof typeof messages["zh-CN"]): string {
  return messages[locale][key];
}
