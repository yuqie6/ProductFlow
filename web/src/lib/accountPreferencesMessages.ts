import type { Locale } from "./i18n";

const messages = {
  "zh-CN": {
    "title": "个人偏好",
    "automatic": "选择后自动保存。",
    "merchantTitle": "商家资料",
    "merchantInvalid": "商家名称需为 1–160 个字符。",
    "merchantSuspended": "商家已停用，暂时无法修改名称。"
  },
  "en-US": {
    "title": "Personal preferences",
    "automatic": "Selections save automatically.",
    "merchantTitle": "Merchant profile",
    "merchantInvalid": "Merchant name must contain 1–160 characters.",
    "merchantSuspended": "This merchant is suspended. Its name cannot be changed."
  },
  "ja-JP": {
    "title": "個人設定",
    "automatic": "選択すると自動的に保存されます。",
    "merchantTitle": "事業者情報",
    "merchantInvalid": "事業者名は 1～160 文字で入力してください。",
    "merchantSuspended": "事業者は停止中のため、名前を変更できません。"
  },
  "vi-VN": {
    "title": "Tùy chọn cá nhân",
    "automatic": "Lựa chọn được lưu tự động.",
    "merchantTitle": "Thông tin cửa hàng",
    "merchantInvalid": "Tên cửa hàng phải có từ 1–160 ký tự.",
    "merchantSuspended": "Cửa hàng đang bị tạm dừng nên không thể đổi tên."
  }
} as const;

export function accountPreferencesMessage(locale: Locale, key: keyof typeof messages["zh-CN"]): string {
  return messages[locale][key];
}
