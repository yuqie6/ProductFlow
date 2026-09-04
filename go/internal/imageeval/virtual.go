package imageeval

import "strings"

var virtualNeedles = []string{
	"账号", "帐号", "激活码", "卡密", "红包", "优惠券", "会员", "代充",
	"成品号", "续费", "cdk", "CDK", "steam", "充值", "点券", "Q币",
	"官方订阅", "独享稳定", "远程代安装",
}

var ugcNeedles = []string{
	"买家秀", "用户评价", "晒图", "追评", "评价图", "buyer show", "review photo",
}

// IsVirtualGoods 用标题判断虚拟货。虚拟货不当套图金标。
func IsVirtualGoods(title string) bool {
	return containsAny(title, virtualNeedles)
}

// IsUGCImage 用 alt/label 判断买家秀。买家秀不当金标单张。
func IsUGCImage(alt, label string) bool {
	return containsAny(alt+" "+label, ugcNeedles)
}

func containsAny(text string, needles []string) bool {
	lower := strings.ToLower(text)
	for _, needle := range needles {
		if strings.Contains(text, needle) || strings.Contains(lower, strings.ToLower(needle)) {
			return true
		}
	}
	return false
}
