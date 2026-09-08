import type { Locale } from "../../lib/i18n";
const messages = {
  "zh-CN": { unavailable: "无法打开此生图会话。会话可能已删除，或当前账号无权访问。", invalid: "会话地址无效，请返回会话列表。", back: "返回会话列表", retry: "重试" },
  "en-US": { unavailable: "This image session could not be opened. It may have been deleted, or this account may not have access.", invalid: "This session address is invalid. Return to the session list.", back: "Back to sessions", retry: "Retry" },
  "ja-JP": { unavailable: "この画像生成セッションを開けません。削除されたか、現在のアカウントにアクセス権がない可能性があります。", invalid: "セッションのアドレスが無効です。一覧に戻ってください。", back: "セッション一覧に戻る", retry: "再試行" },
  "vi-VN": { unavailable: "Không thể mở phiên tạo ảnh này. Phiên có thể đã bị xóa hoặc tài khoản hiện tại không có quyền truy cập.", invalid: "Địa chỉ phiên không hợp lệ. Hãy quay lại danh sách phiên.", back: "Quay lại danh sách phiên", retry: "Thử lại" },
};
export function imageRouteMessage(locale: Locale, key: keyof typeof messages["en-US"]) { return messages[locale][key]; }
