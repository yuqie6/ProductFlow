import type { Locale } from "./i18n";

const messages = {
  "zh-CN": {
    "title": "个人偏好",
    "note": "语言和外观随账号保存，不改变商品内容的输出语言。",
    "automatic": "选择后自动保存。",
    "merchantTitle": "商家资料",
    "merchantNote": "管理此账号自有商家的名称。",
    "merchantInvalid": "商家名称需为 1–160 个字符。",
    "merchantSuspended": "商家已停用，暂时无法修改名称。"
  },
  "en-US": {
    "title": "Personal preferences",
    "note": "Language and appearance follow your account. Product output language stays unchanged.",
    "automatic": "Selections save automatically.",
    "merchantTitle": "Merchant profile",
    "merchantNote": "Manage the name of the merchant owned by this account.",
    "merchantInvalid": "Merchant name must contain 1–160 characters.",
    "merchantSuspended": "This merchant is suspended. Its name cannot be changed."
  },
  "ja-JP": {
    "title": "個人設定",
    "note": "言語と外観はアカウントに保存されます。商品コンテンツの出力言語は変わりません。",
    "automatic": "選択すると自動的に保存されます。",
    "merchantTitle": "事業者情報",
    "merchantNote": "このアカウントが所有する事業者の名前を管理します。",
    "merchantInvalid": "事業者名は 1～160 文字で入力してください。",
    "merchantSuspended": "事業者は停止中のため、名前を変更できません。"
  },
  "vi-VN": {
    "title": "Tùy chọn cá nhân",
    "note": "Ngôn ngữ và giao diện được lưu theo tài khoản, không thay đổi ngôn ngữ nội dung sản phẩm.",
    "automatic": "Lựa chọn được lưu tự động.",
    "merchantTitle": "Thông tin cửa hàng",
    "merchantNote": "Quản lý tên cửa hàng thuộc tài khoản này.",
    "merchantInvalid": "Tên cửa hàng phải có từ 1–160 ký tự.",
    "merchantSuspended": "Cửa hàng đang bị tạm dừng nên không thể đổi tên."
  }
} as const;

export function accountPreferencesMessage(locale: Locale, key: keyof typeof messages["zh-CN"]): string {
  return messages[locale][key];
}
